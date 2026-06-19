package dao

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// --- transaction fakes ------------------------------------------------------

// recTx records statements + commit/rollback for one connection's driver tx.
type recTx struct {
	execs      []string
	committed  bool
	rolledback bool
	commitErr  error
}

func (t *recTx) QueryContext(context.Context, string, ...any) (Rows, error) {
	return &fakeRows{}, nil
}
func (t *recTx) ExecContext(_ context.Context, q string, _ ...any) (Result, error) {
	t.execs = append(t.execs, q)
	return fakeResult{}, nil
}
func (t *recTx) Commit() error   { t.committed = true; return t.commitErr }
func (t *recTx) Rollback() error { t.rolledback = true; return nil }

// txConn is a DataConn whose Begin yields a recTx; it also counts pool execs and
// BEGINs so tests can assert routing and laziness.
type txConn struct {
	nm         string
	dia        Dialect
	beginCount int
	poolExecs  int
	tc         *recTx
}

func (c *txConn) QueryContext(context.Context, string, ...any) (Rows, error) {
	return &fakeRows{}, nil
}
func (c *txConn) ExecContext(context.Context, string, ...any) (Result, error) {
	c.poolExecs++
	return fakeResult{}, nil
}
func (c *txConn) Dialect() Dialect { return c.dia }
func (c *txConn) Begin(context.Context) (TxConn, error) {
	c.beginCount++
	if c.tc == nil {
		c.tc = &recTx{}
	}
	return c.tc, nil
}
func (c *txConn) Name() string { return c.nm }
func (c *txConn) Close() error { return nil }

func newTxConn(name string) *txConn { return &txConn{nm: name, dia: GenericDialect{}} }

// stageUpdate runs one Update on the given DAO (Exec path, no RETURNING scan).
func stageUpdate(d DAO[*artist, artistField, string]) error {
	return d.With(aID, "1").Set(aName, "x").Update()
}

// --- tests ------------------------------------------------------------------

func TestRunTx_CommitsOnSuccess(t *testing.T) {
	t.Parallel()

	conn := newTxConn("db1")
	s := buildSchema(conn)
	err := RunTx(context.Background(), []DataConn{conn}, func(tx *Transaction) error {
		return stageUpdate(s.On(tx))
	})
	if err != nil {
		t.Fatalf("RunTx: %v", err)
	}
	if conn.tc == nil || !conn.tc.committed || conn.tc.rolledback {
		t.Errorf("tx state: %+v", conn.tc)
	}
	if len(conn.tc.execs) != 1 {
		t.Errorf("update ran on tx %d times, want 1", len(conn.tc.execs))
	}
	if conn.poolExecs != 0 {
		t.Errorf("pool exec count = %d, want 0 (statement must run on tx)", conn.poolExecs)
	}
}

func TestRunTx_RollsBackOnError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	conn := newTxConn("db1")
	s := buildSchema(conn)
	err := RunTx(context.Background(), []DataConn{conn}, func(tx *Transaction) error {
		_ = stageUpdate(s.On(tx))
		return boom
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
	if !conn.tc.rolledback || conn.tc.committed {
		t.Errorf("tx should have rolled back: %+v", conn.tc)
	}
}

func TestRunTx_RollsBackAndRepanics(t *testing.T) {
	t.Parallel()

	conn := newTxConn("db1")
	s := buildSchema(conn)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected panic to propagate")
			}
		}()
		_ = RunTx(context.Background(), []DataConn{conn}, func(tx *Transaction) error {
			_ = stageUpdate(s.On(tx))
			panic("kaboom")
		})
	}()
	if !conn.tc.rolledback {
		t.Error("panic did not roll back the transaction")
	}
}

