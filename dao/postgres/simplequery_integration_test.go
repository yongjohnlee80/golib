//go:build integration

// Live acceptance cells for ADR-0018 Amendment 1 (SimpleQuerier). Each names the
// criterion it witnesses. Verbatim fidelity (A1-C2) is proven against a SECOND,
// independent simple-protocol run of the same text through pgconn on its own
// connection: the bytes the server sends to psql are the bytes emit receives.
//
//	go test -race -tags integration -run 'TestSimpleQuery_Live' ./dao/postgres/
package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yongjohnlee80/golib/dao"
)

// reference runs sql over the simple protocol on an independent connection and
// returns pgconn's view of it: one Result per statement group.
func reference(t *testing.T, sql string) []*pgconn.Result {
	t.Helper()
	c, err := pgconn.Connect(bg(t), pgURL(t))
	if err != nil {
		t.Fatalf("reference connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	res, err := c.Exec(bg(t), sql).ReadAll()
	if err != nil {
		t.Fatalf("reference exec: %v", err)
	}
	return res
}

// collect runs SimpleQuery and copies every emitted message (DataRow values are
// borrowed, so they are cloned here — the RawRows rule applied by the test).
func collect(t *testing.T, p *pinnedConn, sql string) ([]ExtendedMessage, byte, error) {
	t.Helper()
	var got []ExtendedMessage
	st, err := p.SimpleQuery(bg(t), sql, func(m ExtendedMessage) error {
		if m.Kind == "DataRow" {
			vals := make([][]byte, len(m.Values))
			for i, v := range m.Values {
				if v != nil {
					vals[i] = bytes.Clone(v)
				}
			}
			m.Values = vals
		}
		got = append(got, m)
		return nil
	})
	return got, st, err
}

func ofKind(ms []ExtendedMessage, kind string) []ExtendedMessage {
	var out []ExtendedMessage
	for _, m := range ms {
		if m.Kind == kind {
			out = append(out, m)
		}
	}
	return out
}

// A1-C2: RowDescription (every descriptor field), DataRow bytes (NULL as nil,
// empty as non-nil empty), and the CommandComplete tag are the reference's, byte
// for byte, across the type zoo.
func TestSimpleQuery_Live_RowDescriptionRowsAndTagAreVerbatim(t *testing.T) {
	conn := openPG(t)
	table := fmt.Sprintf("sq_zoo_%d", pidSuffix())
	mustExec(t, conn, fmt.Sprintf(`CREATE TABLE %s (
		i int4, b bigint, t text, v varchar(12), n numeric(10,3), f float8, bo boolean,
		ts timestamptz, d date, by bytea, j jsonb, u uuid, arr int4[], nul text, emp text)`, table))
	t.Cleanup(func() { mustExec(t, conn, "DROP TABLE IF EXISTS "+table) })
	mustExec(t, conn, fmt.Sprintf(`INSERT INTO %s VALUES
		(1, 9007199254740993, 'x''y', 'short', 1234.500, 1.5e300, true, '2026-09-02T10:00:00Z', '2026-09-02',
		 '\x00ff', '{"k":[1,"two",null]}', 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11', '{1,2,3}', NULL, ''),
		(-2, -1, E'tab\there\nnl', 'ü€', -0.001, 'NaN', false, '1999-12-31T23:59:59.999999+05:30', '0001-01-01',
		 '\xdeadbeef', '[]', '00000000-0000-0000-0000-000000000000', '{}', NULL, '')`, table))
	sql := "SELECT *, i * 2 AS twice, NULL::int4 AS typed_null FROM " + table + " ORDER BY i"

	ref := reference(t, sql)
	if len(ref) != 1 {
		t.Fatalf("reference returned %d groups, want 1", len(ref))
	}
	got, st, err := collect(t, mustPin(t, conn), sql)
	if err != nil || st != 'I' {
		t.Fatalf("SimpleQuery: status %c err %v", st, err)
	}

	rd := ofKind(got, "RowDescription")
	if len(rd) != 1 || len(rd[0].Fields) != len(ref[0].FieldDescriptions) {
		t.Fatalf("RowDescription: got %d message(s) / %d fields, want 1 / %d", len(rd), len(rd[0].Fields), len(ref[0].FieldDescriptions))
	}
	for i, f := range rd[0].Fields {
		r := ref[0].FieldDescriptions[i]
		if f.Name != r.Name || f.TableOID != r.TableOID || f.ColumnAttr != r.TableAttributeNumber ||
			f.TypeOID != r.DataTypeOID || f.TypeSize != r.DataTypeSize || f.TypeModifier != r.TypeModifier || f.Format != r.Format {
			t.Fatalf("field %d differs: got %+v, reference {Name:%s TableOID:%d Attr:%d TypeOID:%d Size:%d Mod:%d Fmt:%d}",
				i, f, r.Name, r.TableOID, r.TableAttributeNumber, r.DataTypeOID, r.DataTypeSize, r.TypeModifier, r.Format)
		}
	}
	rows := ofKind(got, "DataRow")
	if len(rows) != len(ref[0].Rows) {
		t.Fatalf("DataRow count %d, reference %d", len(rows), len(ref[0].Rows))
	}
	for i, row := range rows {
		for j, v := range row.Values {
			rv := ref[0].Rows[i][j]
			if (v == nil) != (rv == nil) {
				t.Fatalf("row %d col %d NULL-ness differs: got nil=%v reference nil=%v", i, j, v == nil, rv == nil)
			}
			if !bytes.Equal(v, rv) {
				t.Fatalf("row %d col %d bytes differ: got %q reference %q", i, j, v, rv)
			}
		}
	}
	// The empty-string column must be non-nil empty, the NULL column nil.
	if rows[0].Values[14] == nil || len(rows[0].Values[14]) != 0 || rows[0].Values[13] != nil {
		t.Fatalf("empty/NULL distinction lost: emp=%#v nul=%#v", rows[0].Values[14], rows[0].Values[13])
	}
	cc := ofKind(got, "CommandComplete")
	if len(cc) != 1 || cc[0].Tag != ref[0].CommandTag.String() {
		t.Fatalf("CommandComplete %+v, reference tag %q", cc, ref[0].CommandTag.String())
	}
}

// A1-C2: multi-statement text yields one group per statement, in order, and ONE
// terminal ReadyForQuery — the consumer sees exactly what psql sees. The text
// mutates (INSERT, UPDATE), so the reference run and the SimpleQuery run each get
// their OWN fresh table: comparing two runs of non-idempotent text against one
// table compares different states, not the two paths.
func TestSimpleQuery_Live_MultiStatementYieldsGroupsInOrderAndOneReady(t *testing.T) {
	conn := openPG(t)
	text := func(table string) string {
		return "SELECT 1 AS a; INSERT INTO " + table + " VALUES (1),(2); SELECT n FROM " + table + " ORDER BY n; UPDATE " + table + " SET n = n + 1"
	}
	fresh := func(tag string) string {
		table := fmt.Sprintf("sq_multi_%s_%d", tag, pidSuffix())
		mustExec(t, conn, "CREATE TABLE "+table+" (n int4)")
		t.Cleanup(func() { mustExec(t, conn, "DROP TABLE IF EXISTS "+table) })
		return table
	}
	ref := reference(t, text(fresh("ref")))
	got, st, err := collect(t, mustPin(t, conn), text(fresh("raw")))
	if err != nil || st != 'I' {
		t.Fatalf("status %c err %v", st, err)
	}
	cc := ofKind(got, "CommandComplete")
	if len(cc) != len(ref) {
		t.Fatalf("%d CommandComplete tags, reference has %d groups", len(cc), len(ref))
	}
	for i := range cc {
		if cc[i].Tag != ref[i].CommandTag.String() {
			t.Fatalf("group %d tag %q, reference %q", i, cc[i].Tag, ref[i].CommandTag.String())
		}
	}
	if n := len(ofKind(got, "RowDescription")); n != 2 {
		t.Fatalf("%d RowDescriptions, want 2 (the two SELECTs)", n)
	}
	if n := len(ofKind(got, "DataRow")); n != 3 {
		t.Fatalf("%d DataRows, want 3", n)
	}
}

// A1-C2: an empty query yields EmptyQueryResponse, not an error.
func TestSimpleQuery_Live_EmptyQueryResponse(t *testing.T) {
	got, st, err := collect(t, mustPin(t, openPG(t)), "")
	if err != nil || st != 'I' {
		t.Fatalf("status %c err %v", st, err)
	}
	if len(got) != 1 || got[0].Kind != "EmptyQueryResponse" {
		t.Fatalf("emitted %+v, want exactly one EmptyQueryResponse", got)
	}
}

// A1-C2 / A1-C4: a server error is delivered as ErrorResponse protocol data with
// the PgError fields verbatim (code, message, position); the handle is neither
// poisoned nor left mid-segment.
func TestSimpleQuery_Live_ErrorResponseVerbatimAndNotPoison(t *testing.T) {
	sql := "SELECT 1; SELECT * FROM no_such_table_a1c2; SELECT 2"
	var refErr *pgconn.PgError
	c, err := pgconn.Connect(bg(t), pgURL(t))
	if err != nil {
		t.Fatalf("reference connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	if _, err := c.Exec(bg(t), sql).ReadAll(); !errors.As(err, &refErr) {
		t.Fatalf("reference did not fail with a PgError: %v", err)
	}
	p := mustPin(t, openPG(t))
	got, st, err := collect(t, p, sql)
	if err != nil || st != 'I' {
		t.Fatalf("status %c err %v; a target error is not a driver error", st, err)
	}
	er := ofKind(got, "ErrorResponse")
	if len(er) != 1 || er[0].Err == nil {
		t.Fatalf("ErrorResponse messages: %+v, want one with Err set", er)
	}
	if er[0].Err.Code != refErr.Code || er[0].Err.Message != refErr.Message || er[0].Err.Position != refErr.Position || er[0].Err.Severity != refErr.Severity {
		t.Fatalf("PgError differs: got {%s %q pos %d %s} reference {%s %q pos %d %s}",
			er[0].Err.Code, er[0].Err.Message, er[0].Err.Position, er[0].Err.Severity, refErr.Code, refErr.Message, refErr.Position, refErr.Severity)
	}
	// The simple protocol aborts the rest of the text: only the first SELECT completed.
	if n := len(ofKind(got, "CommandComplete")); n != 1 {
		t.Fatalf("%d CommandComplete tags after the error, want 1 (statements after the error are not run)", n)
	}
	if p.poisoned {
		t.Fatal("an ErrorResponse poisoned the handle")
	}
	if _, st, err := collect(t, p, "SELECT 3"); err != nil || st != 'I' {
		t.Fatalf("handle unusable after an ErrorResponse: %c %v", st, err)
	}
}

// A1-C2: ParameterStatus is forwarded as protocol data; SET is not control.
func TestSimpleQuery_Live_ParameterStatusIsForwardedAndSetIsNotControl(t *testing.T) {
	p := mustPin(t, openPG(t))
	got, st, err := collect(t, p, "SET application_name = 'a1c2_probe'")
	if err != nil || st != 'I' || p.poisoned {
		t.Fatalf("status %c err %v poisoned %v", st, err, p.poisoned)
	}
	ps := ofKind(got, "ParameterStatus")
	if len(ps) != 1 || ps[0].ParameterName != "application_name" || ps[0].ParameterValue != "a1c2_probe" {
		t.Fatalf("ParameterStatus %+v, want application_name=a1c2_probe", ps)
	}
	if cc := ofKind(got, "CommandComplete"); len(cc) != 1 || cc[0].Tag != "SET" {
		t.Fatalf("CommandComplete %+v, want one SET", cc)
	}
}

// A1-C3: results are unbounded — nothing is paged or truncated by the driver.
func TestSimpleQuery_Live_UnboundedResultIsDeliveredInFull(t *testing.T) {
	const n = 20000
	var rows int
	var last []byte
	st, err := mustPin(t, openPG(t)).SimpleQuery(bg(t), fmt.Sprintf("SELECT g FROM generate_series(1,%d) g", n), func(m ExtendedMessage) error {
		if m.Kind == "DataRow" {
			rows++
			last = bytes.Clone(m.Values[0])
		}
		return nil
	})
	if err != nil || st != 'I' {
		t.Fatalf("status %c err %v", st, err)
	}
	if rows != n || string(last) != fmt.Sprint(n) {
		t.Fatalf("delivered %d rows (last %q), want %d — the driver must not page or truncate", rows, last, n)
	}
}

// A1-C4: the normal case — SimpleQuery INSIDE the owned transaction returns T,
// and the owner's Commit makes the work durable.
func TestSimpleQuery_Live_InsideOwnedTransactionThenOwnerCommits(t *testing.T) {
	conn := openPG(t)
	table := fmt.Sprintf("sq_tx_%d", pidSuffix())
	mustExec(t, conn, "CREATE TABLE "+table+" (n int4)")
	t.Cleanup(func() { mustExec(t, conn, "DROP TABLE IF EXISTS "+table) })
	p := mustPin(t, conn)
	tx, err := p.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatalf("BeginSessionTx: %v", err)
	}
	got, st, err := collect(t, p, "INSERT INTO "+table+" VALUES (7)")
	if err != nil || st != 'T' || p.poisoned {
		t.Fatalf("inside tx: status %c err %v poisoned %v; want T nil false", st, err, p.poisoned)
	}
	if cc := ofKind(got, "CommandComplete"); len(cc) != 1 || cc[0].Tag != "INSERT 0 1" {
		t.Fatalf("CommandComplete %+v, want INSERT 0 1", cc)
	}
	if err := tx.CommitContext(bg(t)); err != nil {
		t.Fatalf("owner Commit after a raw statement inside the tx: %v", err)
	}
	if got := scalar(t, conn, "SELECT count(*) FROM "+table); got != "1" {
		t.Fatalf("committed row count %s, want 1", got)
	}
}

// A1-C4: a statement failing inside the owned transaction is T→E — reported
// through the status, not poison — and the owner's Rollback recovers.
func TestSimpleQuery_Live_FailureInsideOwnedTransactionIsAbortedNotPoison(t *testing.T) {
	p := mustPin(t, openPG(t))
	tx, err := p.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatalf("BeginSessionTx: %v", err)
	}
	got, st, err := collect(t, p, "SELECT 1/0")
	if err != nil || st != 'E' || p.poisoned {
		t.Fatalf("status %c err %v poisoned %v; want E nil false", st, err, p.poisoned)
	}
	if er := ofKind(got, "ErrorResponse"); len(er) != 1 || er[0].Err.Code != "22012" {
		t.Fatalf("ErrorResponse %+v, want division_by_zero 22012", er)
	}
	if err := tx.RollbackContext(bg(t)); err != nil {
		t.Fatalf("owner Rollback of the aborted tx: %v", err)
	}
	if _, st, err := collect(t, p, "SELECT 1"); err != nil || st != 'I' {
		t.Fatalf("handle after rollback: %c %v", st, err)
	}
}

// A1-C4: raw BEGIN from idle is transaction control on the wrong face — the
// server confirms it (tag BEGIN, status T), the handle is poisoned, and the
// next call is refused; Discard reclaims it.
func TestSimpleQuery_Live_RawBeginFromIdlePoisons(t *testing.T) {
	p := mustPin(t, openPG(t))
	_, _, err := collect(t, p, "BEGIN")
	if !errors.Is(err, ErrTransactionControlOnRawFace) {
		t.Fatalf("raw BEGIN returned %v, want ErrTransactionControlOnRawFace", err)
	}
	if _, _, err := collect(t, p, "SELECT 1"); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("after raw BEGIN the next call returned %v, want ErrPoisoned", err)
	}
}

