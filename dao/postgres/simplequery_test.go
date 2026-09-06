package postgres

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/yongjohnlee80/golib/dao"
)

// scriptedWire is a fake server: it hands the handle a fixed sequence of backend
// messages through the recv seam and records how many it was asked for. A gate
// can hold one message until the test releases it, which is how streaming-before-
// tail is observed rather than assumed.
type scriptedWire struct {
	mu    sync.Mutex
	msgs  []pgproto3.BackendMessage
	errAt int // 1-based index at which recv returns errAt's transport error; 0 = never
	err   error
	gates map[int]chan struct{} // 0-based index → wait on this before returning it
	calls int
}

func (s *scriptedWire) recv(ctx context.Context) (pgproto3.BackendMessage, error) {
	s.mu.Lock()
	i := s.calls
	s.calls++
	gate := s.gates[i]
	s.mu.Unlock()
	if s.errAt != 0 && i+1 == s.errAt {
		return nil, s.err
	}
	if gate != nil {
		select {
		case <-gate:
		case <-time.After(2 * time.Second):
			return nil, errors.New("scriptedWire: message %d was not released — the driver buffered instead of streaming")
		}
	}
	if i >= len(s.msgs) {
		return nil, io.ErrUnexpectedEOF
	}
	return s.msgs[i], nil
}

func (s *scriptedWire) callCount() int { s.mu.Lock(); defer s.mu.Unlock(); return s.calls }

// simpleHandle builds a pinnedConn over a drained pipe with a scripted reader.
func simpleHandle(t *testing.T, w *scriptedWire) *pinnedConn {
	t.Helper()
	srv, cli := net.Pipe()
	t.Cleanup(func() { _ = srv.Close(); _ = cli.Close() })
	drainPeer(t, srv)
	return &pinnedConn{frontend: pgproto3.NewFrontend(srv, cli), netConn: cli, recv: w.recv}
}

func rfq(st byte) pgproto3.BackendMessage { return &pgproto3.ReadyForQuery{TxStatus: st} }
func cc(tag string) pgproto3.BackendMessage {
	return &pgproto3.CommandComplete{CommandTag: []byte(tag)}
}
func row(vals ...string) pgproto3.BackendMessage {
	v := make([][]byte, len(vals))
	for i, s := range vals {
		v[i] = []byte(s)
	}
	return &pgproto3.DataRow{Values: v}
}
func rowDesc(names ...string) pgproto3.BackendMessage {
	f := make([]pgproto3.FieldDescription, len(names))
	for i, n := range names {
		f[i] = pgproto3.FieldDescription{Name: []byte(n), DataTypeOID: 25, DataTypeSize: -1, TypeModifier: -1}
	}
	return &pgproto3.RowDescription{Fields: f}
}

// A1-C3: nil emit fails BEFORE dispatch — no read, no write, no state change.
func TestSimpleQuery_NilEmitFailsBeforeDispatch(t *testing.T) {
	t.Parallel()
	w := &scriptedWire{msgs: []pgproto3.BackendMessage{rfq('I')}}
	p := simpleHandle(t, w)
	_, err := p.SimpleQuery(context.Background(), "SELECT 1", nil)
	if !errors.Is(err, ErrEmitNil) {
		t.Fatalf("nil emit returned %v, want ErrEmitNil", err)
	}
	if w.callCount() != 0 {
		t.Fatalf("nil emit read %d message(s) from the wire; it must touch nothing", w.callCount())
	}
	if !p.quiescentLocked() {
		t.Fatal("nil emit left the handle non-quiescent")
	}
}

