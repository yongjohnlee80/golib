//go:build integration

// Live acceptance suite for transaction options (per-operation
// finalizer contexts, raw result access). It requires a reachable Postgres; set
// TEST_PGURL and run:
//
//	go test -tags integration ./dao/postgres/
//
// Every table and function it creates is dropped again; nothing else is
// touched.
package postgres

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yongjohnlee80/golib/dao"
)

// --- harness -----------------------------------------------------------------

// pgURL returns TEST_PGURL, skipping the test when it is unset (the suite-wide
// gate every postgres integration test in this package uses).
func pgURL(t *testing.T) string {
	t.Helper()

	url := os.Getenv("TEST_PGURL")
	if url == "" {
		t.Skip("TEST_PGURL not set; skipping postgres integration tests")
	}
	return url
}

// openPG opens a pool for this suite. Each test gets its own so a test
// that deliberately destroys a connection cannot perturb another.
func openPG(t *testing.T, opts ...Option) *pgxConn {
	t.Helper()

	url := pgURL(t)
	conn, err := Open(context.Background(), url, opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	pc, ok := conn.(*pgxConn)
	if !ok {
		t.Fatalf("Open returned %T, want *pgxConn", conn)
	}
	return pc
}

// mustExec runs a statement outside any transaction, failing the test on error.
func mustExec(t *testing.T, c dao.DataConn, sql string) {
	t.Helper()

	if _, err := c.ExecContext(context.Background(), sql); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// scalar reads one string from ex (a pool or a transaction).
func scalar(t *testing.T, ex dao.Querier, sql string) string {
	t.Helper()

	rows, err := ex.QueryContext(context.Background(), sql)
	if err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatalf("query %q returned no rows", sql)
	}
	var out string
	if err := rows.Scan(&out); err != nil {
		t.Fatalf("scan %q: %v", sql, err)
	}
	return out
}

// --- the matrix, live --------------------------------------------------------

// Every postgres cell of the option matrix actually reaches the server and takes
// effect. Rendering the right pgx options is checked without a server
// (TestPgTxOptions_FullMatrix); this checks the server agrees.
func TestIntegration_BeginConnTx_MatrixTakesEffect(t *testing.T) {
	conn := openPG(t)

	tests := []struct {
		name     string
		opts     dao.TxOptions
		wantIso  string
		wantRead string // transaction_read_only
	}{
		{"read only", dao.TxOptions{Access: dao.TxReadOnly}, "", "on"},
		{"explicit read write", dao.TxOptions{Access: dao.TxReadWrite}, "", "off"},

		// PostgreSQL accepts READ UNCOMMITTED, reports it back, and behaves as
		// READ COMMITTED — it has no dirty-read mode. The option is honored,
		// not refused, which is why it is in the domain.
		{"read uncommitted", dao.TxOptions{Isolation: dao.TxReadUncommitted}, "read uncommitted", ""},
		{"read committed", dao.TxOptions{Isolation: dao.TxReadCommitted}, "read committed", ""},
		{"repeatable read", dao.TxOptions{Isolation: dao.TxRepeatableRead}, "repeatable read", ""},
		{"serializable", dao.TxOptions{Isolation: dao.TxSerializable}, "serializable", ""},

		{
			"serializable read only",
			dao.TxOptions{Isolation: dao.TxSerializable, Access: dao.TxReadOnly},
			"serializable", "on",
		},
		{
			"serializable read only deferrable",
			dao.TxOptions{Isolation: dao.TxSerializable, Access: dao.TxReadOnly, Deferrable: dao.TxDeferrable},
			"serializable", "on",
		},
		{
			"serializable read only not deferrable",
			dao.TxOptions{Isolation: dao.TxSerializable, Access: dao.TxReadOnly, Deferrable: dao.TxNotDeferrable},
			"serializable", "on",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := dao.BeginConnTx(context.Background(), conn, tt.opts)
			if err != nil {
				t.Fatalf("BeginConnTx(%+v): %v", tt.opts, err)
			}
			defer func() { _ = tx.Rollback() }()

			if tt.wantIso != "" {
				if got := scalar(t, tx, "SHOW transaction_isolation"); got != tt.wantIso {
					t.Errorf("transaction_isolation = %q, want %q", got, tt.wantIso)
				}
			}
			if tt.wantRead != "" {
				if got := scalar(t, tx, "SHOW transaction_read_only"); got != tt.wantRead {
					t.Errorf("transaction_read_only = %q, want %q", got, tt.wantRead)
				}
			}
		})
	}
}

