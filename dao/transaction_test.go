package dao

import (
	"context"
	"errors"
	"reflect"
	"strings"
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

// --- two-phase commit --------------------------------------------------------

// tpRecorder collects the 2PC hook calls across all participating dialects.
type tpRecorder struct {
	calls []string          // "prepare:db1", "commitPrepared:db1", ...
	gids  map[string]string // context name -> gid seen at Prepare
}

// tpDialect is a two-phase-capable fake dialect bound to one connection name.
type tpDialect struct {
	GenericDialect
	name              string
	rec               *tpRecorder
	prepareErr        error
	commitPreparedErr error
}

func (d tpDialect) TwoPhaseSupported() bool { return true }

func (d tpDialect) Prepare(_ context.Context, _ TxConn, gid string) error {
	d.rec.calls = append(d.rec.calls, "prepare:"+d.name)
	if d.rec.gids == nil {
		d.rec.gids = map[string]string{}
	}
	d.rec.gids[d.name] = gid
	return d.prepareErr
}

func (d tpDialect) CommitPrepared(_ context.Context, _ DataConn, gid string) error {
	d.rec.calls = append(d.rec.calls, "commitPrepared:"+d.name)
	if d.commitPreparedErr != nil {
		return d.commitPreparedErr
	}
	return nil
}

func (d tpDialect) RollbackPrepared(_ context.Context, _ DataConn, gid string) error {
	d.rec.calls = append(d.rec.calls, "rollbackPrepared:"+d.name)
	return nil
}

func newTPConn(name string, rec *tpRecorder) *txConn {
	return &txConn{nm: name, dia: tpDialect{name: name, rec: rec}}
}

func TestTransaction_TwoPhaseSuccess(t *testing.T) {
	t.Parallel()

	rec := &tpRecorder{}
	c1, c2 := newTPConn("db1", rec), newTPConn("db2", rec)
	s1, s2 := buildSchema(c1), buildSchema(c2)

	err := RunTx(context.Background(), []DataConn{c1, c2}, func(tx *Transaction) error {
		tx.TwoPhase()
		if err := stageUpdate(s1.On(tx)); err != nil {
			return err
		}
		return stageUpdate(s2.On(tx))
	})
	if err != nil {
		t.Fatalf("RunTx: %v", err)
	}

	want := []string{"prepare:db1", "prepare:db2", "commitPrepared:db1", "commitPrepared:db2"}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("calls = %v, want %v", rec.calls, want)
	}
	if c1.tc.committed || c2.tc.committed {
		t.Error("plain driver Commit must not run on the two-phase path")
	}
	if rec.gids["db1"] == rec.gids["db2"] {
		t.Error("each context must prepare under a distinct gid")
	}
	for name, gid := range rec.gids {
		if !strings.HasPrefix(gid, "golib-dao-"+name+"-") {
			t.Errorf("gid %q lacks the recognizable prefix for %s", gid, name)
		}
	}
}

func TestTransaction_TwoPhasePrepareFailure(t *testing.T) {
	t.Parallel()

	rec := &tpRecorder{}
	c1 := newTPConn("db1", rec)
	boom := errors.New("prepare boom")
	c2 := &txConn{nm: "db2", dia: tpDialect{name: "db2", rec: rec, prepareErr: boom}}
	s1, s2 := buildSchema(c1), buildSchema(c2)

	var resourceRolledBack bool
	err := RunTx(context.Background(), []DataConn{c1, c2}, func(tx *Transaction) error {
		tx.TwoPhase()
		tx.Register("file", ResourceFunc(nil, func() error { resourceRolledBack = true; return nil }))
		if err := stageUpdate(s1.On(tx)); err != nil {
			return err
		}
		return stageUpdate(s2.On(tx))
	})

	var ce *CommitError
	if !errors.As(err, &ce) || ce.Failed != "db2" || !errors.Is(err, boom) {
		t.Fatalf("err = %v, want CommitError failing db2 wrapping prepare error", err)
	}
	if len(ce.AlreadyDurable) != 0 {
		t.Errorf("nothing must be durable after a prepare failure, got %v", ce.AlreadyDurable)
	}
	if !contains(rec.calls, "rollbackPrepared:db1") {
		t.Errorf("db1's prepared tx must be rolled back, calls = %v", rec.calls)
	}
	if contains(rec.calls, "commitPrepared:db1") || contains(rec.calls, "commitPrepared:db2") {
		t.Errorf("no commitPrepared may run after a prepare failure, calls = %v", rec.calls)
	}
	if !c2.tc.rolledback {
		t.Error("the failed context's open driver tx must be rolled back")
	}
	if !resourceRolledBack {
		t.Error("resources must roll back when phase one aborts")
	}
}

func TestTransaction_TwoPhaseCommitPreparedFailure(t *testing.T) {
	t.Parallel()

	rec := &tpRecorder{}
	boom := errors.New("commit prepared boom")
	c1 := &txConn{nm: "db1", dia: tpDialect{name: "db1", rec: rec, commitPreparedErr: boom}}
	c2 := newTPConn("db2", rec)
	s1, s2 := buildSchema(c1), buildSchema(c2)

	err := RunTx(context.Background(), []DataConn{c1, c2}, func(tx *Transaction) error {
		tx.TwoPhase()
		if err := stageUpdate(s1.On(tx)); err != nil {
			return err
		}
		return stageUpdate(s2.On(tx))
	})

	var ce *CommitError
	if !errors.As(err, &ce) || ce.Failed != "db1" || !errors.Is(err, boom) {
		t.Fatalf("err = %v, want CommitError failing db1", err)
	}
	// The decision was commit: db2 must still be attempted and succeed.
	if !reflect.DeepEqual(ce.AlreadyDurable, []string{"db2"}) {
		t.Errorf("AlreadyDurable = %v, want [db2]", ce.AlreadyDurable)
	}
	if gid, ok := ce.PreparedPending["db1"]; !ok || gid != rec.gids["db1"] {
		t.Errorf("PreparedPending = %v, want db1's gid %q", ce.PreparedPending, rec.gids["db1"])
	}
	if contains(rec.calls, "rollbackPrepared:db1") {
		t.Error("a failed COMMIT PREPARED must stay prepared for operator recovery, not roll back")
	}
}

func TestTransaction_TwoPhaseResourceCommitsAfterDBs(t *testing.T) {
	t.Parallel()

	rec := &tpRecorder{}
	c1 := newTPConn("db1", rec)
	s1 := buildSchema(c1)

	var order []string
	err := RunTx(context.Background(), []DataConn{c1}, func(tx *Transaction) error {
		tx.TwoPhase()
		// Register the resource FIRST so touch order alone would commit it first;
		// the two-phase path must still commit it after the databases are durable.
		tx.Register("file", ResourceFunc(func() error { order = append(order, "resource"); return nil }, nil))
		return stageUpdate(s1.On(tx))
	})
	if err != nil {
		t.Fatalf("RunTx: %v", err)
	}
	order = append(rec.calls, order...)
	want := []string{"prepare:db1", "commitPrepared:db1", "resource"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}
