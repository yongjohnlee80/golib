//go:build integration

// Live acceptance suite for session-pinned connections (with raw
// extended-protocol execution). One test per acceptance criterion, named for it. It
// requires a reachable PostgreSQL; set TEST_PGURL and run:
//
//	go test -race -tags integration -run 'TestPinned_' ./dao/postgres/
//
// Every table it creates is dropped again; nothing else is touched. The harness helpers
// (pgURL, openPG, mustExec, scalar, setupSlowCommit, assertPoolDrained, lyingCtx) are
// shared with the transaction-options suite in this package.
package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/yongjohnlee80/golib/dao"
)

// --- harness -------------------------------------------------------------------------

// bg is a per-test bounded context: every blocking call in this suite is bounded so a
// regression hangs a cell for at most this long, never the whole run. It is created ONCE
// per test and memoized — a fresh context per call would retain a timer and a cleanup
// closure per call, which in a 20000-row streaming loop is 21 MB of harness garbage that
// the bounded-memory cell would misattribute to the driver (that is exactly how the
// first live run of the bounded-memory cell failed).
func bg(t *testing.T) context.Context {
	if v, ok := bgCtxs.Load(t); ok {
		return v.(context.Context)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	if v, loaded := bgCtxs.LoadOrStore(t, ctx); loaded {
		cancel()
		return v.(context.Context)
	}
	t.Cleanup(func() { cancel(); bgCtxs.Delete(t) })
	return ctx
}

var bgCtxs sync.Map // *testing.T → context.Context

// mustPin pins a connection on conn and registers the deferred Discard every
// PinSessionConn success must carry.
func mustPin(t *testing.T, conn dao.DataConn) *pinnedConn {
	t.Helper()
	pc, err := PinSessionConn(bg(t), conn)
	if err != nil {
		t.Fatalf("PinSessionConn: %v", err)
	}
	t.Cleanup(pc.Discard)
	h, ok := pc.(*pinnedConn)
	if !ok {
		t.Fatalf("PinSessionConn returned %T, want *pinnedConn", pc)
	}
	return h
}

func wantTuple(t *testing.T, p *pinnedConn, out outboundState, in inboundState, at string) {
	t.Helper()
	gotOut, gotIn := tuple(p)
	if gotOut != out || gotIn != in {
		t.Fatalf("%s: state = (%d, %d), want (%d, %d)", at, gotOut, gotIn, out, in)
	}
}

func mustSend(t *testing.T, p PinnedConn, ops ...ExtendedOp) {
	t.Helper()
	for _, op := range ops {
		if err := p.Send(bg(t), op); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}
}

func mustFlush(t *testing.T, p PinnedConn) {
	t.Helper()
	if err := p.Flush(bg(t)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

func mustSync(t *testing.T, p PinnedConn) byte {
	t.Helper()
	st, err := p.Sync(bg(t))
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	return st
}

// expect receives one message and asserts its Kind.
func expect(t *testing.T, p PinnedConn, kind string) ExtendedMessage {
	t.Helper()
	m, err := p.Receive(bg(t))
	if err != nil {
		t.Fatalf("Receive (want %s): %v", kind, err)
	}
	if m.Kind != kind {
		t.Fatalf("Receive = %s (%+v), want %s", m.Kind, m, kind)
	}
	return m
}

// segment runs one complete unnamed extended segment — Parse, Bind (text params),
// Describe portal, Execute, Flush — and returns every message up to and including the
// group's completion, then Syncs and returns the status byte.
func segment(t *testing.T, p PinnedConn, sql string, params ...string) ([]ExtendedMessage, byte) {
	t.Helper()
	vals := make([][]byte, len(params))
	fmts := make([]int16, len(params))
	for i, s := range params {
		vals[i] = []byte(s)
	}
	mustSend(t, p,
		ParseOp("", sql, nil),
		BindOp("", "", vals, fmts, nil),
		DescribePortalOp(""),
		ExecuteOp("", 0),
	)
	mustFlush(t, p)
	msgs := drainGroup(t, p)
	return msgs, mustSync(t, p)
}

// drainGroup receives until a group-ending message (CommandComplete, EmptyQueryResponse,
// PortalSuspended, ErrorResponse), copying DataRow values so callers may keep them.
func drainGroup(t *testing.T, p PinnedConn) []ExtendedMessage {
	t.Helper()
	var out []ExtendedMessage
	for {
		m, err := p.Receive(bg(t))
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		if m.Kind == "DataRow" {
			cp := make([][]byte, len(m.Values))
			for i, v := range m.Values {
				if v != nil {
					cp[i] = bytes.Clone(v)
				}
			}
			m.Values = cp
		}
		out = append(out, m)
		switch m.Kind {
		case "CommandComplete", "EmptyQueryResponse", "PortalSuspended", "ErrorResponse":
			return out
		}
	}
}

// rowsOf extracts the DataRow values from a drained group.
func rowsOf(msgs []ExtendedMessage) [][][]byte {
	var rows [][][]byte
	for _, m := range msgs {
		if m.Kind == "DataRow" {
			rows = append(rows, m.Values)
		}
	}
	return rows
}

// lastKind returns the Kind of the last message.
func lastKind(msgs []ExtendedMessage) string { return msgs[len(msgs)-1].Kind }

// traceFrames attaches pgproto3's tracer to the pinned frontend and returns the
// buffer plus a stop func. Lines are "<F|B>\t<MessageType>\t<len>...".
func traceFrames(p *pinnedConn) (*bytes.Buffer, func()) {
	var buf bytes.Buffer
	p.frontend.Trace(&buf, pgproto3.TracerOptions{SuppressTimestamps: true})
	return &buf, p.frontend.Untrace
}

// countFrontend counts frontend frames of one message type in a trace.
func countFrontend(trace, msgType string) int {
	re := regexp.MustCompile(`(?m)^F\t` + regexp.QuoteMeta(msgType) + `\t`)
	return len(re.FindAllStringIndex(trace, -1))
}

// withWatchdog runs f and fails the test if it has not returned within d — the
// deterministic immediacy check: a guarded call that WAITS on a held segment trips it.
func withWatchdog(t *testing.T, d time.Duration, what string, f func() error) error {
	t.Helper()
	res := make(chan error, 1)
	go func() { res <- f() }()
	select {
	case err := <-res:
		return err
	case <-time.After(d):
		t.Fatalf("%s did not return within %v: the guard WAITED on the segment instead of refusing", what, d)
		return nil
	}
}

// holdAdvisoryLock takes a transaction-level advisory lock on a separate pool and
// returns a func that releases it (by rolling the transaction back).
func holdAdvisoryLock(t *testing.T, key int) func() {
	t.Helper()
	locker := openPG(t)
	tx, err := locker.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatalf("locker BeginSessionTx: %v", err)
	}
	if _, err := tx.ExecContext(bg(t), fmt.Sprintf("SELECT pg_advisory_xact_lock(%d)", key)); err != nil {
		t.Fatalf("pg_advisory_xact_lock: %v", err)
	}
	var once sync.Once
	release := func() { once.Do(func() { _ = tx.RollbackContext(context.Background()) }) }
	t.Cleanup(release)
	return release
}

// poisonByTimeout drives the handle into the poisoned state deterministically: a Parse
// is queued (building — Receive is legal in that state) but never flushed, so the server
// has nothing to answer and the bounded Receive times out — a transport-level outcome.
func poisonByTimeout(t *testing.T, p PinnedConn) {
	t.Helper()
	mustSend(t, p, ParseOp("", "SELECT 1", nil))
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := p.Receive(ctx)
	if err == nil {
		t.Fatal("Receive returned a message with nothing flushed; the poison setup is wrong")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Receive err = %v, want the raw context deadline error", err)
	}
}

// --- capability present, miss honest -------------------------------------------------

// The LIVE half: the real driver has the capability through the typed helper
// and through the probe. The miss half is TestPinned_CapabilityMissIsErrUnsupported.
func TestPinned_C9_CapabilityPresentOnPostgres(t *testing.T) {
	conn := openPG(t)
	if !SupportsSessionPinning(conn) {
		t.Fatal("SupportsSessionPinning(postgres) = false")
	}
	pc := mustPin(t, conn)
	if _, st := segment(t, pc, "SELECT 1"); st != 'I' {
		t.Fatalf("status = %c, want I", st)
	}
	if err := pc.Release(bg(t)); err != nil {
		t.Fatalf("Release from quiescent: %v", err)
	}
	// The lease went back to the pool as a REUSABLE member: still one total, one idle.
	if s := conn.pool.Stat(); s.TotalConns() != 1 || s.IdleConns() != 1 {
		t.Fatalf("after Release: total=%d idle=%d, want 1/1 (recycled, not destroyed)", s.TotalConns(), s.IdleConns())
	}
	// …and the handle is terminal; the full face sweep is
	// TestPinned_MF1_ReleasedHandleIsTerminal.
	if err := pc.Send(bg(t), ParseOp("", "SELECT 1", nil)); !errors.Is(err, ErrReleased) {
		t.Fatalf("Send after Release = %v, want ErrReleased", err)
	}
}

// --- asynchrony + bounded memory -----------------------------------------------------

func TestPinned_C2_QueueThenStreamInBoundedMemory(t *testing.T) {
	conn := openPG(t)
	pc := mustPin(t, conn)

	const rows = 20000
	// Four frames queued with NO blocking and NO flush; the outbound track builds.
	mustSend(t, pc,
		ParseOp("", "SELECT g, repeat('x', 1000) FROM generate_series(1, $1::int) g", nil),
		BindOp("", "", [][]byte{[]byte(fmt.Sprint(rows))}, []int16{0}, nil),
		DescribePortalOp(""),
		ExecuteOp("", 0),
	)
	wantTuple(t, pc, building, noInbound, "after four Sends")
	mustFlush(t, pc)
	wantTuple(t, pc, flushed, noInbound, "after Flush")

	expect(t, pc, "ParseComplete")
	wantTuple(t, pc, flushed, receiving, "after first Receive")
	expect(t, pc, "BindComplete")
	rd := expect(t, pc, "RowDescription")
	if len(rd.Fields) != 2 || rd.Fields[0].Name != "g" {
		t.Fatalf("RowDescription = %+v", rd.Fields)
	}

	// Stream one row per Receive. Heap growth between early and late rows must stay far
	// below what accumulation would cost (~19 MB of payload); the driver holds at most
	// one row buffer. Mutation that breaks this: retain every DataRow's Values in a
	// driver-side slice — the delta climbs past 18 MB and the assertion fails.
	var ms runtime.MemStats
	var early, late uint64
	ctx := bg(t)
	for i := 1; i <= rows; i++ {
		m, err := pc.Receive(ctx)
		if err != nil || m.Kind != "DataRow" {
			t.Fatalf("row %d: %s %v", i, m.Kind, err)
		}
		if string(m.Values[0]) != fmt.Sprint(i) || len(m.Values[1]) != 1000 {
			t.Fatalf("row %d = %q/%d bytes", i, m.Values[0], len(m.Values[1]))
		}
		switch i {
		case 500:
			runtime.GC()
			runtime.ReadMemStats(&ms)
			early = ms.HeapAlloc
		case rows - 500:
			runtime.GC()
			runtime.ReadMemStats(&ms)
			late = ms.HeapAlloc
		}
	}
	if delta := int64(late) - int64(early); delta > 4<<20 {
		t.Fatalf("heap grew %d bytes across %d streamed rows; rows are being accumulated", delta, rows-1000)
	}
	cc := expect(t, pc, "CommandComplete")
	if cc.Tag != fmt.Sprintf("SELECT %d", rows) {
		t.Fatalf("tag = %q", cc.Tag)
	}
	if st := mustSync(t, pc); st != 'I' {
		t.Fatalf("status = %c", st)
	}
	wantTuple(t, pc, idleOut, noInbound, "after Sync")
}

// --- the state machine ---------------------------------------------------------------

func TestPinned_C3_ErrorResponseIsProtocolDataThenSyncReopens(t *testing.T) {
	conn := openPG(t)
	pc := mustPin(t, conn)

	mustSend(t, pc, ParseOp("", "SELEC 1", nil))
	mustFlush(t, pc)
	m := expect(t, pc, "ErrorResponse")
	if m.Err == nil || m.Err.Code != "42601" {
		t.Fatalf("ErrorResponse = %+v, want SQLSTATE 42601", m.Err)
	}
	wantTuple(t, pc, flushed, discarding, "after ErrorResponse")

	// While discarding: only Sync is legal.
	if err := pc.Send(bg(t), ParseOp("", "SELECT 1", nil)); !errors.Is(err, ErrSegmentInFlight) {
		t.Fatalf("Send while discarding err = %v, want ErrSegmentInFlight", err)
	}
	if _, err := pc.Receive(bg(t)); !errors.Is(err, ErrSegmentInFlight) {
		t.Fatalf("Receive while discarding err = %v, want ErrSegmentInFlight", err)
	}
	if _, err := pc.BeginSessionTx(bg(t), dao.TxOptions{}); !errors.Is(err, ErrSegmentInFlight) {
		t.Fatalf("BeginSessionTx while discarding err = %v, want ErrSegmentInFlight", err)
	}
	if st := mustSync(t, pc); st != 'I' {
		t.Fatalf("status = %c, want I", st)
	}
	wantTuple(t, pc, idleOut, noInbound, "after the single Sync")

	// The wire is reopened: a normal segment works.
	msgs, st := segment(t, pc, "SELECT 42")
	if st != 'I' || string(rowsOf(msgs)[0][0]) != "42" {
		t.Fatalf("post-recovery segment: status=%c rows=%v", st, rowsOf(msgs))
	}
}

func TestPinned_C3_TransportErrorPoisonsEverythingButDiscard(t *testing.T) {
	conn := openPG(t)
	pc := mustPin(t, conn)
	poisonByTimeout(t, pc)

	for name, op := range map[string]func() error{
		"Send":  func() error { return pc.Send(bg(t), ParseOp("", "SELECT 1", nil)) },
		"Flush": func() error { return pc.Flush(bg(t)) },
		"Receive": func() error {
			_, err := pc.Receive(bg(t))
			return err
		},
		"Sync": func() error {
			_, err := pc.Sync(bg(t))
			return err
		},
		"BeginSessionTx": func() error {
			_, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
			return err
		},
		"Release": func() error { return pc.Release(bg(t)) },
	} {
		if err := op(); !errors.Is(err, ErrPoisoned) {
			t.Errorf("%s after poison err = %v, want ErrPoisoned", name, err)
		}
	}
	// Discard is legal, idempotent, and CLOSES the dirty member — the pool ends empty
	// rather than holding a connection with an unread response on it.
	pc.Discard()
	pc.Discard()
	assertPoolDrained(t, conn)
	if got := scalar(t, conn, "SELECT 'healthy'"); got != "healthy" {
		t.Fatalf("pool after discard: %q", got)
	}
}

// A ReadyForQuery reaching Receive is a consumer contract violation and is reported as a
// typed error, not absorbed. The vocabulary has no Sync op, so the only way to produce
// one here is to queue the raw frame on the frontend directly (white-box).
func TestPinned_C3_PrematureReadyForQueryIsTyped(t *testing.T) {
	conn := openPG(t)
	pc := mustPin(t, conn)

	mustSend(t, pc, ParseOp("", "SELECT 1", nil))
	pc.frontend.SendSync(&pgproto3.Sync{}) // the consumer "sent" a Sync frame outside the seam
	mustFlush(t, pc)
	expect(t, pc, "ParseComplete")
	_, err := pc.Receive(bg(t))
	if !errors.Is(err, ErrPrematureReadyForQuery) {
		t.Fatalf("Receive err = %v, want ErrPrematureReadyForQuery", err)
	}
}

// --- the fault-state matrix through the shared classifier ----------------------------

func TestPinned_C4_FaultStateMatrix(t *testing.T) {
	t.Run("state 1: pre-dispatch cancellation leaves the handle open", func(t *testing.T) {
		conn := openPG(t)
		pc := mustPin(t, conn)
		tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
		if err != nil {
			t.Fatal(err)
		}
		dead, cancel := context.WithCancel(context.Background())
		cancel()
		err = tx.CommitContext(dead)
		if !errors.Is(err, context.Canceled) || errors.Is(err, dao.ErrTxRolledBack) || errors.Is(err, dao.ErrTxOutcomeUnknown) {
			t.Fatalf("err = %v, want the raw context error and no outcome claim", err)
		}
		if err := tx.RollbackContext(bg(t)); err != nil {
			t.Fatalf("the handle should still be open: %v", err)
		}
		if err := tx.RollbackContext(bg(t)); !errors.Is(err, dao.ErrTransactionClosed) {
			t.Fatalf("second rollback err = %v, want ErrTransactionClosed", err)
		}
		if err := pc.Release(bg(t)); err != nil {
			t.Fatalf("Release after a clean rollback: %v", err)
		}
	})

	t.Run("state 2: dispatched but proven never written", func(t *testing.T) {
		conn := openPG(t)
		pc := mustPin(t, conn)
		tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
		if err != nil {
			t.Fatal(err)
		}
		dead, cancel := context.WithCancel(context.Background())
		cancel()
		err = tx.CommitContext(&lyingCtx{Context: dead})
		if !errors.Is(err, dao.ErrTxRolledBack) || errors.Is(err, dao.ErrTxOutcomeUnknown) {
			t.Fatalf("err = %v, want ErrTxRolledBack only", err)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cause not preserved: %v", err)
		}
		if err := tx.RollbackContext(bg(t)); !errors.Is(err, dao.ErrTransactionClosed) {
			t.Fatalf("post-dispatch rollback err = %v, want ErrTransactionClosed", err)
		}
		// Nothing was written, so the server transaction is still open: Release must
		// refuse, and Discard must close the member rather than recycle it.
		if err := pc.Release(bg(t)); !errors.Is(err, ErrTxStillOpen) {
			t.Fatalf("Release err = %v, want ErrTxStillOpen", err)
		}
		pc.Discard()
		assertPoolDrained(t, conn)
	})

	t.Run("state 3a: COMMIT answered with ROLLBACK tag", func(t *testing.T) {
		conn := openPG(t)
		pc := mustPin(t, conn)
		tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
		if err != nil {
			t.Fatal(err)
		}
		// Abort the transaction through the extended face, then finalize.
		msgs, st := segment(t, pc, "SELECT 1/0")
		if lastKind(msgs) != "ErrorResponse" || st != 'E' {
			t.Fatalf("abort segment: last=%s status=%c", lastKind(msgs), st)
		}
		err = tx.CommitContext(bg(t))
		if !errors.Is(err, dao.ErrTxRolledBack) || errors.Is(err, dao.ErrTxOutcomeUnknown) {
			t.Fatalf("err = %v, want ErrTxRolledBack only", err)
		}
		if !errors.Is(err, pgx.ErrTxCommitRollback) {
			t.Fatalf("cause = %v, want pgx.ErrTxCommitRollback preserved", err)
		}
		if err := pc.Release(bg(t)); err != nil {
			t.Fatalf("Release after a server-finalized transaction: %v", err)
		}
	})

	t.Run("state 3b: COMMIT answered with ErrorResponse", func(t *testing.T) {
		conn := openPG(t)
		mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0018_def`)
		mustExec(t, conn, `CREATE TABLE golib_adr0018_def (id int, CONSTRAINT golib_adr0018_def_u UNIQUE (id) DEFERRABLE INITIALLY DEFERRED)`)
		t.Cleanup(func() { mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0018_def`) })

		pc := mustPin(t, conn)
		tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if _, err := tx.ExecContext(bg(t), `INSERT INTO golib_adr0018_def VALUES ($1)`, 1); err != nil {
				t.Fatalf("deferred insert %d: %v", i, err)
			}
		}
		err = tx.CommitContext(bg(t))
		if !errors.Is(err, dao.ErrTxRolledBack) || errors.Is(err, dao.ErrTxOutcomeUnknown) {
			t.Fatalf("err = %v, want ErrTxRolledBack only", err)
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			t.Fatalf("cause = %v, want *pgconn.PgError 23505 preserved", err)
		}
	})

	t.Run("state 4: response lost is unknown", func(t *testing.T) {
		conn := openPG(t)
		setupSlowCommit(t, conn)
		pc := mustPin(t, conn)
		tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(bg(t), `INSERT INTO golib_adr0017_slow VALUES (1)`); err != nil {
			t.Fatalf("insert: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		start := time.Now()
		err = tx.CommitContext(ctx)
		if el := time.Since(start); el > 2*time.Second {
			t.Fatalf("commit not interrupted (%v)", el)
		}
		if !errors.Is(err, dao.ErrTxOutcomeUnknown) || errors.Is(err, dao.ErrTxRolledBack) {
			t.Fatalf("err = %v, want ErrTxOutcomeUnknown only (a lost answer must never read as a definite rollback)", err)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cause not preserved: %v", err)
		}
		if err := tx.CommitContext(bg(t)); !errors.Is(err, dao.ErrTransactionClosed) {
			t.Fatalf("second commit err = %v", err)
		}
		// The wire is poisoned; Discard closes it.
		if err := pc.Release(bg(t)); !errors.Is(err, ErrPoisoned) {
			t.Fatalf("Release err = %v, want ErrPoisoned", err)
		}
		pc.Discard()
		assertPoolDrained(t, conn)
	})
}

// --- one wire, one backend transaction -----------------------------------------------

func TestPinned_C5_RawSegmentAndPinnedTxShareTheBackendTransaction(t *testing.T) {
	conn := openPG(t)
	pc := mustPin(t, conn)
	tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.RollbackContext(context.Background()) }()

	msgs, st := segment(t, pc, "SELECT txid_current()")
	if st != 'T' {
		t.Fatalf("status = %c, want T (inside the transaction)", st)
	}
	viaRaw := string(rowsOf(msgs)[0][0])
	viaTx := scalar(t, tx, "SELECT txid_current()")
	if viaRaw != viaTx {
		t.Fatalf("raw segment xid %s != pinned tx xid %s: two connections", viaRaw, viaTx)
	}
	// Control: the pool path is a DIFFERENT transaction.
	if viaPool := scalar(t, conn, "SELECT txid_current()"); viaPool == viaRaw {
		t.Fatalf("control failed: pool xid %s equals the pinned xid", viaPool)
	}
}