// A1-C4: raw COMMIT inside the owned transaction — the same-class case the tag
// scan exists for — poisons the handle, and the OWNER sees a terminal error
// rather than believing its transaction is still open.
func TestSimpleQuery_Live_RawCommitInsideOwnedTransactionPoisonsAndOwnerSees(t *testing.T) {
	conn := openPG(t)
	table := fmt.Sprintf("sq_rawcommit_%d", pidSuffix())
	mustExec(t, conn, "CREATE TABLE "+table+" (n int4)")
	t.Cleanup(func() { mustExec(t, conn, "DROP TABLE IF EXISTS "+table) })
	p := mustPin(t, conn)
	tx, err := p.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatalf("BeginSessionTx: %v", err)
	}
	if _, _, err := collect(t, p, "INSERT INTO "+table+" VALUES (1)"); err != nil {
		t.Fatalf("insert inside tx: %v", err)
	}
	_, _, err = collect(t, p, "COMMIT; BEGIN") // net status T→T: only the tag stream sees it
	if !errors.Is(err, ErrTransactionControlOnRawFace) {
		t.Fatalf("raw COMMIT;BEGIN inside the owned tx returned %v, want ErrTransactionControlOnRawFace", err)
	}
	if !p.poisoned {
		t.Fatal("handle not poisoned after raw COMMIT")
	}
	cerr := tx.CommitContext(bg(t))
	if cerr == nil {
		t.Fatal("owner Commit succeeded on a poisoned handle; the owner must learn its transaction state is unprovable")
	}
	t.Logf("owner Commit on the poisoned handle reported: %v", cerr)
}