// The unchanged path, live: zero options still take DataConn.Begin and behave
// exactly as before.
func TestIntegration_BeginConnTx_ZeroOptionsUnchanged(t *testing.T) {
	conn := openPG(t)
	mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0017_zero`)
	mustExec(t, conn, `CREATE TABLE golib_adr0017_zero (v text)`)
	t.Cleanup(func() { mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0017_zero`) })

	ctx := context.Background()
	tx, err := dao.BeginConnTx(ctx, conn, dao.TxOptions{})
	if err != nil {
		t.Fatalf("BeginConnTx: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO golib_adr0017_zero VALUES ('x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if got := scalar(t, conn, `SELECT v FROM golib_adr0017_zero`); got != "x" {
		t.Errorf("committed value = %q, want %q", got, "x")
	}
}

// --- engine-enforced read-only through the session path ----------------------

// The proof autodb M9 requires, taken through the exact call autodb makes.
// A transport-level "does this statement look like a write?" check is not
// equivalent: the server refuses the write itself, whatever the statement looks
// like.
func TestIntegration_BeginSessionTx_ReadOnlyIsEnforcedByTheServer(t *testing.T) {
	conn := openPG(t)
	mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0017_ro`)
	mustExec(t, conn, `CREATE TABLE golib_adr0017_ro (v text)`)
	t.Cleanup(func() { mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0017_ro`) })

	ctx := context.Background()

	// The capability is assertable on the CONNECTION, before any transaction —
	// which is what lets a session-capable connection be refused at the moment
	// it is declared, rather than at the user's first BEGIN.
	sess, ok := dao.DataConn(conn).(dao.SessionTxBeginner)
	if !ok {
		t.Fatal("the postgres connection must satisfy dao.SessionTxBeginner")
	}

	tx, err := sess.BeginSessionTx(ctx, dao.TxOptions{Access: dao.TxReadOnly})
	if err != nil {
		t.Fatalf("BeginSessionTx: %v", err)
	}
	defer func() { _ = tx.RollbackContext(context.Background()) }()

	// The returned handle is a ContextTxConn by its static type — asserted here
	// as the criterion words it.
	var _ dao.ContextTxConn = tx

	_, err = tx.ExecContext(ctx, `INSERT INTO golib_adr0017_ro VALUES ('nope')`)
	if err == nil {
		t.Fatal("a write succeeded inside a READ ONLY transaction")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("err = %v, want a *pgconn.PgError", err)
	}
	if pgErr.Code != "25006" {
		t.Errorf("SQLSTATE = %q, want 25006 (read_only_sql_transaction)", pgErr.Code)
	}
}

// --- finalization with fresh contexts and the fault states -------------------