// --- refusal, not serialization ------------------------------------------------------

func TestPinned_C6_GuardsRefuseImmediatelyWhileBarrierHeld(t *testing.T) {
	const key = 424218
	releaseLock := holdAdvisoryLock(t, key)

	conn := openPG(t)
	pc := mustPin(t, conn)
	tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// The segment's Execute blocks server-side on the lock: the barrier is closed and
	// provably stays closed until releaseLock. Parse and Bind are flushed FIRST — the
	// server processes frames in order, so a Flush queued behind a blocking Execute is
	// never reached and their acknowledgements would stay in its output buffer.
	mustSend(t, pc,
		ParseOp("", fmt.Sprintf("SELECT pg_advisory_xact_lock(%d)", key), nil),
		BindOp("", "", nil, nil, nil),
	)
	mustFlush(t, pc)
	expect(t, pc, "ParseComplete")
	expect(t, pc, "BindComplete")
	mustSend(t, pc, ExecuteOp("", 0)) // flushed→building with inbound preserved
	wantTuple(t, pc, building, receiving, "resume-shaped Send mid-group")
	mustFlush(t, pc)
	wantTuple(t, pc, flushed, receiving, "mid-segment, Execute blocked on the barrier")

	blocked := make(chan error, 1)
	go func() {
		_, err := pc.Receive(bg(t)) // blocks on the barrier
		blocked <- err
	}()

	guarded := map[string]func() error{
		"BeginSessionTx": func() error { _, e := pc.BeginSessionTx(bg(t), dao.TxOptions{}); return e },
		"Release":        func() error { return pc.Release(bg(t)) },
		"CommitContext":  func() error { return tx.CommitContext(bg(t)) },
		"RollbackContext": func() error {
			return tx.RollbackContext(bg(t))
		},
		"Commit":   tx.Commit,
		"Rollback": tx.Rollback,
		"ExecContext": func() error {
			_, e := tx.ExecContext(bg(t), "SELECT 1")
			return e
		},
		"QueryContext": func() error {
			_, e := tx.QueryContext(bg(t), "SELECT 1")
			return e
		},
	}
	for name, f := range guarded {
		err := withWatchdog(t, 3*time.Second, name, f)
		if !errors.Is(err, ErrSegmentInFlight) {
			t.Fatalf("%s mid-segment err = %v, want ErrSegmentInFlight", name, err)
		}
	}
	// The barrier is still closed: the blocked Receive has not returned.
	select {
	case err := <-blocked:
		t.Fatalf("the segment progressed while the barrier was held (Receive returned %v)", err)
	default:
	}

	// Open the barrier; the segment completes coherently and the transaction — which
	// no refused finalizer was allowed to close — finalizes normally.
	releaseLock()
	if err := <-blocked; err != nil {
		t.Fatalf("Receive after the barrier opened: %v", err)
	}
	expect(t, pc, "CommandComplete")
	if st := mustSync(t, pc); st != 'T' {
		t.Fatalf("status = %c, want T", st)
	}
	if err := tx.RollbackContext(bg(t)); err != nil {
		t.Fatalf("RollbackContext after the segment: %v (a refused finalizer must not have closed the handle)", err)
	}
}