// A1-C3: on a REAL handle, SimpleQuery mid-segment is refused, and the segment
// is unaffected — Sync completes it normally.
func TestSimpleQuery_Live_RefusedMidSegmentAndSegmentSurvives(t *testing.T) {
	p := mustPin(t, openPG(t))
	for _, op := range []ExtendedOp{ParseOp("", "SELECT 42", nil), BindOp("", "", nil, nil, nil), ExecuteOp("", 0)} {
		if err := p.Send(bg(t), op); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
	if _, _, err := collect(t, p, "SELECT 1"); !errors.Is(err, ErrSegmentInFlight) {
		t.Fatalf("SimpleQuery mid-segment returned %v, want ErrSegmentInFlight", err)
	}
	if _, err := p.Sync(bg(t)); err != nil {
		t.Fatalf("Sync after the refused call: %v — the segment must be untouched", err)
	}
	if _, st, err := collect(t, p, "SELECT 1"); err != nil || st != 'I' {
		t.Fatalf("SimpleQuery after the segment: %c %v", st, err)
	}
}

// pidSuffix keeps table names unique across concurrent runs against one database.
func pidSuffix() int { return os.Getpid() }

// A1-C3: Discard from INSIDE emit — the unconditional terminal operation on the
// goroutine that holds the wire. It must return, the handle must be terminal,
// SimpleQuery must return the terminal error without reading further, and the
// pool member must be destroyed (never recycled dirty). This is the cell lector's
// MF1 (PR #22 r0) proved deadlocked at b86f649.
func TestSimpleQuery_Live_DiscardFromInsideEmitDoesNotDeadlock(t *testing.T) {
	p := mustPin(t, openPG(t))
	var afterDiscard int
	done := make(chan struct{})
	var st byte
	var err error
	go func() {
		defer close(done)
		st, err = p.SimpleQuery(bg(t), "SELECT g FROM generate_series(1,5) g", func(m ExtendedMessage) error {
			if m.Kind == "DataRow" && afterDiscard == 0 {
				p.Discard() // must return
				afterDiscard = 1
				return nil
			}
			if afterDiscard > 0 {
				afterDiscard++ // any further emit after Discard is a defect
			}
			return nil
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("SimpleQuery did not return after the emitter called Discard: deadlock on wireMu")
	}
	if !errors.Is(err, ErrPoisoned) {
		t.Fatalf("SimpleQuery returned (%c, %v) after an in-emit Discard, want ErrPoisoned", st, err)
	}
	if afterDiscard != 1 {
		t.Fatalf("emit ran %d more time(s) after Discard; delivery must stop at the callback that discarded", afterDiscard-1)
	}
	if _, _, err := collect(t, p, "SELECT 1"); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("handle after in-emit Discard returned %v, want ErrPoisoned (terminal)", err)
	}
}

// A1-C3: Discard from ANOTHER goroutine while the emitter is running must not
// close the connection under an in-flight read: the decision to skip the barrier
// is taken atomically with the reader's own state, so either the reader stops
// before its next read or Discard waits behind it. Repeated to shake the race.
func TestSimpleQuery_Live_ConcurrentDiscardDuringEmitIsRaceFree(t *testing.T) {
	for i := 0; i < 20; i++ {
		p := mustPin(t, openPG(t))
		inEmit := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			_, err := p.SimpleQuery(bg(t), "SELECT g FROM generate_series(1,50) g", func(m ExtendedMessage) error {
				if m.Kind == "DataRow" && string(m.Values[0]) == "3" {
					close(inEmit)
					<-release
				}
				return nil
			})
			done <- err
		}()
		<-inEmit
		go func() { p.Discard() }()
		close(release)
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, ErrPoisoned) {
				var de *dispatchedError
				if !errors.As(err, &de) {
					t.Fatalf("iteration %d: SimpleQuery returned %v; want nil, ErrPoisoned, or a dispatched transport error", i, err)
				}
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: SimpleQuery did not return", i)
		}
	}
}