// The defect this capability exists for: pgxTx captured the Begin context and
// reused it for Commit/Rollback, so once the session context died there was no
// usable context left for cleanup. CommitContext takes its own.
func TestIntegration_CommitContext_OnAFreshContextAfterBeginCtxCancelled(t *testing.T) {
	conn := openPG(t)
	mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0017_fresh`)
	mustExec(t, conn, `CREATE TABLE golib_adr0017_fresh (v text)`)
	t.Cleanup(func() { mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0017_fresh`) })

	beginCtx, cancel := context.WithCancel(context.Background())
	tx, err := conn.BeginSessionTx(beginCtx, dao.TxOptions{})
	if err != nil {
		t.Fatalf("BeginSessionTx: %v", err)
	}
	if _, err := tx.ExecContext(context.Background(), `INSERT INTO golib_adr0017_fresh VALUES ('kept')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	cancel() // the session is over; its context is gone

	if err := tx.CommitContext(context.Background()); err != nil {
		t.Fatalf("CommitContext on a fresh context: %v", err)
	}
	if got := scalar(t, conn, `SELECT v FROM golib_adr0017_fresh`); got != "kept" {
		t.Errorf("committed value = %q, want %q", got, "kept")
	}
}

// The same, for rollback — tested separately, because a commit path that works
// says nothing about the cleanup path that actually runs when a session times
// out.
func TestIntegration_RollbackContext_OnAFreshContextAfterBeginCtxCancelled(t *testing.T) {
	conn := openPG(t)
	mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0017_freshrb`)
	mustExec(t, conn, `CREATE TABLE golib_adr0017_freshrb (v text)`)
	t.Cleanup(func() { mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0017_freshrb`) })

	beginCtx, cancel := context.WithCancel(context.Background())
	tx, err := conn.BeginSessionTx(beginCtx, dao.TxOptions{})
	if err != nil {
		t.Fatalf("BeginSessionTx: %v", err)
	}
	if _, err := tx.ExecContext(context.Background(), `INSERT INTO golib_adr0017_freshrb VALUES ('dropped')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	cancel()

	if err := tx.RollbackContext(context.Background()); err != nil {
		t.Fatalf("RollbackContext on a fresh context: %v", err)
	}
	rows, err := conn.QueryContext(context.Background(), `SELECT v FROM golib_adr0017_freshrb`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		t.Error("the rolled-back row is still there")
	}
}

// Fault state 1 — the finalizer's OWN context is already dead when it is
// called. Nothing is dispatched, so no outcome is claimed: the raw context
// error comes back, neither sentinel matches, and the handle is still open and
// still rollable with a fresh context.
func TestIntegration_FaultState1_PreDispatchCancellationLeavesTheHandleOpen(t *testing.T) {
	conn := openPG(t)

	for _, tc := range []struct {
		name     string
		finalize func(tx dao.ContextTxConn, ctx context.Context) error
	}{
		{"commit", func(tx dao.ContextTxConn, ctx context.Context) error { return tx.CommitContext(ctx) }},
		{"rollback", func(tx dao.ContextTxConn, ctx context.Context) error { return tx.RollbackContext(ctx) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := conn.BeginSessionTx(context.Background(), dao.TxOptions{})
			if err != nil {
				t.Fatalf("BeginSessionTx: %v", err)
			}

			dead, cancel := context.WithCancel(context.Background())
			cancel()

			err = tc.finalize(tx, dead)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want the raw context error", err)
			}
			if errors.Is(err, dao.ErrTxRolledBack) || errors.Is(err, dao.ErrTxOutcomeUnknown) {
				t.Error("a pre-dispatch refusal must claim no outcome at all")
			}

			// The handle is open: the transaction is still there to clean up,
			// which is the whole point of reporting rather than closing it.
			if err := tx.RollbackContext(context.Background()); err != nil {
				t.Errorf("the handle should still be open: %v", err)
			}
			// And once it IS finalized, it reports itself closed.
			if err := tx.RollbackContext(context.Background()); !errors.Is(err, dao.ErrTransactionClosed) {
				t.Errorf("second rollback err = %v, want dao.ErrTransactionClosed", err)
			}
		})
	}
}

// Fault state 2 — the finalizer dispatched, and pgconn proves nothing was
// written. The transaction definitely did not commit.
//
// Reaching it end-to-end needs a context that is dead for pgconn but not yet
// dead for our own pre-dispatch check: lyingCtx reports a nil Err exactly once
// (satisfying the pre-check) over an already-closed Done channel (which is what
// pgconn actually selects on). That is not a contrivance for its own sake — it
// is the real race the pre-check cannot close, and the classification has to be
// right when it happens.
func TestIntegration_FaultState2_ProvenNotWritten(t *testing.T) {
	conn := openPG(t)

	tx, err := conn.BeginSessionTx(context.Background(), dao.TxOptions{})
	if err != nil {
		t.Fatalf("BeginSessionTx: %v", err)
	}

	dead, cancel := context.WithCancel(context.Background())
	cancel()

	err = tx.CommitContext(&lyingCtx{Context: dead})
	if !errors.Is(err, dao.ErrTxRolledBack) {
		t.Fatalf("err = %v, want a dao.ErrTxRolledBack match", err)
	}
	if errors.Is(err, dao.ErrTxOutcomeUnknown) {
		t.Error("a proven non-commit must not read as an unknown outcome")
	}
	// The context cause is preserved for a caller that wants to know why.
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the context cause was not preserved: %v", err)
	}
	// The handle is closed — this one DID dispatch.
	if err := tx.RollbackContext(context.Background()); !errors.Is(err, dao.ErrTransactionClosed) {
		t.Errorf("post-finalization rollback err = %v, want dao.ErrTransactionClosed", err)
	}
	assertPoolDrained(t, conn)
}