// --- no lease leak -------------------------------------------------------------------

func TestPinned_C7_PoisonAndDiscardCyclesNeverLeakOrRecycleDirty(t *testing.T) {
	conn := openPG(t, MaxOpenConns(1))
	for i := 0; i < 6; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pc, err := PinSessionConn(ctx, conn)
		cancel()
		if err != nil {
			t.Fatalf("cycle %d: PinSessionConn: %v (a leaked lease blocks the single-slot pool)", i, err)
		}
		poisonByTimeout(t, pc)
		pc.Discard()
		// Closed, not recycled: with one slot, a recycled dirty member would stay as one
		// idle connection; a closed one leaves the pool empty.
		assertPoolDrained(t, conn)
	}
	// The next consumer sees a healthy wire.
	pc := mustPin(t, conn)
	msgs, st := segment(t, pc, "SELECT 'clean'")
	if st != 'I' || string(rowsOf(msgs)[0][0]) != "clean" {
		t.Fatalf("post-cycle segment: %c %v", st, rowsOf(msgs))
	}
	if err := pc.Release(bg(t)); err != nil {
		t.Fatal(err)
	}
	if got := scalar(t, conn, "SELECT 1"); got != "1" {
		t.Fatal(got)
	}
}

// --- no cache collision, structurally ------------------------------------------------

