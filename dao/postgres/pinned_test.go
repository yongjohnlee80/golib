package postgres

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/yongjohnlee80/golib/dao"
)

// Server-free cells for the pinned connection. Everything needing a live
// PostgreSQL is in
// pinned_integration_test.go (build tag integration); these run in the default gate.

// noPinConn is a dao.DataConn without the SessionPinner capability. The embedded nil
// interface satisfies the method set; nothing here ever calls through it.
type noPinConn struct{ dao.DataConn }

// The SERVER-FREE half: a DataConn without the capability is reported
// honestly through the typed helper — dao.ErrUnsupported, never a panic.
func TestPinned_CapabilityMissIsErrUnsupported(t *testing.T) {
	t.Parallel()

	if SupportsSessionPinning(noPinConn{}) {
		t.Fatal("SupportsSessionPinning(noPinConn) = true, want false")
	}
	pc, err := PinSessionConn(context.Background(), noPinConn{})
	if !errors.Is(err, dao.ErrUnsupported) {
		t.Fatalf("PinSessionConn err = %v, want dao.ErrUnsupported", err)
	}
	if pc != nil {
		t.Fatalf("PinSessionConn returned a handle %v on a miss", pc)
	}
	// And the capability is present on the real driver type (the compile-time
	// assertion in pinned.go is the primary proof; this is its runtime echo).
	if !SupportsSessionPinning((*pgxConn)(nil)) {
		t.Fatal("SupportsSessionPinning(*pgxConn) = false, want true")
	}
}

// The closed vocabulary, server-free half: the zero ExtendedOp{} — the one spelling a caller
// can build without a constructor — is refused at the vocabulary boundary, and the
// handle's outbound track is untouched by the refusal.
func TestPinned_ZeroExtendedOpIsRefused(t *testing.T) {
	t.Parallel()

	p := &pinnedConn{frontend: pgproto3.NewFrontend(bytes.NewReader(nil), io.Discard)}
	err := p.Send(context.Background(), ExtendedOp{})
	if !errors.Is(err, ErrInvalidExtendedOp) {
		t.Fatalf("Send(ExtendedOp{}) err = %v, want ErrInvalidExtendedOp", err)
	}
	if p.out != idleOut {
		t.Fatalf("outbound track = %d after a refused Send, want idleOut", p.out)
	}
	// Control: a constructed op is accepted and moves the track.
	if err := p.Send(context.Background(), ParseOp("", "SELECT 1", nil)); err != nil {
		t.Fatalf("Send(ParseOp) err = %v", err)
	}
	if p.out != building {
		t.Fatalf("outbound track = %d after Send, want building", p.out)
	}
}

// beginSQL renders every option the domain has, and refuses before the wire what
// Validate refuses.
func TestPinned_BeginSQL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts dao.TxOptions
		want string
	}{
		{"defaults", dao.TxOptions{}, "BEGIN"},
		{"read only", dao.TxOptions{Access: dao.TxReadOnly}, "BEGIN read only"},
		{"read write", dao.TxOptions{Access: dao.TxReadWrite}, "BEGIN read write"},
		{"repeatable read", dao.TxOptions{Isolation: dao.TxRepeatableRead}, "BEGIN ISOLATION LEVEL repeatable read"},
		{
			"serializable read only deferrable",
			dao.TxOptions{Isolation: dao.TxSerializable, Access: dao.TxReadOnly, Deferrable: dao.TxDeferrable},
			"BEGIN ISOLATION LEVEL serializable read only deferrable",
		},
		{
			"serializable read only not deferrable",
			dao.TxOptions{Isolation: dao.TxSerializable, Access: dao.TxReadOnly, Deferrable: dao.TxNotDeferrable},
			"BEGIN ISOLATION LEVEL serializable read only not deferrable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := beginSQL(tt.opts)
			if err != nil {
				t.Fatalf("beginSQL(%+v): %v", tt.opts, err)
			}
			if got != tt.want {
				t.Fatalf("beginSQL(%+v) = %q, want %q", tt.opts, got, tt.want)
			}
		})
	}

	// Invalid: DEFERRABLE without SERIALIZABLE READ ONLY is refused before any BEGIN.
	var inv *dao.ErrTxOptionInvalid
	if _, err := beginSQL(dao.TxOptions{Deferrable: dao.TxDeferrable}); !errors.As(err, &inv) {
		t.Fatalf("beginSQL(deferrable alone) err = %v, want *dao.ErrTxOptionInvalid", err)
	}
}

