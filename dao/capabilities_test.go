package dao

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// noTxDialect is a GenericDialect with the OLAP / no-transaction capability
// profile (ADR-0008): no interactive transactions, no upsert, no RETURNING, no
// LastInsertID. It models the BigQuery driver's dialect for core-level tests
// without a real database.
type noTxDialect struct{ GenericDialect }

func (noTxDialect) Name() string               { return "notx" }
func (noTxDialect) SupportsTransactions() bool { return false }
func (noTxDialect) SupportsUpsert() bool       { return false }
func (noTxDialect) SupportsReturning() bool    { return false }
func (noTxDialect) SupportsLastInsertID() bool { return false }

func newNoTxConn() *fakeConn { return &fakeConn{d: noTxDialect{}} }

// --- §2.4 Upsert -------------------------------------------------------------

func TestNoTx_UpsertUnsupported(t *testing.T) {
	t.Parallel()

	conn := newNoTxConn()
	err := buildSchema(conn).DAO().Set(aName, "x").Set(aURI, "u").Upsert()
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Upsert err = %v, want ErrUnsupported", err)
	}
	if conn.lastExec != "" || conn.lastQuery != "" {
		t.Errorf("no SQL should be emitted on an unsupported upsert; exec=%q query=%q", conn.lastExec, conn.lastQuery)
	}
}

// --- §2.6 Insert: documented (zero, nil) on a no-id store --------------------

func TestNoTx_InsertReturnsZeroNoError(t *testing.T) {
	t.Parallel()

	conn := newNoTxConn()
	id, err := buildSchema(conn).DAO().Set(aName, "x").Set(aURI, "u").Insert()
	if err != nil {
		t.Fatalf("Insert err = %v, want nil (documented no-generated-id insert)", err)
	}
	if id != "" {
		t.Errorf("id = %q, want zero value", id)
	}
	// The DML ran via ExecContext (not the RETURNING/QueryContext path)...
	if conn.lastExec == "" {
		t.Error("expected the INSERT DML to be executed via ExecContext")
	}
	// ...and carries no RETURNING clause.
	if strings.Contains(conn.lastExec, "RETURNING") {
		t.Errorf("no-RETURNING dialect emitted a RETURNING clause: %q", conn.lastExec)
	}
}

// --- §2.5 batch COPY + conflict gates ---------------------------------------

func TestNoTx_BatchForceCopyUnsupported(t *testing.T) {
	t.Parallel()

	conn := newNoTxConn()
	err := buildSchema(conn).DAO().Batch().Add(map[artistField]any{aName: "x"}).ForceCopy().Flush()
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("ForceCopy Flush err = %v, want ErrUnsupported", err)
	}
	if conn.lastExec != "" {
		t.Errorf("no SQL should be emitted on an unsupported ForceCopy; exec=%q", conn.lastExec)
	}
}

func TestNoTx_BatchConflictHandlingUnsupported(t *testing.T) {
	t.Parallel()

	conn := newNoTxConn()
	err := buildSchema(conn).DAO().Batch().Add(map[artistField]any{aName: "x", aURI: "u"}).OnConflictUpdate(aURI).Flush()
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("OnConflictUpdate Flush err = %v, want ErrUnsupported", err)
	}
	if conn.lastExec != "" {
		t.Errorf("no SQL should be emitted on unsupported batch conflict handling; exec=%q", conn.lastExec)
	}
}

// A COPY-capable dialect that also can't upsert: ForceCopy wins over the
// combination error (nit #4). genericDialect can't COPY, so we use a dialect
// that reports CopySupported true but SupportsUpsert false.
type copyNoUpsertDialect struct{ GenericDialect }

func (copyNoUpsertDialect) CopySupported() bool  { return true }
func (copyNoUpsertDialect) SupportsUpsert() bool { return false }

func TestForceCopyPlusConflict_CapabilityErrorOrdering(t *testing.T) {
	t.Parallel()

	// ForceCopy + conflict handling on a dialect that CAN copy but can't upsert:
	// the upsert capability gate fires (conflict handling unsupported) before the
	// generic "ForceCopy cannot be combined with conflict handling" message.
	conn := &fakeConn{d: copyNoUpsertDialect{}}
	err := buildSchema(conn).DAO().Batch().Add(map[artistField]any{aName: "x"}).ForceCopy().OnConflictUpdate(aURI).Flush()
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported (capability gate wins)", err)
	}
}