func TestPinned_C8_NamedObjectsUnaffectedByPoolCacheChurn(t *testing.T) {
	conn := openPG(t, MaxOpenConns(6))
	pc := mustPin(t, conn)

	mustSend(t, pc, ParseOp("s1", "SELECT $1::int + 1", nil))
	mustFlush(t, pc)
	expect(t, pc, "ParseComplete")
	mustSync(t, pc)

	// Other pool members run cached high-level queries concurrently.
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				rows, err := conn.QueryContext(context.Background(), "SELECT $1::int + 2", g*100+i)
				if err != nil {
					t.Error(err)
					return
				}
				for rows.Next() {
				}
				_ = rows.Close()
			}
		}(g)
	}
	wg.Wait()

	// The named statement on the pinned wire behaves exactly as before.
	mustSend(t, pc, BindOp("", "s1", [][]byte{[]byte("41")}, []int16{0}, nil), ExecuteOp("", 0))
	mustFlush(t, pc)
	expect(t, pc, "BindComplete")
	if m := expect(t, pc, "DataRow"); string(m.Values[0]) != "42" {
		t.Fatalf("s1(41) = %q, want 42", m.Values[0])
	}
	expect(t, pc, "CommandComplete")
	mustSync(t, pc)

	// Unnamed lifetime: a pinned-tx finalizer (a simple-protocol COMMIT) destroys the
	// unnamed statement, observed through the raw face.
	tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustSend(t, pc, ParseOp("", "SELECT 7", nil))
	mustFlush(t, pc)
	expect(t, pc, "ParseComplete")
	mustSync(t, pc)
	mustSend(t, pc, DescribeStatementOp(""))
	mustFlush(t, pc)
	expect(t, pc, "ParameterDescription") // exists before the finalizer
	expect(t, pc, "RowDescription")
	mustSync(t, pc)
	if err := tx.CommitContext(bg(t)); err != nil {
		t.Fatal(err)
	}
	mustSend(t, pc, DescribeStatementOp(""))
	mustFlush(t, pc)
	if m := expect(t, pc, "ErrorResponse"); m.Err.Code != "26000" {
		t.Fatalf("Describe unnamed after COMMIT = %s, want 26000 (destroyed)", m.Err.Code)
	}
	mustSync(t, pc)
}

// --- Release terminalizes the handle -------------------------------------------------

// A successful Release hands the member back to the pool, so the handle must be
// TERMINAL: its frontend/pgConn/netConn still point at a connection the pool may have
// already given to someone else, and a surviving Send would queue bytes onto a
// stranger's wire. Every face refuses with ErrReleased, and the max-pool-1 arm proves
// the stale handle cannot touch the member after it has been re-borrowed.
func TestPinned_MF1_ReleasedHandleIsTerminal(t *testing.T) {
	conn := openPG(t, MaxOpenConns(1))
	pc := mustPin(t, conn)

	// A working segment first: the refusals below are the release, not a broken handle.
	if msgs, st := segment(t, pc, "SELECT 'before'"); st != 'I' || string(rowsOf(msgs)[0][0]) != "before" {
		t.Fatalf("pre-release segment: %c %v", st, rowsOf(msgs))
	}
	if err := pc.Release(bg(t)); err != nil {
		t.Fatalf("Release from quiescent: %v", err)
	}

	for name, op := range map[string]func() error{
		"Send":  func() error { return pc.Send(bg(t), ParseOp("", "SELECT 1", nil)) },
		"Flush": func() error { return pc.Flush(bg(t)) },
		"Receive": func() error {
			_, err := pc.Receive(bg(t))
			return err
		},
		"Sync": func() error {
			_, err := pc.Sync(bg(t))
			return err
		},
		"BeginSessionTx": func() error {
			_, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
			return err
		},
		"Release": func() error { return pc.Release(bg(t)) },
	} {
		if err := op(); !errors.Is(err, ErrReleased) {
			t.Errorf("%s after Release = %v, want ErrReleased", name, err)
		}
	}

	// The member went back to the pool intact (released, not destroyed) and is
	// re-borrowable — the single slot proves it is the SAME member.
	if s := conn.pool.Stat(); s.TotalConns() != 1 || s.IdleConns() != 1 {
		t.Fatalf("after Release: total=%d idle=%d, want 1/1 (recycled, not destroyed)", s.TotalConns(), s.IdleConns())
	}
	next := mustPin(t, conn)
	if next == pc {
		t.Fatal("the pool returned the same handle object; the cell cannot distinguish the two owners")
	}
	// The new owner works…
	if msgs, st := segment(t, next, "SELECT 'new owner'"); st != 'I' || string(rowsOf(msgs)[0][0]) != "new owner" {
		t.Fatalf("new owner's segment: %c %v", st, rowsOf(msgs))
	}
	// …and the stale handle still cannot touch it, mid-borrow.
	if err := pc.Send(bg(t), ParseOp("", "SELECT 'intruder'", nil)); !errors.Is(err, ErrReleased) {
		t.Fatalf("stale Send while the member is re-borrowed = %v, want ErrReleased", err)
	}
	// The new owner is unperturbed by the stale handle's attempt.
	if msgs, st := segment(t, next, "SELECT 'still fine'"); st != 'I' || string(rowsOf(msgs)[0][0]) != "still fine" {
		t.Fatalf("new owner after the intruder attempt: %c %v", st, rowsOf(msgs))
	}
	// A deferred Discard on the released handle is a clean no-op and must not reclaim
	// the member the new owner is holding.
	pc.Discard()
	if msgs, st := segment(t, next, "SELECT 'after stale discard'"); st != 'I' || string(rowsOf(msgs)[0][0]) != "after stale discard" {
		t.Fatalf("new owner after the stale Discard: %c %v", st, rowsOf(msgs))
	}
	if err := next.Release(bg(t)); err != nil {
		t.Fatalf("new owner Release: %v", err)
	}
}