// encodeTextArgs: NULL is a nil slice (both a nil interface and a typed nil pointer),
// values render in PostgreSQL text format against the declared OIDs.
func TestPinned_EncodeTextArgs(t *testing.T) {
	t.Parallel()

	m := pgtype.NewMap()
	var nilStr *string
	oids := []uint32{pgtype.TextOID, pgtype.Int8OID, pgtype.ByteaOID, pgtype.BoolOID, pgtype.Float8OID, pgtype.TextOID, pgtype.Int4OID, pgtype.TextOID}
	args := []any{"abc", int64(42), []byte{0, 255}, true, 1.5, nilStr, nil, ""}
	got, err := encodeTextArgs(m, oids, args)
	if err != nil {
		t.Fatalf("encodeTextArgs: %v", err)
	}
	// The last entry is the empty string: zero-length and NON-nil — an empty value, not
	// NULL. (Encoding into a nil scratch buffer would have returned nil here and sent a
	// NULL; the first live run of the args cell caught exactly that.)
	want := [][]byte{[]byte("abc"), []byte("42"), []byte(`\x00ff`), []byte("t"), []byte("1.5"), nil, nil, {}}
	if got[7] == nil || len(got[7]) != 0 {
		t.Fatalf("empty string encoded as %#v, want a zero-length non-nil slice (NOT NULL)", got[7])
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if want[i] == nil {
			if got[i] != nil {
				t.Errorf("arg %d: got %q, want NULL (nil slice)", i, got[i])
			}
			continue
		}
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("arg %d: got %q, want %q", i, got[i], want[i])
		}
	}

	// An unencodable value is an error naming the argument, not a silent NULL.
	if _, err := encodeTextArgs(m, []uint32{pgtype.Int8OID}, []any{struct{ X int }{1}}); err == nil {
		t.Fatal("encoding a struct as int8 succeeded; want an error")
	}

	// The discriminating input for the NULL-vs-empty trap: an empty string as the FIRST
	// (here, only) argument, so it is encoded into a still-empty scratch buffer. With a
	// nil scratch, Encode appends zero bytes and returns nil — indistinguishable from
	// NULL — and the empty value would be sent as NULL. A non-nil scratch keeps it a
	// zero-length non-nil slice. (An empty value later in the list rides on a scratch the
	// earlier args already made non-nil, so it does NOT exercise the bug — which is why
	// the mutation survived the mixed-position case above.)
	if only, err := encodeTextArgs(m, []uint32{pgtype.TextOID}, []any{""}); err != nil {
		t.Fatalf("encodeTextArgs(empty first): %v", err)
	} else if only[0] == nil {
		t.Fatal("an empty string as the first argument encoded to NULL; NULL and empty were conflated")
	} else if len(only[0]) != 0 {
		t.Fatalf("empty string encoded to %q, want a zero-length non-nil slice", only[0])
	}
}