// Fault state 3 — the server answers the COMMIT with a rollback. A statement
// error earlier in the transaction aborts it; PostgreSQL then accepts COMMIT
// and reports ROLLBACK, which pgx surfaces as ErrTxCommitRollback.
func TestIntegration_FaultState3_ServerConfirmedRollback(t *testing.T) {
	conn := openPG(t)

	tx, err := conn.BeginSessionTx(context.Background(), dao.TxOptions{})
	if err != nil {
		t.Fatalf("BeginSessionTx: %v", err)
	}
	if _, err := tx.ExecContext(context.Background(), `SELECT 1/0`); err == nil {
		t.Fatal("the poisoning statement unexpectedly succeeded")
	}

	err = tx.CommitContext(context.Background())
	if !errors.Is(err, dao.ErrTxRolledBack) {
		t.Fatalf("err = %v, want a dao.ErrTxRolledBack match", err)
	}
	if errors.Is(err, dao.ErrTxOutcomeUnknown) {
		t.Error("a server-confirmed rollback must not read as an unknown outcome")
	}
	if !errors.Is(err, pgx.ErrTxCommitRollback) {
		t.Errorf("the pgx cause was not preserved: %v", err)
	}
	if err := tx.CommitContext(context.Background()); !errors.Is(err, dao.ErrTransactionClosed) {
		t.Errorf("second commit err = %v, want dao.ErrTransactionClosed", err)
	}
}

// Fault state 4 — the COMMIT is written and the answer never arrives. A
// deferred constraint trigger that sleeps makes the server take longer over the
// COMMIT than the finalizer's context allows, so the cancellation lands AFTER
// the write: the outcome is genuinely unknowable, and must be reported as such
// rather than guessed either way.
func TestIntegration_FaultState4_ResponseLostIsUnknown(t *testing.T) {
	conn := openPG(t)
	setupSlowCommit(t, conn)

	tx, err := conn.BeginSessionTx(context.Background(), dao.TxOptions{})
	if err != nil {
		t.Fatalf("BeginSessionTx: %v", err)
	}
	if _, err := tx.ExecContext(context.Background(), `INSERT INTO golib_adr0017_slow VALUES (1)`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = tx.CommitContext(ctx)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("the commit was not interrupted (took %v); the deferred trigger did not fire", elapsed)
	}
	if !errors.Is(err, dao.ErrTxOutcomeUnknown) {
		t.Fatalf("err = %v, want a dao.ErrTxOutcomeUnknown match", err)
	}
	if errors.Is(err, dao.ErrTxRolledBack) {
		t.Error("an unknown outcome must not read as a definite rollback — that is the false guarantee the contract exists to prevent")
	}
	if err := tx.CommitContext(context.Background()); !errors.Is(err, dao.ErrTransactionClosed) {
		t.Errorf("second commit err = %v, want dao.ErrTransactionClosed", err)
	}
	// The connection is discarded rather than pooled: its state is undefined.
	assertPoolDrained(t, conn)
}