// A1-C3: emit is called AS EACH MESSAGE IS DECODED, before the tail is consumed.
// The second DataRow is held back until the emitter reports the first; a driver
// that buffered until ReadyForQuery could never release it.
func TestSimpleQuery_StreamsBeforeTheTail(t *testing.T) {
	t.Parallel()
	sawFirst := make(chan struct{})
	w := &scriptedWire{
		msgs:  []pgproto3.BackendMessage{rowDesc("n"), row("1"), row("2"), cc("SELECT 2"), rfq('I')},
		gates: map[int]chan struct{}{2: sawFirst}, // row("2") waits for the emitter
	}
	p := simpleHandle(t, w)
	var kinds []string
	st, err := p.SimpleQuery(context.Background(), "SELECT n FROM t", func(m ExtendedMessage) error {
		kinds = append(kinds, m.Kind)
		if m.Kind == "DataRow" && len(kinds) == 2 { // the first row
			close(sawFirst)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("SimpleQuery: %v", err)
	}
	if st != 'I' {
		t.Fatalf("status %c, want I", st)
	}
	want := []string{"RowDescription", "DataRow", "DataRow", "CommandComplete"}
	if len(kinds) != len(want) {
		t.Fatalf("emitted %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("emitted %v, want %v (wire order)", kinds, want)
		}
	}
}

// A1-C3: ReadyForQuery is NEVER emitted; it returns only as the status byte.
func TestSimpleQuery_NeverEmitsReadyForQuery(t *testing.T) {
	t.Parallel()
	w := &scriptedWire{msgs: []pgproto3.BackendMessage{cc("SELECT 0"), rfq('T')}}
	p := simpleHandle(t, w)
	p.txOpen = true
	st, err := p.SimpleQuery(context.Background(), "SELECT 1 WHERE false", func(m ExtendedMessage) error {
		if m.Kind == "ReadyForQuery" {
			t.Fatal("ReadyForQuery was emitted as a message; readiness is the status byte")
		}
		return nil
	})
	if err != nil || st != 'T' {
		t.Fatalf("status %c err %v, want T nil", st, err)
	}
}

// A1-C3: an emitter error stops delivery, the driver DRAINS to the terminal
// ReadyForQuery, returns the emitter's error, and the handle is quiescent — a
// following SimpleQuery on the same handle works.
func TestSimpleQuery_EmitterErrorDrainsToReadyAndTheHandleIsReusable(t *testing.T) {
	t.Parallel()
	boom := errors.New("emitter refused")
	w := &scriptedWire{msgs: []pgproto3.BackendMessage{rowDesc("n"), row("1"), row("2"), cc("SELECT 2"), rfq('I'),
		/* second query */ cc("SELECT 0"), rfq('I')}}
	p := simpleHandle(t, w)
	var delivered int
	_, err := p.SimpleQuery(context.Background(), "SELECT n FROM t", func(m ExtendedMessage) error {
		delivered++
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("returned %v, want the emitter's error", err)
	}
	if delivered != 1 {
		t.Fatalf("emit was called %d times after it errored; delivery must stop at the first error", delivered)
	}
	if w.callCount() != 5 {
		t.Fatalf("driver read %d messages; it must drain all 5 to the terminal ReadyForQuery before returning", w.callCount())
	}
	if !p.quiescentLocked() {
		t.Fatal("handle not quiescent after an emitter error; the next query would be refused")
	}
	st, err := p.SimpleQuery(context.Background(), "SELECT 1 WHERE false", func(ExtendedMessage) error { return nil })
	if err != nil || st != 'I' {
		t.Fatalf("the following SimpleQuery failed (%c, %v); the wire was not returned to quiescent", st, err)
	}
}

// A1-C4: a transport failure during the drain outranks a prior emitter error and
// poisons the handle; the caller must Discard.
func TestSimpleQuery_TransportFailureOutranksEmitterErrorAndPoisons(t *testing.T) {
	t.Parallel()
	boom := errors.New("emitter refused")
	w := &scriptedWire{msgs: []pgproto3.BackendMessage{rowDesc("n"), row("1")}, errAt: 3, err: io.ErrUnexpectedEOF}
	p := simpleHandle(t, w)
	_, err := p.SimpleQuery(context.Background(), "SELECT n FROM t", func(ExtendedMessage) error { return boom })
	var de *dispatchedError
	if !errors.As(err, &de) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("returned %v, want the TRANSPORT error (dispatched-and-lost), not the emitter's", err)
	}
	if errors.Is(err, boom) {
		t.Fatal("the emitter's error was returned over the transport failure")
	}
	if _, err := p.SimpleQuery(context.Background(), "SELECT 1", func(ExtendedMessage) error { return nil }); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("after a transport failure the next call returned %v, want ErrPoisoned", err)
	}
}

// A1-C4: ErrorResponse is PROTOCOL DATA — emitted verbatim, not poison, and the
// server's own ReadyForQuery ends the exchange.
func TestSimpleQuery_ErrorResponseIsProtocolDataNotPoison(t *testing.T) {
	t.Parallel()
	w := &scriptedWire{msgs: []pgproto3.BackendMessage{&pgproto3.ErrorResponse{Severity: "ERROR", Code: "42P01", Message: "relation \"t\" does not exist"}, rfq('I')}}
	p := simpleHandle(t, w)
	var got ExtendedMessage
	st, err := p.SimpleQuery(context.Background(), "SELECT * FROM t", func(m ExtendedMessage) error { got = m; return nil })
	if err != nil || st != 'I' {
		t.Fatalf("status %c err %v, want I nil — a target error is not a driver error", st, err)
	}
	if got.Kind != "ErrorResponse" || got.Err == nil || got.Err.Code != "42P01" {
		t.Fatalf("emitted %+v, want Kind ErrorResponse with the PgError verbatim", got)
	}
	if p.poisoned {
		t.Fatal("an ErrorResponse poisoned the handle")
	}
}

// A1-C4: transaction control is detected on the CommandComplete TAG stream, so
// net-zero and same-class sequences cannot hide it. Each poisons.
func TestSimpleQuery_TransactionControlPoisons(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		txOpen bool
		msgs   []pgproto3.BackendMessage
	}{
		{"single BEGIN from idle (I→T)", false, []pgproto3.BackendMessage{cc("BEGIN"), rfq('T')}},
		{"BEGIN;COMMIT from idle — net-zero I→I", false, []pgproto3.BackendMessage{cc("BEGIN"), cc("COMMIT"), rfq('I')}},
		{"COMMIT;BEGIN inside a live tx — same-class T→T", true, []pgproto3.BackendMessage{cc("COMMIT"), cc("BEGIN"), rfq('T')}},
		{"SAVEPOINT inside a live tx — T→T", true, []pgproto3.BackendMessage{cc("SAVEPOINT"), rfq('T')}},
		{"status crosses with NO control tag (defence in depth)", false, []pgproto3.BackendMessage{cc("SELECT 1"), rfq('T')}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := &scriptedWire{msgs: tc.msgs}
			p := simpleHandle(t, w)
			p.txOpen = tc.txOpen
			_, err := p.SimpleQuery(context.Background(), "x", func(ExtendedMessage) error { return nil })
			if !errors.Is(err, ErrTransactionControlOnRawFace) {
				t.Fatalf("returned %v, want ErrTransactionControlOnRawFace", err)
			}
			if !p.poisoned {
				t.Fatal("transaction control on the raw face did not poison the handle")
			}
		})
	}
}