// decodeMessage maps every backend message the extended face forwards, including the
// asynchronous three, and refuses one it does not know rather than mislabeling it.
func TestPinned_DecodeMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		msg  pgproto3.BackendMessage
		kind string
	}{
		{&pgproto3.ParseComplete{}, "ParseComplete"},
		{&pgproto3.BindComplete{}, "BindComplete"},
		{&pgproto3.ParameterDescription{ParameterOIDs: []uint32{23}}, "ParameterDescription"},
		{&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{{Name: []byte("x"), DataTypeOID: 23, DataTypeSize: 4, TypeModifier: -1}}}, "RowDescription"},
		{&pgproto3.DataRow{Values: [][]byte{[]byte("1"), nil, {}}}, "DataRow"},
		{&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")}, "CommandComplete"},
		{&pgproto3.PortalSuspended{}, "PortalSuspended"},
		{&pgproto3.CloseComplete{}, "CloseComplete"},
		{&pgproto3.NoData{}, "NoData"},
		{&pgproto3.EmptyQueryResponse{}, "EmptyQueryResponse"},
		{&pgproto3.NoticeResponse{Severity: "NOTICE", Code: "00000", Message: "hi"}, "NoticeResponse"},
		{&pgproto3.ParameterStatus{Name: "TimeZone", Value: "UTC"}, "ParameterStatus"},
		{&pgproto3.NotificationResponse{PID: 7, Channel: "c", Payload: "p"}, "NotificationResponse"},
	}
	for _, tt := range tests {
		got, err := decodeMessage(tt.msg)
		if err != nil {
			t.Fatalf("decodeMessage(%T): %v", tt.msg, err)
		}
		if got.Kind != tt.kind {
			t.Errorf("decodeMessage(%T).Kind = %q, want %q", tt.msg, got.Kind, tt.kind)
		}
	}

	rd, _ := decodeMessage(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{{Name: []byte("x"), TableOID: 9, TableAttributeNumber: 2, DataTypeOID: 23, DataTypeSize: 4, TypeModifier: -1, Format: 1}}})
	want := ExtendedFieldDescription{Name: "x", TableOID: 9, ColumnAttr: 2, TypeOID: 23, TypeSize: 4, TypeModifier: -1, Format: 1}
	if len(rd.Fields) != 1 || rd.Fields[0] != want {
		t.Errorf("RowDescription fields = %+v, want [%+v]", rd.Fields, want)
	}
	dr, _ := decodeMessage(&pgproto3.DataRow{Values: [][]byte{[]byte("1"), nil, {}}})
	if dr.Values[1] != nil || dr.Values[2] == nil || len(dr.Values[2]) != 0 {
		t.Errorf("DataRow NULL/empty distinction lost: %#v", dr.Values)
	}
	nt, _ := decodeMessage(&pgproto3.NoticeResponse{Severity: "NOTICE", Code: "00000", Message: "hi"})
	if nt.Notice == nil || nt.Notice.Message != "hi" || nt.Notice.Severity != "NOTICE" {
		t.Errorf("NoticeResponse not carried: %+v", nt.Notice)
	}
	ps, _ := decodeMessage(&pgproto3.ParameterStatus{Name: "TimeZone", Value: "UTC"})
	if ps.ParameterName != "TimeZone" || ps.ParameterValue != "UTC" {
		t.Errorf("ParameterStatus not carried: %q=%q", ps.ParameterName, ps.ParameterValue)
	}
	nf, _ := decodeMessage(&pgproto3.NotificationResponse{PID: 7, Channel: "c", Payload: "p"})
	if nf.Notification == nil || nf.Notification.PID != 7 || nf.Notification.Channel != "c" || nf.Notification.Payload != "p" {
		t.Errorf("NotificationResponse not carried: %+v", nf.Notification)
	}
	if _, err := decodeMessage(&pgproto3.CopyInResponse{}); err == nil {
		t.Error("decodeMessage(CopyInResponse) succeeded; a message outside the vocabulary must be an error")
	}

	// The ErrorResponse converter is pgconn's own, so the PgError is byte-identical to
	// what pgx would surface.
	em := errorResponseMessage(&pgproto3.ErrorResponse{Severity: "ERROR", Code: "42601", Message: "syntax error"})
	if em.Kind != "ErrorResponse" || em.Err == nil || em.Err.Code != "42601" || em.Err.Message != "syntax error" {
		t.Errorf("errorResponseMessage = %+v", em)
	}
}

// retryable is a stand-in for pgconn's own SafeToRetry errors (every ReceiveMessage
// failure carries safeToRetry=true).
type retryable struct{}

func (retryable) Error() string     { return "receive message failed" }
func (retryable) SafeToRetry() bool { return true }

// The dispatch-aware error shapes drive classifyCommit to the RIGHT fault state — and
// the control shows the wrapper is what makes the difference: pgconn's blanket
// SafeToRetry, unqualified, would read a lost COMMIT response as a proven rollback.
func TestPinned_DispatchErrorShapesClassify(t *testing.T) {
	t.Parallel()

	// Not dispatched: proven never written → fault state 2, cause preserved.
	nd := classifyCommit(&notDispatchedError{cause: context.Canceled})
	if !errors.Is(nd, dao.ErrTxRolledBack) || errors.Is(nd, dao.ErrTxOutcomeUnknown) {
		t.Errorf("notDispatched → %v, want ErrTxRolledBack only", nd)
	}
	if !errors.Is(nd, context.Canceled) {
		t.Errorf("notDispatched lost its cause: %v", nd)
	}

	// Dispatched and lost: the cause says SafeToRetry, the wrapper says no → fault
	// state 4 — and the cause is still reachable.
	dl := classifyCommit(&dispatchedError{cause: retryable{}})
	if !errors.Is(dl, dao.ErrTxOutcomeUnknown) || errors.Is(dl, dao.ErrTxRolledBack) {
		t.Errorf("dispatched → %v, want ErrTxOutcomeUnknown only", dl)
	}
	var r retryable
	if !errors.As(dl, &r) {
		t.Errorf("dispatched lost its cause: %v", dl)
	}
	if pgconn.SafeToRetry(&dispatchedError{cause: retryable{}}) {
		t.Error("dispatchedError reports SafeToRetry; the frame was written")
	}

	// Control — the instrument observes: the SAME cause unwrapped is classified as a
	// rollback, which is exactly the false guarantee the wrapper exists to prevent.
	if ctl := classifyCommit(retryable{}); !errors.Is(ctl, dao.ErrTxRolledBack) {
		t.Errorf("control: bare retryable → %v, want ErrTxRolledBack (proves the wrapper is load-bearing)", ctl)
	}
}