// A rollback that fails is observable, not swallowed — a caller whose cleanup
// did not happen has to be told. The failure is produced deterministically with
// the same lyingCtx: the ROLLBACK is dispatched into a context pgconn already
// considers dead.
func TestIntegration_RollbackFailureIsObservable(t *testing.T) {
	conn := openPG(t)

	tx, err := conn.BeginSessionTx(context.Background(), dao.TxOptions{})
	if err != nil {
		t.Fatalf("BeginSessionTx: %v", err)
	}

	dead, cancel := context.WithCancel(context.Background())
	cancel()

	err = tx.RollbackContext(&lyingCtx{Context: dead})
	if err == nil {
		t.Fatal("the failed rollback was swallowed")
	}
	if !strings.Contains(err.Error(), "rollback") {
		t.Errorf("err = %v, want a message naming the failed rollback", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the cause was not preserved: %v", err)
	}
	// The handle is closed and the connection is discarded: after a failed
	// rollback its state is undefined, so it must not go back into the pool.
	if err := tx.RollbackContext(context.Background()); !errors.Is(err, dao.ErrTransactionClosed) {
		t.Errorf("second rollback err = %v, want dao.ErrTransactionClosed", err)
	}
	assertPoolDrained(t, conn)
}

// --- raw result access -------------------------------------------------------

// The capability, on both executor paths, with the descriptors the server
// actually sent.
func TestIntegration_RawRows_PoolAndTxPaths(t *testing.T) {
	conn := openPG(t)
	setupRawTable(t, conn)

	ctx := context.Background()
	const q = rawQuery

	run := func(t *testing.T, ex dao.Querier) {
		t.Helper()

		rows, err := ex.QueryContext(ctx, q)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer func() { _ = rows.Close() }()

		rr, ok := dao.RawRowsOf(rows)
		if !ok {
			t.Fatal("the postgres row stream must satisfy dao.RawRows")
		}

		fds := rr.Fields()
		if len(fds) != 3 {
			t.Fatalf("got %d descriptors, want 3", len(fds))
		}
		// Metadata fidelity: the server's own OIDs, sizes and modifiers, not a
		// dao re-derivation.
		if fds[0].Name != "id" || fds[0].TypeOID != 23 || fds[0].TypeSize != 4 {
			t.Errorf("id descriptor = %+v, want int4 (oid 23, size 4)", fds[0])
		}
		if fds[1].TypeOID != 1043 || fds[1].TypeModifier != 14 {
			t.Errorf("name descriptor = %+v, want varchar(10) (oid 1043, typmod 14)", fds[1])
		}
		if fds[2].TypeOID != 25 || fds[2].TypeSize != -1 {
			t.Errorf("note descriptor = %+v, want text (oid 25, variable size)", fds[2])
		}
		// A plain table column carries its table's OID and its ordinal.
		if fds[0].TableOID == 0 || fds[0].ColumnAttr != 1 {
			t.Errorf("id descriptor lost its table identity: %+v", fds[0])
		}
		if fds[2].ColumnAttr != 3 {
			t.Errorf("note ColumnAttr = %d, want 3", fds[2].ColumnAttr)
		}

		if !rr.Next() {
			t.Fatal("no first row")
		}
		vals := rr.RawValues()
		if len(vals) != 3 {
			t.Fatalf("row 1 has %d values, want 3", len(vals))
		}
		// NULL and empty stay distinguishable all the way through.
		if vals[2] != nil {
			t.Errorf("NULL note came through as %#v, want a nil slice", vals[2])
		}
		if !rr.Next() {
			t.Fatal("no second row")
		}
		vals = rr.RawValues()
		if vals[1] == nil {
			t.Error("an empty varchar came through as NULL")
		}
		if len(vals[1]) != 0 {
			t.Errorf("empty varchar has length %d", len(vals[1]))
		}
		if string(vals[2]) != "two" {
			t.Errorf("note = %q, want %q", vals[2], "two")
		}
	}

	t.Run("pool", func(t *testing.T) { run(t, conn) })

	t.Run("transaction", func(t *testing.T) {
		tx, err := conn.BeginSessionTx(ctx, dao.TxOptions{Access: dao.TxReadOnly})
		if err != nil {
			t.Fatalf("BeginSessionTx: %v", err)
		}
		defer func() { _ = tx.RollbackContext(context.Background()) }()
		run(t, tx)
	})
}

// NULL and empty are different values, and the difference has to survive the
// copy a consumer makes to keep them — not merely exist while the row is
// current. A retained NULL that reads back as empty, or an empty that reads
// back as NULL, is the same corruption arriving one step later, and no
// assertion made before Close can see it.
//
// Both rows are retained across the end of the stream, its Close, and a
// deliberate churn of the pool: the only lifetime a real pass-through consumer
// ever has.
func TestIntegration_RawRows_NullAndEmptySurviveRetention(t *testing.T) {
	conn := openPG(t)
	setupRawTable(t, conn)

	rows, err := conn.QueryContext(context.Background(), rawQuery)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	rr, ok := dao.RawRowsOf(rows)
	if !ok {
		t.Fatal("the postgres row stream must satisfy dao.RawRows")
	}

	var kept [][][]byte
	for rr.Next() {
		kept = append(kept, copyRow(rr.RawValues()))
	}
	if err := rr.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	if err := rr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	churn(t, conn)

	if len(kept) != 2 {
		t.Fatalf("read %d rows, want 2", len(kept))
	}
	// Row 1: note IS NULL.
	if kept[0][2] != nil {
		t.Errorf("a retained NULL came back as %#v, want a nil slice", kept[0][2])
	}
	// Row 2: name is an empty varchar — present, not missing.
	if kept[1][1] == nil {
		t.Error("a retained empty varchar came back as nil; it is not NULL")
	}
	if len(kept[1][1]) != 0 {
		t.Errorf("retained empty varchar has length %d", len(kept[1][1]))
	}
	// And an ordinary value in the same retained row still reads correctly,
	// so the two assertions above are not passing on a wholly empty copy.
	if string(kept[1][2]) != "two" {
		t.Errorf("retained note = %q, want %q", kept[1][2], "two")
	}
}

// The format codes are the server's, and they are not all the same: a
// pass-through consumer that ignored them would misread every binary value as
// text. Both formats are produced here from real query paths — the default
// extended protocol (binary for int4) and the simple protocol (everything
// text) — rather than asserted from one and assumed for the other.
//
// Both rows are deep-copied with copyRow before Close and then survive a
// deliberate churn of the pool, so the assertions below are made against bytes
// this test owns. That is not ceremony: an outer-slice copy alone leaves the
// values pointing at pgx's receive buffers, and five subsequent queries
// rewrite them in place (measured — "alpha" reads back as "zzzzz").
func TestIntegration_RawRows_FormatCodes(t *testing.T) {
	ctx := context.Background()
	const q = `SELECT 1::int4, 'x'::text`

	binaryConn := openPG(t)
	rows, err := binaryConn.QueryContext(ctx, q)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	rr, ok := dao.RawRowsOf(rows)
	if !ok {
		t.Fatal("the postgres row stream must satisfy dao.RawRows")
	}
	if !rr.Next() {
		t.Fatal("no row")
	}
	binFDs := rr.Fields()
	binVals := copyRow(rr.RawValues())
	_ = rr.Close()
	churn(t, binaryConn)

	if binFDs[0].Format != 1 {
		t.Errorf("int4 format = %d, want 1 (binary)", binFDs[0].Format)
	}
	if len(binVals[0]) != 4 {
		t.Errorf("binary int4 is %d bytes (%v), want 4", len(binVals[0]), binVals[0])
	}

	textConn := openPG(t, func(cfg *pgxpool.Config) {
		cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	})
	rows, err = textConn.QueryContext(ctx, q)
	if err != nil {
		t.Fatalf("query (simple protocol): %v", err)
	}
	rr, ok = dao.RawRowsOf(rows)
	if !ok {
		t.Fatal("the postgres row stream must satisfy dao.RawRows")
	}
	if !rr.Next() {
		t.Fatal("no row")
	}
	txtFDs := rr.Fields()
	txtVals := copyRow(rr.RawValues())
	_ = rr.Close()
	churn(t, textConn)

	if txtFDs[0].Format != 0 {
		t.Errorf("simple-protocol int4 format = %d, want 0 (text)", txtFDs[0].Format)
	}
	if string(txtVals[0]) != "1" {
		t.Errorf("text int4 = %q, want %q", txtVals[0], "1")
	}
	// The same column, two formats, two different byte strings: the format code
	// is load-bearing, which is why it is carried.
	if string(binVals[0]) == string(txtVals[0]) {
		t.Error("binary and text renderings are identical; the format code proves nothing here")
	}
}

// --- helpers -----------------------------------------------------------------

// rawQuery reads the scratch table setupRawTable builds, whose second row
// carries both an empty non-NULL value and a NULL one.
const rawQuery = `SELECT id, name, note FROM golib_adr0017_raw ORDER BY id`

// setupRawTable (re)creates the raw-rows scratch table and drops it after.
func setupRawTable(t *testing.T, conn dao.DataConn) {
	t.Helper()

	mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0017_raw`)
	mustExec(t, conn, `CREATE TABLE golib_adr0017_raw (id int PRIMARY KEY, name varchar(10), note text)`)
	mustExec(t, conn, `INSERT INTO golib_adr0017_raw VALUES (1, 'a', NULL), (2, '', 'two')`)
	t.Cleanup(func() { mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0017_raw`) })
}