// --- race detector -------------------------------------------------------------------

// trySegment runs one unnamed segment without failing the test: it returns
// ErrSegmentInFlight when a racing private exchange (a finalizer) legitimately owns the
// quiescent wire, and any other error verbatim.
func trySegment(ctx context.Context, pc PinnedConn, sql string) error {
	for _, op := range []ExtendedOp{ParseOp("", sql, nil), BindOp("", "", nil, nil, nil), ExecuteOp("", 0)} {
		if err := pc.Send(ctx, op); err != nil {
			return err
		}
	}
	if err := pc.Flush(ctx); err != nil {
		return err
	}
	for {
		m, err := pc.Receive(ctx)
		if err != nil {
			return err
		}
		if m.Kind == "CommandComplete" || m.Kind == "ErrorResponse" {
			break
		}
	}
	_, err := pc.Sync(ctx)
	return err
}

// Concurrent segment steps vs finalizer / Release / BeginSessionTx attempts on one
// handle, under the race detector. Every outcome must be one of the typed contract
// results — a refusal, a closed handle, or success — never a data race, a panic, or a
// corrupted wire: after the storm the handle still runs a coherent segment.
func TestPinned_C10_RaceSegmentsVsFinalizersAndRelease(t *testing.T) {
	conn := openPG(t)
	pc := mustPin(t, conn)
	tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := bg(t)

	allowed := func(err error) bool {
		return err == nil || errors.Is(err, ErrSegmentInFlight) || errors.Is(err, ErrTxStillOpen) || errors.Is(err, dao.ErrTransactionClosed)
	}

	// Finalizer / BeginSessionTx spinners run a BOUNDED burst and then exit; stop closes
	// when they are done. Every one of their outcomes must be a typed contract result —
	// never a data race (the -race gate), a panic, or a corrupt wire.
	//
	// Release is deliberately NOT in this mix: it TERMINALIZES the handle, so a
	// Release winning the wire would legitimately end every later segment and the liveness
	// assertion below would be asserting nothing. The Release race has its own cell,
	// TestPinned_MF1_ReleasedHandleIsTerminal, where the post-Release refusal IS the
	// property under test rather than noise.
	stop := make(chan struct{})
	var finWg sync.WaitGroup
	for g := 0; g < 2; g++ {
		finWg.Add(1)
		go func(g int) {
			defer finWg.Done()
			for i := 0; i < 300; i++ {
				var err error
				if g == 0 {
					err = tx.CommitContext(ctx)
				} else {
					_, err = pc.BeginSessionTx(ctx, dao.TxOptions{})
				}
				if !allowed(err) {
					t.Errorf("goroutine %d: unexpected %v", g, err)
					return
				}
				runtime.Gosched()
			}
		}(g)
	}
	go func() { finWg.Wait(); close(stop) }()

	// The segment goroutine drives coherent segments concurrently, retrying its current
	// segment when a finalizer legitimately owns the wire. It runs until the spinners
	// stop AND it has completed a handful of segments — the finalizers exhaust their
	// burst, so the wire is guaranteed to clear and progress is not schedule-dependent.
	// A generous total-attempt budget is the livelock guard, with its own message so
	// exhaustion is never mistaken for a passing run.
	var segmentsRun, segmentsRefused, attempts int
	const minSegments, maxAttempts = 5, 200000
	var stopped bool
	for {
		if !stopped {
			select {
			case <-stop:
				stopped = true
			default:
			}
		}
		if stopped && segmentsRun >= minSegments {
			break
		}
		if attempts++; attempts > maxAttempts {
			t.Fatalf("livelock guard: %d attempts, only %d segments completed — the wire never cleared", attempts, segmentsRun)
		}
		switch err := trySegment(ctx, pc, "SELECT 1"); {
		case err == nil:
			segmentsRun++
		case errors.Is(err, ErrSegmentInFlight):
			segmentsRefused++
			runtime.Gosched()
		default:
			t.Fatalf("segment: unexpected %v", err)
		}
	}
	finWg.Wait()
	t.Logf("segments run=%d refused-by-racing-finalizer=%d finalizer-attempts=600", segmentsRun, segmentsRefused)

	// Coherence after the storm: the wire still speaks the protocol. Nothing in the mix
	// terminalizes the handle, so ANY error here is a defect — post-Release success is
	// not an accepted outcome of this cell.
	if err := trySegment(ctx, pc, "SELECT 'coherent'"); err != nil {
		t.Fatalf("post-storm segment: %v", err)
	}
	_ = tx.RollbackContext(context.Background())
}

