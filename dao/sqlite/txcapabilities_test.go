package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

// The sqlite row of the transaction-capability matrix — the "no capability"
// row. It runs
// against a real database, because the claim being tested is not only that the
// options are refused but that the UNCHANGED zero-option path still works
// exactly as it did.

func openScratch(t *testing.T) dao.DataConn {
	t.Helper()

	conn, err := Open(context.Background(), filepath.Join(t.TempDir(), "adr0017.db"), MaxOpenConns(1))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// SQLite claims no transaction capability at all. modernc/sqlite's transaction
// semantics come from the BEGIN keyword (DEFERRED/IMMEDIATE/EXCLUSIVE), which
// database/sql's TxOptions cannot reach; claiming TxBeginner would mean
// accepting an option set and ignoring most of it.
func TestSqliteCapabilitySet(t *testing.T) {
	t.Parallel()

	conn := openScratch(t)
	if _, ok := conn.(dao.TxBeginner); ok {
		t.Error("sqliteConn must NOT claim dao.TxBeginner")
	}
	if _, ok := conn.(dao.SessionTxBeginner); ok {
		t.Error("sqliteConn must NOT claim dao.SessionTxBeginner")
	}

	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, ok := tx.(dao.ContextTxConn); ok {
		t.Error("sqliteTx must NOT claim dao.ContextTxConn — *sql.Tx has no context finalizers")
	}

	var rows dao.Rows = &sql.Rows{}
	if _, ok := dao.RawRowsOf(rows); ok {
		t.Error("*sql.Rows must NOT satisfy dao.RawRows")
	}
}

// The unchanged path: zero options still go straight to DataConn.Begin, on a
// driver that opts into nothing. This is the byte-compatibility claim, run
// against a real database rather than asserted.
func TestSqliteBeginConnTx_ZeroOptionsUnchanged(t *testing.T) {
	t.Parallel()

	conn := openScratch(t)
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}

	tx, err := dao.BeginConnTx(ctx, conn, dao.TxOptions{})
	if err != nil {
		t.Fatalf("BeginConnTx: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO t (v) VALUES ('x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rows, err := conn.QueryContext(ctx, `SELECT v FROM t`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatal("the committed row is missing")
	}
}

// Every non-default option is refused up front by the helper, naming this
// driver — and no transaction is started, so the caller never holds a handle
// weaker than the one they asked for.
func TestSqliteBeginConnTx_NonDefaultOptionsRefused(t *testing.T) {
	t.Parallel()

	conn := openScratch(t)
	for _, opts := range []dao.TxOptions{
		{Access: dao.TxReadOnly},
		{Access: dao.TxReadWrite},
		{Isolation: dao.TxReadUncommitted},
		{Isolation: dao.TxSerializable},
		{Isolation: dao.TxSerializable, Access: dao.TxReadOnly, Deferrable: dao.TxDeferrable},
	} {
		tx, err := dao.BeginConnTx(context.Background(), conn, opts)
		if tx != nil {
			_ = tx.Rollback()
			t.Errorf("%+v: a transaction was started despite the refusal", opts)
		}
		if !errors.Is(err, dao.ErrUnsupported) {
			t.Errorf("%+v: err = %v, want a dao.ErrUnsupported match", opts, err)
			continue
		}
		var unsup *dao.ErrTxOptionUnsupported
		if !errors.As(err, &unsup) {
			t.Errorf("%+v: err = %v, want *dao.ErrTxOptionUnsupported", opts, err)
			continue
		}
		if unsup.Driver != "sqlite" {
			t.Errorf("%+v: Driver = %q, want %q", opts, unsup.Driver, "sqlite")
		}
	}
}

// Malformed options are reported as malformed, not as this driver's limitation.
func TestSqliteBeginConnTx_InvalidBeforeUnsupported(t *testing.T) {
	t.Parallel()

	conn := openScratch(t)
	_, err := dao.BeginConnTx(context.Background(), conn, dao.TxOptions{Access: dao.TxAccess(9)})

	var invalid *dao.ErrTxOptionInvalid
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *dao.ErrTxOptionInvalid", err)
	}
	if errors.Is(err, dao.ErrUnsupported) {
		t.Error("the invalid-input error must not also read as a capability miss")
	}
}

// The context helpers refuse a sqlite transaction rather than finalizing it
// with the context thrown away.
func TestSqliteCommitRollbackTx_Refused(t *testing.T) {
	t.Parallel()

	conn := openScratch(t)
	ctx := context.Background()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := dao.CommitTx(ctx, tx); !errors.Is(err, dao.ErrUnsupported) {
		t.Errorf("CommitTx err = %v, want a dao.ErrUnsupported match", err)
	}
	if err := dao.RollbackTx(ctx, tx); !errors.Is(err, dao.ErrUnsupported) {
		t.Errorf("RollbackTx err = %v, want a dao.ErrUnsupported match", err)
	}
	// The refusals were not finalizations: the base handle is still live.
	if err := tx.Rollback(); err != nil {
		t.Errorf("the transaction should still be open and rollable: %v", err)
	}
}
