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
	err := RunTx(context.Background(), func(tx *Transaction) error {
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
	err := RunTx(context.Background(), func(tx *Transaction) error {
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
		_ = RunTx(context.Background(), func(tx *Transaction) error {
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
	tx := Begin(context.Background(), Spanning(c1, c2))

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

	err := RunTx(context.Background(), func(tx *Transaction) error {
		if e := stageUpdate(s1.On(tx)); e != nil {
			return e
		}
		return stageUpdate(s2.On(tx))
	}, Spanning(c1, c2))

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
	err := RunTx(context.Background(), func(tx *Transaction) error {
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
	_ = RunTx(context.Background(), func(tx *Transaction) error {
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
	err := RunTx(context.Background(), func(tx *Transaction) error {
		return stageUpdate(s.On(tx))
	}, TwoPhase())
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
	err := RunTx(context.Background(), func(tx *Transaction) error {
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

func TestTransaction_ClosedAndNotPermittedConn(t *testing.T) {
	t.Parallel()

	conn, other := newTxConn("db1"), newTxConn("nope")

	// A connection outside a declared span may not join.
	tx := Begin(context.Background(), Spanning(conn))
	if _, err := tx.join(other); !errors.Is(err, ErrUnknownConnection) {
		t.Errorf("join(undeclared) = %v, want ErrUnknownConnection", err)
	}
	if other.beginCount != 0 {
		t.Errorf("a rejected connection must not be begun; begin = %d", other.beginCount)
	}
	_ = tx.Rollback()

	// After Commit, the transaction is closed.
	tx2 := Begin(context.Background())
	_ = tx2.Commit()
	if _, err := tx2.join(conn); !errors.Is(err, ErrTransactionClosed) {
		t.Errorf("join after commit = %v, want ErrTransactionClosed", err)
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

	err := RunTx(context.Background(), func(tx *Transaction) error {
		if err := stageUpdate(s1.On(tx)); err != nil {
			return err
		}
		return stageUpdate(s2.On(tx))
	}, Spanning(c1, c2), TwoPhase())
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
	err := RunTx(context.Background(), func(tx *Transaction) error {
		tx.Register("file", ResourceFunc(nil, func() error { resourceRolledBack = true; return nil }))
		if err := stageUpdate(s1.On(tx)); err != nil {
			return err
		}
		return stageUpdate(s2.On(tx))
	}, Spanning(c1, c2), TwoPhase())

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

	err := RunTx(context.Background(), func(tx *Transaction) error {
		if err := stageUpdate(s1.On(tx)); err != nil {
			return err
		}
		return stageUpdate(s2.On(tx))
	}, Spanning(c1, c2), TwoPhase())

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
	err := RunTx(context.Background(), func(tx *Transaction) error {
		// Register the resource FIRST so touch order alone would commit it first;
		// the two-phase path must still commit it after the databases are durable.
		tx.Register("file", ResourceFunc(func() error { order = append(order, "resource"); return nil }, nil))
		return stageUpdate(s1.On(tx))
	}, TwoPhase())
	if err != nil {
		t.Fatalf("RunTx: %v", err)
	}
	order = append(rec.calls, order...)
	want := []string{"prepare:db1", "commitPrepared:db1", "resource"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

// --- ADR-0015: connection ownership, admission, option semantics -------------

// TestTransaction_UndeclaredSecondConnRejected covers ADR-0015 criterion 2: with
// no declared span the first database locks the transaction, so a second one
// fails typed before any BEGIN on it, and the first rolls back.
func TestTransaction_UndeclaredSecondConnRejected(t *testing.T) {
	t.Parallel()

	c1, c2 := newTxConn("db1"), newTxConn("db2")
	s1, s2 := buildSchema(c1), buildSchema(c2)

	err := RunTx(context.Background(), func(tx *Transaction) error {
		if e := stageUpdate(s1.On(tx)); e != nil {
			return e
		}
		return stageUpdate(s2.On(tx))
	})
	if !errors.Is(err, ErrUnknownConnection) {
		t.Fatalf("err = %v, want ErrUnknownConnection", err)
	}
	// The message must name both connections and the remedy.
	for _, want := range []string{"db1", "db2", "dao.Spanning"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if c2.beginCount != 0 {
		t.Errorf("rejected connection was begun (%d BEGINs)", c2.beginCount)
	}
	if c1.tc == nil || !c1.tc.rolledback {
		t.Error("the first connection's work must roll back with the transaction")
	}
}

// TestTransaction_SpanningAdmitsOnlyDeclared covers ADR-0015 criterion 3.
func TestTransaction_SpanningAdmitsOnlyDeclared(t *testing.T) {
	t.Parallel()

	c1, c2, c3 := newTxConn("db1"), newTxConn("db2"), newTxConn("db3")
	s1, s2, s3 := buildSchema(c1), buildSchema(c2), buildSchema(c3)

	// Declared in the order (c1, c2) but touched in the order (c2, c1): commit
	// order must follow touch, not declaration.
	err := RunTx(context.Background(), func(tx *Transaction) error {
		if e := stageUpdate(s2.On(tx)); e != nil {
			return e
		}
		if e := stageUpdate(s1.On(tx)); e != nil {
			return e
		}
		if e := stageUpdate(s3.On(tx)); !errors.Is(e, ErrUnknownConnection) {
			t.Errorf("undeclared third connection = %v, want ErrUnknownConnection", e)
		}
		return nil
	}, Spanning(c1, c2))
	if err != nil {
		t.Fatalf("RunTx: %v", err)
	}
	if c3.beginCount != 0 {
		t.Errorf("undeclared connection was begun (%d BEGINs)", c3.beginCount)
	}
	if !c1.tc.committed || !c2.tc.committed {
		t.Error("both declared connections should have committed")
	}
}

// TestTransaction_TwoPhasePreflightFailsBeforeFn covers ADR-0015 criterion 4:
// a declared span whose dialect cannot prepare fails from RunTx without
// invoking fn and without any driver BEGIN.
func TestTransaction_TwoPhasePreflightFailsBeforeFn(t *testing.T) {
	t.Parallel()

	rec := &tpRecorder{}
	capable := newTPConn("db1", rec)
	incapable := newTxConn("db2") // GenericDialect → TwoPhaseSupported() == false

	var ran bool
	err := RunTx(context.Background(), func(*Transaction) error {
		ran = true
		return nil
	}, Spanning(capable, incapable), TwoPhase())

	if !errors.Is(err, ErrTwoPhaseUnsupported) {
		t.Fatalf("err = %v, want ErrTwoPhaseUnsupported", err)
	}
	if !strings.Contains(err.Error(), "db2") {
		t.Errorf("error %q should name the incapable connection", err)
	}
	if ran {
		t.Error("fn must not run when the pre-flight fails")
	}
	if capable.beginCount != 0 || incapable.beginCount != 0 {
		t.Error("a pre-flight failure must issue no driver BEGIN")
	}
	if len(rec.calls) != 0 {
		t.Errorf("no 2PC hook may run, calls = %v", rec.calls)
	}
}

// TestTransaction_TwoPhaseValidatesActualParticipant covers ADR-0015
// criterion 5: capability is decided by the participant that joined, never by a
// same-named declaration. The pre-flight passes (the declared connection is
// capable) and Commit still refuses, because the schema supplied an incapable
// connection under the same name.
func TestTransaction_TwoPhaseValidatesActualParticipant(t *testing.T) {
	t.Parallel()

	rec := &tpRecorder{}
	declared := newTPConn("db1", rec) // capable
	actual := newTxConn("db1")        // same name, GenericDialect → incapable
	s := buildSchema(actual)

	err := RunTx(context.Background(), func(tx *Transaction) error {
		return stageUpdate(s.On(tx))
	}, Spanning(declared), TwoPhase())

	if !errors.Is(err, ErrTwoPhaseUnsupported) {
		t.Fatalf("err = %v, want ErrTwoPhaseUnsupported from Commit", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("nothing may be prepared or committed, calls = %v", rec.calls)
	}
	if actual.tc == nil || !actual.tc.rolledback {
		t.Error("the touched context must roll back")
	}
	if actual.tc.committed {
		t.Error("the incapable participant must not commit")
	}
}

// bareDBContext is a database participant with no two-phase capability at all —
// the defensive half of ADR-0015 criterion 5 (a dbTxContext that fails the
// twoPhaseContext assertion must fail exactly like an unsupported one).
type bareDBContext struct {
	n          string
	rolledback bool
}

func (c *bareDBContext) name() string     { return c.n }
func (c *bareDBContext) commit() error    { return nil }
func (c *bareDBContext) rollback() error  { c.rolledback = true; return nil }
func (c *bareDBContext) executor() TxConn { return &recTx{} }

func TestTransaction_TwoPhaseMissingCapabilityFailsClosed(t *testing.T) {
	t.Parallel()

	if _, ok := any(&bareDBContext{}).(twoPhaseContext); ok {
		t.Fatal("bareDBContext must not satisfy twoPhaseContext")
	}

	tx := Begin(context.Background(), TwoPhase())
	bare := &bareDBContext{n: "db1"}
	tx.contexts["db1"] = bare
	tx.order = append(tx.order, "db1")

	if err := tx.Commit(); !errors.Is(err, ErrTwoPhaseUnsupported) {
		t.Errorf("Commit = %v, want ErrTwoPhaseUnsupported", err)
	}
	if !bare.rolledback {
		t.Error("the participant must roll back when 2PC validation fails")
	}
}

// TestSpanning_ConstructionErrors covers ADR-0015 criterion 9: a nil or empty
// span fails the transaction before any work, on both paths, without panicking.
func TestSpanning_ConstructionErrors(t *testing.T) {
	t.Parallel()

	conn := newTxConn("db1")
	s := buildSchema(conn)

	cases := []struct {
		name string
		opt  TxOption
		want string
	}{
		{"nil connection", Spanning(nil), "nil connection at index 0"},
		{"nil after valid", Spanning(conn, nil), "nil connection at index 1"},
		{"no connections", Spanning(), "no connections declared"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// RunTx: fn never runs.
			var ran bool
			err := RunTx(context.Background(), func(*Transaction) error {
				ran = true
				return nil
			}, tc.opt)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("RunTx err = %v, want one mentioning %q", err, tc.want)
			}
			if ran {
				t.Error("fn must not run on a mis-constructed transaction")
			}

			// Manual path: join refuses, Commit reports and still compensates.
			tx := Begin(context.Background(), tc.opt)
			if _, jerr := tx.join(conn); jerr == nil || !strings.Contains(jerr.Error(), tc.want) {
				t.Errorf("join err = %v, want one mentioning %q", jerr, tc.want)
			}
			if _, serr := s.On(tx).With(aID, "1").Select(); serr == nil {
				t.Error("a statement on a mis-constructed transaction must fail")
			}
			var resourceRolledBack bool
			tx.Register("file", ResourceFunc(nil, func() error { resourceRolledBack = true; return nil }))
			if cerr := tx.Commit(); cerr == nil || !strings.Contains(cerr.Error(), tc.want) {
				t.Errorf("Commit err = %v, want one mentioning %q", cerr, tc.want)
			}
			if !resourceRolledBack {
				t.Error("Commit on a mis-constructed transaction must roll back registered resources")
			}
			if conn.beginCount != 0 {
				t.Errorf("no BEGIN may be issued; got %d", conn.beginCount)
			}
		})
	}
}

// TestSpanning_MergesAndDedupes covers the ADR-0015 §2.3 normalization rules:
// repeated options merge, duplicate names keep their first connection and
// position, and declaration order drives pre-flight reporting.
func TestSpanning_MergesAndDedupes(t *testing.T) {
	t.Parallel()

	rec := &tpRecorder{}
	capable := newTPConn("db1", rec)
	dupName := newTxConn("db1") // same logical database, incapable handle
	other := newTxConn("db2")

	var cfg txConfig
	Spanning(capable, dupName)(&cfg)
	Spanning(other, capable)(&cfg)
	if cfg.err != nil {
		t.Fatalf("merge produced an error: %v", cfg.err)
	}
	if len(cfg.span) != 2 {
		t.Fatalf("span = %d entries, want 2 (deduped by name)", len(cfg.span))
	}
	if cfg.span[0] != DataConn(capable) {
		t.Error("a duplicate name must keep its FIRST connection and position")
	}
	if cfg.span[1].Name() != "db2" {
		t.Errorf("second entry = %q, want db2 (declaration order preserved)", cfg.span[1].Name())
	}

	// Declaration order is the pre-flight reporting order: db2 is the first
	// incapable entry, so it is the one named.
	err := RunTx(context.Background(), func(*Transaction) error { return nil },
		Spanning(capable, other), TwoPhase())
	if !errors.Is(err, ErrTwoPhaseUnsupported) || !strings.Contains(err.Error(), "db2") {
		t.Errorf("err = %v, want ErrTwoPhaseUnsupported naming db2", err)
	}
}