func TestPinned_C10_RaceDiscardVsBlockedReceive(t *testing.T) {
	const key = 424219
	releaseLock := holdAdvisoryLock(t, key)

	conn := openPG(t)
	pc := mustPin(t, conn)
	mustSend(t, pc,
		ParseOp("", fmt.Sprintf("SELECT pg_advisory_xact_lock(%d)", key), nil),
		BindOp("", "", nil, nil, nil),
	)
	mustFlush(t, pc)
	expect(t, pc, "ParseComplete")
	expect(t, pc, "BindComplete")
	mustSend(t, pc, ExecuteOp("", 0))
	mustFlush(t, pc)

	blocked := make(chan error, 1)
	go func() {
		_, err := pc.Receive(bg(t))
		blocked <- err
	}()
	// Discard races the blocked read. It must return promptly (it interrupts the read
	// and barriers behind it) and must not corrupt anything the race detector can see.
	done := make(chan struct{})
	go func() { pc.Discard(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Discard did not return while a Receive was blocked")
	}
	if err := <-blocked; err == nil {
		t.Fatal("the blocked Receive returned a message after Discard")
	}
	releaseLock()
	assertPoolDrained(t, conn)
	if got := scalar(t, conn, "SELECT 1"); got != "1" {
		t.Fatal(got)
	}
}

// --- portal resume and abandonment ---------------------------------------------------

func TestPinned_C11_PortalResumeAbandonAndNamedLifetime(t *testing.T) {
	conn := openPG(t)
	pc := mustPin(t, conn)
	tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	const q = "SELECT g FROM generate_series(1, 5) g"

	// Resume with exact state tuples.
	mustSend(t, pc, ParseOp("", q, nil), BindOp("", "", nil, nil, nil), ExecuteOp("", 2))
	mustFlush(t, pc)
	wantTuple(t, pc, flushed, noInbound, "after Flush")
	expect(t, pc, "ParseComplete")
	wantTuple(t, pc, flushed, receiving, "receiving")
	expect(t, pc, "BindComplete")
	expect(t, pc, "DataRow")
	expect(t, pc, "DataRow")
	expect(t, pc, "PortalSuspended")
	wantTuple(t, pc, flushed, receiving, "after PortalSuspended")

	mustSend(t, pc, ExecuteOp("", 2)) // the resume: flushed→building, inbound preserved
	wantTuple(t, pc, building, receiving, "after Send(resume)")
	mustFlush(t, pc)
	wantTuple(t, pc, flushed, receiving, "after Flush(resume)")
	if m := expect(t, pc, "DataRow"); string(m.Values[0]) != "3" {
		t.Fatalf("resumed row = %q, want 3", m.Values[0])
	}
	expect(t, pc, "DataRow")
	expect(t, pc, "PortalSuspended")
	mustSend(t, pc, ExecuteOp("", 0))
	mustFlush(t, pc)
	if m := expect(t, pc, "DataRow"); string(m.Values[0]) != "5" {
		t.Fatalf("final row = %q, want 5", m.Values[0])
	}
	expect(t, pc, "CommandComplete")
	if st := mustSync(t, pc); st != 'T' {
		t.Fatalf("status = %c", st)
	}
	wantTuple(t, pc, idleOut, noInbound, "after Sync")

	// Abandonment: suspend, then Sync with no resume ends the SEGMENT.
	mustSend(t, pc, ParseOp("", q, nil), BindOp("", "", nil, nil, nil), ExecuteOp("", 2))
	mustFlush(t, pc)
	expect(t, pc, "ParseComplete")
	expect(t, pc, "BindComplete")
	expect(t, pc, "DataRow")
	expect(t, pc, "DataRow")
	expect(t, pc, "PortalSuspended")
	if st := mustSync(t, pc); st != 'T' {
		t.Fatalf("status = %c", st)
	}
	wantTuple(t, pc, idleOut, noInbound, "after abandon Sync")

	// Named portal inside an explicit transaction SURVIVES Sync; an explicit Close
	// releases it.
	mustSend(t, pc, ParseOp("", q, nil), BindOp("np", "", nil, nil, nil), ExecuteOp("np", 2))
	mustFlush(t, pc)
	expect(t, pc, "ParseComplete")
	expect(t, pc, "BindComplete")
	expect(t, pc, "DataRow")
	expect(t, pc, "DataRow")
	expect(t, pc, "PortalSuspended")
	mustSync(t, pc)
	mustSend(t, pc, ExecuteOp("np", 2)) // new segment, same portal
	mustFlush(t, pc)
	if m := expect(t, pc, "DataRow"); string(m.Values[0]) != "3" {
		t.Fatalf("named portal after Sync resumed at %q, want 3", m.Values[0])
	}
	expect(t, pc, "DataRow")
	expect(t, pc, "PortalSuspended")
	mustSync(t, pc)
	mustSend(t, pc, ClosePortalOp("np"))
	mustFlush(t, pc)
	expect(t, pc, "CloseComplete")
	mustSync(t, pc)
	mustSend(t, pc, ExecuteOp("np", 1))
	mustFlush(t, pc)
	if m := expect(t, pc, "ErrorResponse"); m.Err.Code != "34000" {
		t.Fatalf("Execute closed portal = %s, want 34000", m.Err.Code)
	}
	if st := mustSync(t, pc); st != 'E' {
		t.Fatalf("status = %c", st)
	}
	if err := tx.RollbackContext(bg(t)); err != nil {
		t.Fatal(err)
	}

	// Only transaction end destroys a named portal that was never closed.
	tx, err = pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustSend(t, pc, ParseOp("", q, nil), BindOp("np2", "", nil, nil, nil), ExecuteOp("np2", 1))
	mustFlush(t, pc)
	drainGroup(t, pc)
	mustSync(t, pc)
	if err := tx.RollbackContext(bg(t)); err != nil {
		t.Fatal(err)
	}
	tx, err = pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.RollbackContext(context.Background()) }()
	mustSend(t, pc, ExecuteOp("np2", 1))
	mustFlush(t, pc)
	if m := expect(t, pc, "ErrorResponse"); m.Err.Code != "34000" {
		t.Fatalf("np2 after transaction end = %s, want 34000", m.Err.Code)
	}
	mustSync(t, pc)
}

// --- the pinned query/exec path ------------------------------------------------------