// --- §2.3 transactions: first-touch gating, untouched conn unaffected -------

func TestNoTx_TxBoundInsertUnsupportedOnTouch(t *testing.T) {
	t.Parallel()

	conn := newNoTxConn()
	s := buildSchema(conn)
	err := RunTx(context.Background(), func(tx *Transaction) error {
		_, e := s.On(tx).Set(aName, "x").Set(aURI, "u").Insert()
		return e
	})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("tx-bound Insert err = %v, want ErrUnsupported", err)
	}
	if conn.lastExec != "" {
		t.Errorf("no SQL should run on a no-tx connection touched in a tx; exec=%q", conn.lastExec)
	}
}

func TestNoTx_TxBoundBatchUnsupportedOnTouch(t *testing.T) {
	t.Parallel()

	conn := newNoTxConn()
	s := buildSchema(conn)
	err := RunTx(context.Background(), func(tx *Transaction) error {
		return s.On(tx).Batch().Add(map[artistField]any{aName: "x"}).Flush()
	})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("tx-bound Batch err = %v, want ErrUnsupported (initErr path)", err)
	}
	if conn.lastExec != "" {
		t.Errorf("no SQL should run on a no-tx connection's tx-bound batch; exec=%q", conn.lastExec)
	}
}

// --- a tx-capable fake connection so the "untouched" case can commit --------

type fakeTxConn struct {
	exec       string
	committed  bool
	rolledBack bool
}

func (t *fakeTxConn) QueryContext(context.Context, string, ...any) (Rows, error) {
	return &fakeRows{}, nil
}
func (t *fakeTxConn) ExecContext(_ context.Context, q string, _ ...any) (Result, error) {
	t.exec = q
	return fakeResult{}, nil
}
func (t *fakeTxConn) Commit() error   { t.committed = true; return nil }
func (t *fakeTxConn) Rollback() error { t.rolledBack = true; return nil }

// txCapConn is a tx-capable DataConn (GenericDialect) with a working Begin.
type txCapConn struct {
	*fakeConn
	name  string
	tx    *fakeTxConn
	begun bool
}

func (c *txCapConn) Begin(context.Context) (TxConn, error) { c.begun = true; return c.tx, nil }
func (c *txCapConn) Name() string                          { return c.name }

func TestUntouchedNoTxConn_Unaffected(t *testing.T) {
	t.Parallel()

	okConn := &txCapConn{fakeConn: &fakeConn{d: GenericDialect{}}, name: "pg", tx: &fakeTxConn{}}
	noTxConn := newNoTxConn() // Name() == "fake"
	sOk := buildSchema(okConn)

	// A RunTx spanning {okConn, noTxConn} whose body touches only okConn must
	// commit normally — the untouched no-tx connection is never begun (§2.3).
	err := RunTx(context.Background(), func(tx *Transaction) error {
		return sOk.On(tx).With(aID, "1").Set(aName, "x").Update()
	}, Spanning(okConn, noTxConn))
	if err != nil {
		t.Fatalf("RunTx err = %v, want nil (untouched no-tx conn must not break the tx)", err)
	}
	if !okConn.begun || !okConn.tx.committed {
		t.Errorf("okConn should have begun (%v) and committed (%v)", okConn.begun, okConn.tx.committed)
	}
	if !strings.HasPrefix(okConn.tx.exec, "UPDATE") {
		t.Errorf("expected UPDATE on the tx connection, got %q", okConn.tx.exec)
	}
	if noTxConn.lastExec != "" {
		t.Errorf("untouched no-tx connection ran SQL: %q", noTxConn.lastExec)
	}
}

// --- defaults sanity: GenericDialect/postgres/sqlite keep OLTP capabilities --

func TestGenericDialect_CapabilityDefaults(t *testing.T) {
	t.Parallel()

	var d Dialect = GenericDialect{}
	if !d.SupportsTransactions() {
		t.Error("GenericDialect.SupportsTransactions() = false, want true")
	}
	if !d.SupportsUpsert() {
		t.Error("GenericDialect.SupportsUpsert() = false, want true")
	}
	if d.SupportsLastInsertID() {
		t.Error("GenericDialect.SupportsLastInsertID() = true, want false (RETURNING-based)")
	}
}