// copyRow deep-copies a borrowed RawValues row so it stays valid after the row
// stream is closed. Copying the outer slice alone is not enough — the byte
// slices are pgx's own receive buffers (see dao.RawRows), and the whole point
// of the capability is that dao does not copy them for you.
//
// bytes.Clone, specifically. append([]byte(nil), v...) appends zero bytes to a
// nil destination for an empty value and returns nil, which would quietly
// promote every empty column to NULL in the retained copy — a corruption the
// live assertions cannot see because it happens in the copy.
func copyRow(vals [][]byte) [][]byte {
	out := make([][]byte, len(vals))
	for i, v := range vals {
		out[i] = bytes.Clone(v)
	}
	return out
}

// churn drives more traffic through the pool so the driver reuses the receive
// buffers an earlier row stream handed out. It is the positive control for
// copyRow: swap a shallow outer-slice copy back in and the assertions that
// follow a churn fail, which is precisely what "borrowed until the next Next
// or Close" means.
func churn(t *testing.T, conn *pgxConn) {
	t.Helper()

	for range 5 {
		rows, err := conn.QueryContext(context.Background(), `SELECT 'zzzzzzzz'::text, 'yyyyyyyy'::text`)
		if err != nil {
			t.Fatalf("churn query: %v", err)
		}
		for rows.Next() {
		}
		// A control that fails quietly is not a control: if the churn did not
		// actually run, every assertion it guards silently weakens to the
		// pre-churn case.
		if err := rows.Err(); err != nil {
			t.Fatalf("churn iteration: %v", err)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("churn close: %v", err)
		}
	}
}