func TestDAO_RunsOnPool(t *testing.T) {
	t.Parallel()

	conn := newTxConn("db1")
	if err := stageUpdate(buildSchema(conn).DAO()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if conn.poolExecs != 1 {
		t.Errorf("pool exec = %d, want 1", conn.poolExecs)
	}
	if conn.beginCount != 0 || conn.tc != nil {
		t.Error("schema.DAO() must not begin a transaction")
	}
}

func TestTransaction_LazyBegin(t *testing.T) {
	t.Parallel()

	c1, c2 := newTxConn("db1"), newTxConn("db2")
	s1 := buildSchema(c1)
	tx := Begin(context.Background(), c1, c2)

	if c1.beginCount != 0 || c2.beginCount != 0 {
		t.Fatal("Begin must issue no driver BEGIN")
	}
	if err := stageUpdate(s1.On(tx)); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if c1.beginCount != 1 {
		t.Errorf("c1 begin = %d, want 1", c1.beginCount)
	}
	if c2.beginCount != 0 {
		t.Errorf("untouched c2 begin = %d, want 0", c2.beginCount)
	}
	// Second statement on c1 reuses the cached tx (no new BEGIN).
	if err := stageUpdate(s1.On(tx)); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if c1.beginCount != 1 {
		t.Errorf("c1 begin = %d after second stmt, want 1 (cached)", c1.beginCount)
	}
	_ = tx.Commit()
}

func TestTransaction_CrossDBCommitFailure(t *testing.T) {
	t.Parallel()

	c1 := newTxConn("db1")
	c2 := newTxConn("db2")
	c2.tc = &recTx{commitErr: errors.New("c2 commit failed")} // pre-seed a failing commit
	s1, s2 := buildSchema(c1), buildSchema(c2)

	err := RunTx(context.Background(), []DataConn{c1, c2}, func(tx *Transaction) error {
		if e := stageUpdate(s1.On(tx)); e != nil {
			return e
		}
		return stageUpdate(s2.On(tx))
	})

	var ce *CommitError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %T %v, want *CommitError", err, err)
	}
	if ce.Failed != "db2" {
		t.Errorf("Failed = %q, want db2", ce.Failed)
	}
	if !reflect.DeepEqual(ce.AlreadyDurable, []string{"db1"}) {
		t.Errorf("AlreadyDurable = %v, want [db1]", ce.AlreadyDurable)
	}
	if !c1.tc.committed {
		t.Error("db1 should have durably committed (touched first)")
	}
	if !c2.tc.rolledback {
		t.Error("db2 should have rolled back after its commit failed")
	}
}

func TestTransaction_Resource(t *testing.T) {
	t.Parallel()

	// Commit path: resource Commit runs.
	conn := newTxConn("db1")
	s := buildSchema(conn)
	var committed, rolledback bool
	err := RunTx(context.Background(), []DataConn{conn}, func(tx *Transaction) error {
		tx.Register("file:1", ResourceFunc(
			func() error { committed = true; return nil },
			func() error { rolledback = true; return nil },
		))
		return stageUpdate(s.On(tx))
	})
	if err != nil || !committed || rolledback {
		t.Errorf("commit path: err=%v committed=%v rolledback=%v", err, committed, rolledback)
	}

	// Rollback path: resource Rollback runs.
	conn2 := newTxConn("db1")
	s2 := buildSchema(conn2)
	var committed2, rolledback2 bool
	_ = RunTx(context.Background(), []DataConn{conn2}, func(tx *Transaction) error {
		tx.Register("file:2", ResourceFunc(
			func() error { committed2 = true; return nil },
			func() error { rolledback2 = true; return nil },
		))
		_ = stageUpdate(s2.On(tx))
		return errors.New("force rollback")
	})
	if committed2 || !rolledback2 {
		t.Errorf("rollback path: committed=%v rolledback=%v", committed2, rolledback2)
	}
}

func TestTransaction_TwoPhaseUnsupported(t *testing.T) {
	t.Parallel()

	conn := newTxConn("db1") // GenericDialect → TwoPhaseSupported() == false
	s := buildSchema(conn)
	err := RunTx(context.Background(), []DataConn{conn}, func(tx *Transaction) error {
		tx.TwoPhase()
		return stageUpdate(s.On(tx))
	})
	if !errors.Is(err, ErrTwoPhaseUnsupported) {
		t.Errorf("err = %v, want ErrTwoPhaseUnsupported", err)
	}
	if !conn.tc.rolledback {
		t.Error("a fail-fast 2PC commit should roll back the touched contexts")
	}
}

func TestTransaction_OnCtxRouting(t *testing.T) {
	t.Parallel()

	conn := newTxConn("db1")
	s := buildSchema(conn)
	err := RunTx(context.Background(), []DataConn{conn}, func(tx *Transaction) error {
		ctx := WithTx(context.Background(), tx)
		return stageUpdate(s.OnCtx(ctx))
	})
	if err != nil {
		t.Fatalf("RunTx: %v", err)
	}
	if conn.tc == nil || len(conn.tc.execs) != 1 {
		t.Error("OnCtx did not route the statement onto the transaction")
	}
}

func TestTransaction_ClosedAndUnknownConn(t *testing.T) {
	t.Parallel()

	conn := newTxConn("db1")

	// Unknown connection name.
	tx := Begin(context.Background(), conn)
	if _, err := tx.executorFor("nope"); !errors.Is(err, ErrUnknownConnection) {
		t.Errorf("executorFor(unknown) = %v, want ErrUnknownConnection", err)
	}

	// After Commit, the transaction is closed.
	tx2 := Begin(context.Background(), conn)
	_ = tx2.Commit()
	if _, err := tx2.executorFor("db1"); !errors.Is(err, ErrTransactionClosed) {
		t.Errorf("executorFor after commit = %v, want ErrTransactionClosed", err)
	}
}