// A1-C4 negative control: ordinary multi-statement text inside a live transaction
// is NOT control — two SELECT tags, T→T, not poisoned. The control check must
// not fire on the thing this face exists for.
func TestSimpleQuery_OrdinaryMultiStatementInsideTxIsNotControl(t *testing.T) {
	t.Parallel()
	w := &scriptedWire{msgs: []pgproto3.BackendMessage{rowDesc("a"), row("1"), cc("SELECT 1"), rowDesc("b"), row("2"), cc("SELECT 1"), rfq('T')}}
	p := simpleHandle(t, w)
	p.txOpen = true
	var tags []string
	st, err := p.SimpleQuery(context.Background(), "SELECT 1; SELECT 2", func(m ExtendedMessage) error {
		if m.Kind == "CommandComplete" {
			tags = append(tags, m.Tag)
		}
		return nil
	})
	if err != nil || st != 'T' || p.poisoned {
		t.Fatalf("status %c err %v poisoned %v; want T nil false", st, err, p.poisoned)
	}
	if len(tags) != 2 {
		t.Fatalf("saw %d CommandComplete tags, want 2 (one per statement)", len(tags))
	}
}

// A1-C4: a statement failing inside the owned transaction is T→E — reported to
// the owner through the status, NOT poison, and NOT control.
func TestSimpleQuery_FailureInsideTxIsAbortedNotPoison(t *testing.T) {
	t.Parallel()
	w := &scriptedWire{msgs: []pgproto3.BackendMessage{&pgproto3.ErrorResponse{Severity: "ERROR", Code: "22012", Message: "division by zero"}, rfq('E')}}
	p := simpleHandle(t, w)
	p.txOpen = true
	st, err := p.SimpleQuery(context.Background(), "SELECT 1/0", func(ExtendedMessage) error { return nil })
	if err != nil || st != 'E' || p.poisoned {
		t.Fatalf("status %c err %v poisoned %v; want E nil false", st, err, p.poisoned)
	}
}