// lyingCtx reports a nil Err exactly once, over an already-cancelled parent. It
// models the unavoidable race between a pre-dispatch context check and the
// driver's own: our check passes, pgconn's does not.
type lyingCtx struct {
	context.Context
	asked atomic.Bool
}

func (c *lyingCtx) Err() error {
	if c.asked.CompareAndSwap(false, true) {
		return nil
	}
	return c.Context.Err()
}

// setupSlowCommit installs a deferred constraint trigger that sleeps, so a
// COMMIT can be made to take longer than its context allows.
func setupSlowCommit(t *testing.T, conn dao.DataConn) {
	t.Helper()

	drop := func() {
		mustExec(t, conn, `DROP TABLE IF EXISTS golib_adr0017_slow`)
		mustExec(t, conn, `DROP FUNCTION IF EXISTS golib_adr0017_sleep()`)
	}
	drop()
	mustExec(t, conn, `CREATE TABLE golib_adr0017_slow (id int PRIMARY KEY)`)
	mustExec(t, conn, `CREATE FUNCTION golib_adr0017_sleep() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_sleep(5); RETURN NULL; END $$`)
	mustExec(t, conn, `CREATE CONSTRAINT TRIGGER golib_adr0017_slow_trg
		AFTER INSERT ON golib_adr0017_slow
		DEFERRABLE INITIALLY DEFERRED
		FOR EACH ROW EXECUTE FUNCTION golib_adr0017_sleep()`)
	t.Cleanup(drop)
}

// assertPoolDrained checks that a connection whose transaction ended in an
// undefined state was destroyed rather than returned to the pool. Destruction
// is asynchronous, so it is polled rather than sampled once.
func assertPoolDrained(t *testing.T, conn *pgxConn) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if conn.pool.Stat().TotalConns() == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("the pool still holds %d connection(s); a connection in an undefined state must be discarded",
				conn.pool.Stat().TotalConns())
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}