func TestPinned_C12_PinnedQueryPathStreamsAndCleansUp(t *testing.T) {
	conn := openPG(t)
	pc := mustPin(t, conn)
	tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.RollbackContext(context.Background()) }()

	// A relayed client's NAMED statement, and its UNNAMED statement + portal.
	mustSend(t, pc, ParseOp("client_s", "SELECT 1", nil))
	mustFlush(t, pc)
	expect(t, pc, "ParseComplete")
	mustSync(t, pc)
	mustSend(t, pc, ParseOp("", "SELECT 9", nil), BindOp("", "", nil, nil, nil))
	mustFlush(t, pc)
	expect(t, pc, "ParseComplete")
	expect(t, pc, "BindComplete")
	mustSync(t, pc)

	// The private path streams; RawValues are the wire's text bytes.
	rows, err := tx.QueryContext(bg(t), "SELECT g, 'v' || g FROM generate_series(1, 3) g")
	if err != nil {
		t.Fatal(err)
	}
	cols, _ := dao.Columns(rows)
	if strings.Join(cols, ",") != "g,?column?" {
		t.Fatalf("Columns = %v", cols)
	}
	rr, ok := dao.RawRowsOf(rows)
	if !ok {
		t.Fatal("pinned rows do not implement dao.RawRows")
	}
	n := 0
	for rows.Next() {
		n++
		raw := rr.RawValues()
		if string(raw[0]) != fmt.Sprint(n) || string(raw[1]) != "v"+fmt.Sprint(n) {
			t.Fatalf("row %d raw = %q", n, raw)
		}
		var g int64
		var s string
		if err := rows.Scan(&g, &s); err != nil {
			t.Fatal(err)
		}
		if g != int64(n) || s != "v"+fmt.Sprint(n) {
			t.Fatalf("row %d scanned = %d %q", n, g, s)
		}
	}
	if n != 3 || rows.Err() != nil {
		t.Fatalf("n=%d err=%v", n, rows.Err())
	}
	// While the rows are open the wire is private: every other face refuses.
	if err := pc.Send(bg(t), ParseOp("", "SELECT 1", nil)); !errors.Is(err, ErrSegmentInFlight) {
		t.Fatalf("Send with rows open err = %v, want ErrSegmentInFlight", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	wantTuple(t, pc, idleOut, noInbound, "after Rows.Close")

	// ExecContext: no-args simple path, then the extended path with args.
	res, err := tx.ExecContext(bg(t), "CREATE TEMP TABLE golib_adr0018_c12 (x int)")
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 0 {
		t.Fatalf("CREATE TABLE rows affected = %d", n)
	}
	res, err = tx.ExecContext(bg(t), "INSERT INTO golib_adr0018_c12 VALUES ($1), ($2)", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 2 {
		t.Fatalf("INSERT rows affected = %d, want 2", n)
	}

	// Object lifetime, both directions: the unnamed statement and portal are GONE
	// (destroyed by the explicit Close frames — mutation: drop the Close frames from
	// cleanupLocked and the unnamed statement is still described here), the named
	// statement SURVIVES.
	// Named first: once a missing-object error aborts the transaction, the server
	// refuses to Describe an EXISTING data-returning statement (25P02), so survival
	// must be observed while the transaction is still live.
	mustSend(t, pc, DescribeStatementOp("client_s"))
	mustFlush(t, pc)
	expect(t, pc, "ParameterDescription")
	expect(t, pc, "RowDescription")
	mustSync(t, pc)
	mustSend(t, pc, DescribeStatementOp(""))
	mustFlush(t, pc)
	if m := expect(t, pc, "ErrorResponse"); m.Err.Code != "26000" {
		t.Fatalf("Describe unnamed statement = %s, want 26000", m.Err.Code)
	}
	mustSync(t, pc) // the transaction is now aborted; a missing portal is still 34000
	mustSend(t, pc, DescribePortalOp(""))
	mustFlush(t, pc)
	if m := expect(t, pc, "ErrorResponse"); m.Err.Code != "34000" {
		t.Fatalf("Describe unnamed portal = %s, want 34000", m.Err.Code)
	}
	mustSync(t, pc)
}

// --- the closed vocabulary -----------------------------------------------------------

func TestPinned_C13_NoQueryFrameCrossesTheSeam(t *testing.T) {
	conn := openPG(t)
	pc := mustPin(t, conn)
	trace, stop := traceFrames(pc)
	defer stop()

	// Every constructor once, through the seam.
	mustSend(t, pc,
		ParseOp("v", "SELECT $1::int", nil),
		DescribeStatementOp("v"),
		BindOp("vp", "v", [][]byte{[]byte("1")}, []int16{0}, nil),
		DescribePortalOp("vp"),
		ExecuteOp("vp", 0),
		ClosePortalOp("vp"),
		CloseStatementOp("v"),
	)
	mustFlush(t, pc)
	for _, k := range []string{"ParseComplete", "ParameterDescription", "RowDescription", "BindComplete", "RowDescription", "DataRow", "CommandComplete", "CloseComplete", "CloseComplete"} {
		expect(t, pc, k)
	}
	mustSync(t, pc)
	got := trace.String()
	for _, f := range []string{"Parse", "Describe", "Bind", "Execute", "Close", "Flush", "Sync"} {
		if countFrontend(got, f) == 0 {
			t.Errorf("no %s frame in trace", f)
		}
	}
	if countFrontend(got, "Query") != 0 {
		t.Fatalf("a simple Query frame crossed the seam:\n%s", got)
	}

	// Control — the instrument observes a Query frame when one IS sent: the driver's own
	// no-args ExecContext uses the simple protocol, by design, outside the seam.
	trace.Reset()
	tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// BEGIN itself travels as a simple Query frame on the raw face.
	if countFrontend(trace.String(), "Query") != 1 || !strings.Contains(trace.String(), `"BEGIN"`) {
		t.Fatalf("control: BEGIN did not travel as one simple Query frame:\n%s", trace.String())
	}
	trace.Reset()
	if _, err := tx.ExecContext(bg(t), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if countFrontend(trace.String(), "Query") != 1 {
		t.Fatalf("control: the tracer did not observe the driver's own Query frame:\n%s", trace.String())
	}
	_ = tx.RollbackContext(bg(t))
}

// --- args and multi-statement dispatch -----------------------------------------------

func TestPinned_C14_ArgsAndMultiStatementDispatch(t *testing.T) {
	conn := openPG(t)
	mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0018_args`)
	mustExec(t, conn, `CREATE TABLE golib_adr0018_args (i int8, f float8, b bytea, ts timestamptz, ok bool, s text, n text)`)
	t.Cleanup(func() { mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0018_args`) })

	pc := mustPin(t, conn)
	tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.RollbackContext(context.Background()) }()

	ts := time.Date(2026, 9, 2, 10, 11, 12, 123456000, time.UTC)
	blob := []byte{0, 1, 2, 255, '\'', '\\'}
	if _, err := tx.ExecContext(bg(t), `INSERT INTO golib_adr0018_args VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		int64(-9007199254740993), 1.5e-7, blob, ts, true, "héllo, wörld", nil); err != nil {
		t.Fatalf("pinned insert with args: %v", err)
	}
	if _, err := tx.ExecContext(bg(t), `INSERT INTO golib_adr0018_args VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		int64(0), 0.0, []byte{}, ts, false, "", "not null"); err != nil {
		t.Fatalf("pinned insert (empties): %v", err)
	}

	type row struct {
		i  int64
		f  float64
		b  []byte
		ts time.Time
		ok bool
		s  string
		n  *string
	}
	read := func(q dao.Querier) []row {
		t.Helper()
		rows, err := q.QueryContext(bg(t), `SELECT i, f, b, ts, ok, s, n FROM golib_adr0018_args WHERE i = $1 OR i = $2 ORDER BY i`, int64(-9007199254740993), int64(0))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var out []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.i, &r.f, &r.b, &r.ts, &r.ok, &r.s, &r.n); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		if rows.Err() != nil {
			t.Fatal(rows.Err())
		}
		return out
	}
	// The pinned path reads its own uncommitted rows; the pool path cannot see them
	// until commit — so commit first, then compare DECODED values across both paths.
	if err := tx.CommitContext(bg(t)); err != nil {
		t.Fatal(err)
	}
	tx, err = pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	viaPinned := read(tx)
	viaPool := read(conn)
	if len(viaPinned) != 2 || len(viaPool) != 2 {
		t.Fatalf("rows: pinned=%d pool=%d", len(viaPinned), len(viaPool))
	}
	for k := range viaPinned {
		a, b := viaPinned[k], viaPool[k]
		if a.i != b.i || a.f != b.f || !bytes.Equal(a.b, b.b) || !a.ts.Equal(b.ts) || a.ok != b.ok || a.s != b.s ||
			(a.n == nil) != (b.n == nil) || (a.n != nil && *a.n != *b.n) {
			t.Fatalf("row %d differs after decoding:\n pinned=%+v\n pool=%+v", k, a, b)
		}
	}
	if viaPinned[0].n != nil || viaPinned[1].n == nil || *viaPinned[1].n != "not null" {
		t.Fatalf("NULL handling: %+v", viaPinned)
	}
	if !bytes.Equal(viaPinned[0].b, blob) || viaPinned[1].b == nil || len(viaPinned[1].b) != 0 {
		t.Fatalf("bytea: %q / %q", viaPinned[0].b, viaPinned[1].b)
	}

	// Multi-statement no-args ExecContext: ONE simple Query frame drains both groups.
	res, err := tx.ExecContext(bg(t), `INSERT INTO golib_adr0018_args (i) VALUES (10); INSERT INTO golib_adr0018_args (i) VALUES (11)`)
	if err != nil {
		t.Fatalf("multi-statement exec: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("last tag rows affected = %d, want 1", n)
	}
	if got := scalar(t, tx, `SELECT count(*) FROM golib_adr0018_args WHERE i IN (10, 11)`); got != "2" {
		t.Fatalf("both statements did not run: count = %s", got)
	}

	// Multi-command QueryContext: rejected by the SERVER at Parse, verbatim.
	_, err = tx.QueryContext(bg(t), `SELECT 1; SELECT 2`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42601" || !strings.Contains(pgErr.Message, "multiple commands") {
		t.Fatalf("multi-command query err = %v, want the server's 42601 Parse error verbatim", err)
	}
	// The wire came back quiescent (transaction aborted by the server error).
	wantTuple(t, pc, idleOut, noInbound, "after rejected multi-command query")
	if msgs, st := segment(t, pc, "SELECT 1"); lastKind(msgs) != "ErrorResponse" || st != 'E' {
		t.Fatalf("expected the aborted-transaction refusal, got %s/%c", lastKind(msgs), st)
	}
}

// --- legacy finalizer shape ----------------------------------------------------------

func TestPinned_C15_LegacyFinalizersDispatchOnBeginContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		fin  func(dao.TxConn) error
	}{
		{"Commit", func(tx dao.TxConn) error { return tx.Commit() }},
		{"Rollback", func(tx dao.TxConn) error { return tx.Rollback() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := openPG(t)
			pc := mustPin(t, conn)
			ctx, cancel := context.WithCancel(context.Background())
			tx, err := pc.BeginSessionTx(ctx, dao.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			err = tc.fin(tx)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s err = %v, want the BEGIN context's error", tc.name, err)
			}
			if errors.Is(err, dao.ErrTransactionClosed) {
				t.Fatalf("%s reported closed on its own first call", tc.name)
			}
			// TERMINAL: the legacy shape marks the handle closed before dispatch.
			if err := tx.Rollback(); !errors.Is(err, dao.ErrTransactionClosed) {
				t.Fatalf("subsequent finalizer err = %v, want ErrTransactionClosed", err)
			}
			if err := tx.RollbackContext(bg(t)); !errors.Is(err, dao.ErrTransactionClosed) {
				t.Fatalf("subsequent context finalizer err = %v, want ErrTransactionClosed", err)
			}
			// The server transaction was never finalized: Release refuses; Discard
			// reclaims the lease by closing the member.
			if err := pc.Release(bg(t)); !errors.Is(err, ErrTxStillOpen) {
				t.Fatalf("Release err = %v, want ErrTxStillOpen", err)
			}
			pc.Discard()
			assertPoolDrained(t, conn)
		})
	}

	// Control: with a live BEGIN context the legacy finalizers succeed and the member is
	// reusable.
	t.Run("live context control", func(t *testing.T) {
		conn := openPG(t)
		pc := mustPin(t, conn)
		tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := pc.Release(bg(t)); err != nil {
			t.Fatal(err)
		}
		if s := conn.pool.Stat(); s.IdleConns() != 1 {
			t.Fatalf("idle = %d, want 1 (recycled)", s.IdleConns())
		}
	})
}