// A1-C3: inside emit the handle is PRIVATE — a re-entrant call returns
// ErrSegmentInFlight and does not deadlock. And a SimpleQuery while an extended
// segment is in flight is refused the same way.
func TestSimpleQuery_ReentryAndMidSegmentAreRefused(t *testing.T) {
	t.Parallel()
	w := &scriptedWire{msgs: []pgproto3.BackendMessage{cc("SELECT 0"), rfq('I')}}
	p := simpleHandle(t, w)
	var send, flush, receive, sync, simple, begin, release error
	if _, err := p.SimpleQuery(context.Background(), "SELECT 1 WHERE false", func(ExtendedMessage) error {
		send = p.Send(context.Background(), ParseOp("", "SELECT 1", nil))
		flush = p.Flush(context.Background())
		_, receive = p.Receive(context.Background())
		_, sync = p.Sync(context.Background())
		_, simple = p.SimpleQuery(context.Background(), "SELECT 2", func(ExtendedMessage) error { return nil })
		_, begin = p.BeginSessionTx(context.Background(), dao.TxOptions{})
		release = p.Release(context.Background())
		return nil
	}); err != nil {
		t.Fatalf("SimpleQuery: %v", err)
	}
	for name, got := range map[string]error{"Send": send, "Flush": flush, "Receive": receive, "Sync": sync, "SimpleQuery": simple, "BeginSessionTx": begin, "Release": release} {
		if !errors.Is(got, ErrSegmentInFlight) {
			t.Fatalf("re-entrant %s inside emit returned %v, want ErrSegmentInFlight", name, got)
		}
	}
	// Mid-segment: a queued extended frame makes the handle non-quiescent.
	p2 := simpleHandle(t, &scriptedWire{})
	if err := p2.Send(context.Background(), ParseOp("", "SELECT 1", nil)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := p2.SimpleQuery(context.Background(), "SELECT 1", func(ExtendedMessage) error { return nil }); !errors.Is(err, ErrSegmentInFlight) {
		t.Fatalf("SimpleQuery mid-segment returned %v, want ErrSegmentInFlight", err)
	}
}

// WIRE SAFETY: a PANICKING emitter must not leave the wire
// looking reusable. The response tail is unread, so the handle must be poisoned
// BEFORE the panic propagates, inEmit must be restored (a later Discard must not
// skip its barrier on stale certification), and the next call must be refused
// WITHOUT reading — a driver that read on would return the previous query's
// frames as the new query's answer. The panic value reaches the caller unchanged.
func TestSimpleQuery_EmitterPanicPoisonsBeforePropagating(t *testing.T) {
	t.Parallel()
	type sentinel struct{ why string }
	w := &scriptedWire{msgs: []pgproto3.BackendMessage{rowDesc("n"), row("1"), cc("SELECT 1"), rfq('I'),
		/* would be the "second query" if the driver read on */ cc("SELECT 0"), rfq('I')}}
	p := simpleHandle(t, w)
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_, _ = p.SimpleQuery(context.Background(), "SELECT n FROM t", func(m ExtendedMessage) error {
			panic(sentinel{"consumer bug"})
		})
	}()
	got, ok := recovered.(sentinel)
	if !ok || got.why != "consumer bug" {
		t.Fatalf("recovered %#v, want the emitter's own panic value unchanged", recovered)
	}
	p.mu.Lock()
	poisoned, inEmit := p.poisoned, p.inEmit
	p.mu.Unlock()
	if !poisoned {
		t.Fatal("handle not poisoned after the emitter panicked with the response tail unread")
	}
	if inEmit {
		t.Fatal("inEmit left true after the panic; a later Discard would skip its barrier on stale certification")
	}
	readsBefore := w.callCount()
	st, err := p.SimpleQuery(context.Background(), "SELECT 2", func(ExtendedMessage) error { return nil })
	if !errors.Is(err, ErrPoisoned) {
		t.Fatalf("after a recovered emitter panic the next query returned (%c, %v), want ErrPoisoned", st, err)
	}
	if w.callCount() != readsBefore {
		t.Fatalf("the refused query still read %d frame(s) from the wire — it would have consumed the previous response", w.callCount()-readsBefore)
	}
}