// --- the ErrorResponse cleanup tail --------------------------------------------------

func TestPinned_C16_ErrorTailClosesOnlyWhatWasCreated(t *testing.T) {
	conn := openPG(t)
	pc := mustPin(t, conn)
	tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	trace, stop := traceFrames(pc)
	defer stop()

	// Positive arm: an Execute-stage error. The division happens inside a set-returning
	// function's output, which the planner cannot fold at Bind, so Parse AND Bind
	// succeed, both unnamed objects exist, and BOTH are closed after the recovery Sync.
	_, err = tx.ExecContext(bg(t), "SELECT 1 / g FROM generate_series($1::int, $1::int) g", 0)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "22012" {
		t.Fatalf("err = %v, want 22012 division_by_zero", err)
	}
	got := trace.String()
	if !strings.Contains(got, "B\tBindComplete\t") {
		t.Fatalf("the error was not Execute-stage (no BindComplete); the cell would not observe the two-object cleanup:\n%s", got)
	}
	if n := countFrontend(got, "Close"); n != 2 {
		t.Fatalf("Close frames after an Execute-stage error = %d, want 2 (portal + statement):\n%s", n, got)
	}
	if n := countFrontend(got, "Sync"); n != 2 {
		t.Fatalf("Sync frames = %d, want 2 (the group's own, then the cleanup exchange):\n%s", n, got)
	}
	// Close frames must come AFTER the recovery Sync (a Close before it is discarded).
	if firstClose, firstSync := strings.Index(got, "F\tClose\t"), strings.Index(got, "F\tSync\t"); firstClose < firstSync {
		t.Fatalf("a Close was queued before the recovery Sync:\n%s", got)
	}
	wantTuple(t, pc, idleOut, noInbound, "after the error tail")

	// The private unnamed objects are ABSENT, observed through the raw face, and the
	// wire runs a segment normally (the transaction is aborted, so the server answers
	// the Describe of a MISSING statement with 26000, not 25P02).
	mustSend(t, pc, DescribeStatementOp(""))
	mustFlush(t, pc)
	if m := expect(t, pc, "ErrorResponse"); m.Err.Code != "26000" {
		t.Fatalf("Describe unnamed after error tail = %s, want 26000 (absent)", m.Err.Code)
	}
	if st := mustSync(t, pc); st != 'E' {
		t.Fatalf("status = %c, want E", st)
	}
	if err := tx.RollbackContext(bg(t)); err != nil {
		t.Fatal(err)
	}
	tx, err = pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.RollbackContext(context.Background()) }()
	if msgs, st := segment(t, pc, "SELECT 'after'"); st != 'T' || string(rowsOf(msgs)[0][0]) != "after" {
		t.Fatalf("subsequent segment: %c %v", st, rowsOf(msgs))
	}

	// Negative arm: a Parse-stage error creates nothing, so NO Close is sent — only the
	// recovery Sync.
	trace.Reset()
	_, err = tx.ExecContext(bg(t), "SELEC $1::int", 1)
	if !errors.As(err, &pgErr) || pgErr.Code != "42601" {
		t.Fatalf("err = %v, want 42601", err)
	}
	got = trace.String()
	if n := countFrontend(got, "Close"); n != 0 {
		t.Fatalf("Close frames after a Parse-stage error = %d, want 0 (blind Close):\n%s", n, got)
	}
	if n := countFrontend(got, "Sync"); n != 1 {
		t.Fatalf("Sync frames = %d, want 1 (recovery only):\n%s", n, got)
	}
	wantTuple(t, pc, idleOut, noInbound, "after the Parse-error tail")
	mustSend(t, pc, DescribeStatementOp(""))
	mustFlush(t, pc)
	if m := expect(t, pc, "ErrorResponse"); m.Err.Code != "26000" {
		t.Fatalf("Describe unnamed = %s, want 26000", m.Err.Code)
	}
	mustSync(t, pc)

	// Bind-stage arm (observed on the first live run): the planner folds a constant
	// division at Bind, so the statement exists but the portal was never created —
	// exactly ONE Close (the statement), never a blind Close of the portal.
	if err := tx.RollbackContext(bg(t)); err != nil {
		t.Fatal(err)
	}
	tx, err = pc.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	trace.Reset()
	_, err = tx.ExecContext(bg(t), "SELECT 1 / $1::int", 0)
	if !errors.As(err, &pgErr) || pgErr.Code != "22012" {
		t.Fatalf("err = %v, want 22012", err)
	}
	got = trace.String()
	if strings.Contains(got, "B\tBindComplete\t") {
		t.Skipf("the server did not fold this division at Bind; the Bind-stage arm is not observable here:\n%s", got)
	}
	if n := countFrontend(got, "Close"); n != 1 {
		t.Fatalf("Close frames after a Bind-stage error = %d, want 1 (statement only):\n%s", n, got)
	}
	wantTuple(t, pc, idleOut, noInbound, "after the Bind-error tail")
	mustSend(t, pc, DescribeStatementOp(""))
	mustFlush(t, pc)
	if m := expect(t, pc, "ErrorResponse"); m.Err.Code != "26000" {
		t.Fatalf("Describe unnamed after Bind-stage tail = %s, want 26000", m.Err.Code)
	}
	mustSync(t, pc)
}

// --- the F2 gate this seam exists for: 25006 THROUGH the extended path ---------------

// A READ ONLY session transaction begun on the pinned connection is enforced by the
// server against statements relayed through the extended face — the engine-enforced
// guarantee autodb F2 relies on, with no second policy path.
func TestPinned_ReadOnlyEnforcedThroughExtendedPath(t *testing.T) {
	conn := openPG(t)
	mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0018_ro`)
	mustExec(t, conn, `CREATE TABLE golib_adr0018_ro (x int)`)
	t.Cleanup(func() { mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0018_ro`) })

	pc := mustPin(t, conn)
	tx, err := pc.BeginSessionTx(bg(t), dao.TxOptions{Access: dao.TxReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.RollbackContext(context.Background()) }()

	msgs, st := segment(t, pc, "SHOW transaction_read_only")
	if st != 'T' || string(rowsOf(msgs)[0][0]) != "on" {
		t.Fatalf("transaction_read_only = %v (%c)", rowsOf(msgs), st)
	}
	msgs, st = segment(t, pc, "INSERT INTO golib_adr0018_ro VALUES ($1)", "1")
	if lastKind(msgs) != "ErrorResponse" || msgs[len(msgs)-1].Err.Code != "25006" {
		t.Fatalf("relayed INSERT in a READ ONLY pinned tx: last=%s err=%+v, want 25006", lastKind(msgs), msgs[len(msgs)-1].Err)
	}
	if st != 'E' {
		t.Fatalf("status = %c, want E", st)
	}
	// Control: the same relayed INSERT in a READ WRITE pinned tx succeeds.
	if err := tx.RollbackContext(bg(t)); err != nil {
		t.Fatal(err)
	}
	tx, err = pc.BeginSessionTx(bg(t), dao.TxOptions{Access: dao.TxReadWrite})
	if err != nil {
		t.Fatal(err)
	}
	if msgs, _ := segment(t, pc, "INSERT INTO golib_adr0018_ro VALUES ($1)", "1"); lastKind(msgs) != "CommandComplete" {
		t.Fatalf("control INSERT: %s", lastKind(msgs))
	}
}
