package dao

import (
	"context"
	"slices"
	"testing"
	"time"
)

// The nil-transaction contract (ADR-0019).
//
// A nil *Transaction passed to Schema.On means "no transaction is held" and
// routes to the pool, exactly as Schema.DAO does. It is load-bearing: every
// executor-parameter helper in every consumer — `func f(tx *dao.Transaction)`
// passing tx straight to On(tx) — is built on it, so a future maintainer
// "fixing" the fallthrough into a panic or an error would break all of them at
// once. These tests are the lock; the doc comment alone is not one.
//
// THREE exported doors take a *Transaction, and nil does NOT mean the same
// thing at all three — the asymmetry is the reason these cells exist:
//
//	Schema.On(nil)  -> pool, no transaction context   (== Schema.DAO())
//	Schema.DAO()    -> pool, no transaction context
//	DAO.Use(nil)    -> pool, but RETAINS a transaction context already
//	                   inherited, deadline and cancellation included
//
// Use's asymmetry falls out of ADR-0009 §2.3 stickiness: its guard skips the
// ctxv ASSIGNMENT on nil, it does not CLEAR an assigned one. Locked by
// TestUseNil_* below so it cannot be "tidied" in either direction unnoticed.
//
// txConn (transaction_test.go) is the instrument: it counts pool execs and
// BEGINs separately, so "ran on the pool" and "began nothing" are two
// independent readings rather than one absence.

// On(nil) executes on the pool and begins no transaction.
func TestOnNil_RunsOnThePoolAndBeginsNothing(t *testing.T) {
	t.Parallel()

	conn := newTxConn("db1")
	s := buildSchema(conn)

	if err := stageUpdate(s.On(nil)); err != nil {
		t.Fatalf("Update through On(nil): %v", err)
	}
	if conn.poolExecs != 1 {
		t.Errorf("pool exec count = %d, want 1 — On(nil) must run the statement on the pool", conn.poolExecs)
	}
	if conn.beginCount != 0 {
		t.Errorf("begin count = %d, want 0 — On(nil) must not begin a transaction of its own", conn.beginCount)
	}
	if conn.tc != nil {
		t.Errorf("a driver transaction exists (%+v), want none", conn.tc)
	}
}

// The control for the test above: a non-nil transaction still binds. Without
// this, TestOnNil_RunsOnThePoolAndBeginsNothing would pass just as well against
// a build where On ignored its argument entirely.
func TestOn_NonNilStillBindsToTheTransaction(t *testing.T) {
	t.Parallel()

	conn := newTxConn("db1")
	s := buildSchema(conn)

	if err := RunTx(context.Background(), func(tx *Transaction) error {
		return stageUpdate(s.On(tx))
	}); err != nil {
		t.Fatalf("RunTx: %v", err)
	}
	if conn.poolExecs != 0 {
		t.Errorf("pool exec count = %d, want 0 — On(tx) must not reach the pool", conn.poolExecs)
	}
	if conn.tc == nil || len(conn.tc.execs) != 1 {
		t.Errorf("tx execs = %+v, want exactly one statement on the transaction", conn.tc)
	}
}

// On(nil) and DAO() produce the same DAO. Asserted on both a bare schema and a
// hooked+debug one, so the equivalence is not an artifact of the zero-hook fast
// path in acquire().
func TestOnNil_IsEquivalentToDAO(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		extra []Option[*artist, artistField, artistSort, string]
	}{
		{name: "bare"},
		{name: "hooked+debug", extra: []Option[*artist, artistField, artistSort, string]{
			Hooks[*artist, artistField, artistSort, string](NopHook{}),
			Debug[*artist, artistField, artistSort, string](true),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := buildSchema(newTxConn("db1"), tc.extra...)

			pool := s.DAO().(*queryDAO[*artist, artistField, artistSort, string])
			nilTx := s.On(nil).(*queryDAO[*artist, artistField, artistSort, string])

			if nilTx.tx != nil {
				t.Errorf("On(nil).tx = %v, want nil", nilTx.tx)
			}
			if nilTx.schema != pool.schema {
				t.Error("schema differs between On(nil) and DAO()")
			}
			if nilTx.conn != pool.conn {
				t.Error("conn differs between On(nil) and DAO()")
			}
			if nilTx.ctxv != pool.ctxv {
				t.Errorf("ctxv = %v, want %v (same as DAO())", nilTx.ctxv, pool.ctxv)
			}
			if nilTx.explicitCtx != pool.explicitCtx {
				t.Errorf("explicitCtx = %v, want %v", nilTx.explicitCtx, pool.explicitCtx)
			}
			if len(nilTx.hooks) != len(pool.hooks) {
				t.Errorf("hooks = %d, want %d (same effective set as DAO())", len(nilTx.hooks), len(pool.hooks))
			}
		})
	}
}

// The batch writer from an On(nil) DAO flushes to the pool. Batch() resolves its
// executor separately from handle(), so it needs its own reading.
func TestOnNil_BatchFlushesToThePool(t *testing.T) {
	t.Parallel()

	conn := newTxConn("db1")
	s := buildSchema(conn)

	b := s.On(nil).Batch()
	b.Add(map[artistField]any{aName: "Alpha", aURI: "alpha"})
	if err := b.Flush(); err != nil {
		t.Fatalf("Flush through On(nil).Batch(): %v", err)
	}
	if conn.poolExecs != 1 {
		t.Errorf("pool exec count = %d, want 1 — On(nil).Batch() must flush on the pool", conn.poolExecs)
	}
	if conn.beginCount != 0 {
		t.Errorf("begin count = %d, want 0 — On(nil).Batch() must not begin a transaction", conn.beginCount)
	}
}

// A WithQueryContext on an On(nil) DAO is honored: there is no transaction
// context to inherit, and the nil branch must not discard the explicit one.
func TestOnNil_HonorsWithQueryContext(t *testing.T) {
	t.Parallel()

	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "v")
	s := buildSchema(newTxConn("db1"))

	d := s.On(nil, WithQueryContext(ctx)).(*queryDAO[*artist, artistField, artistSort, string])
	if !d.explicitCtx {
		t.Error("explicitCtx = false, want true")
	}
	if d.ctx() != ctx {
		t.Errorf("ctx = %v, want the WithQueryContext value", d.ctx())
	}
}

// MF1 (gold-man r0): Use(nil) unbinds the transaction but RETAINS its context,
// so a pool statement issued afterwards carries the transaction's deadline and
// cancellation. Verified by execution before being documented, not inferred
// from the guard. This cell exists so the asymmetry cannot drift in either
// direction silently: tightening it (clearing ctxv) is a BEHAVIOUR change that
// needs its own decision, since a caller may depend on the sticky context.
func TestUseNil_UnbindsTheTransactionButRetainsItsContext(t *testing.T) {
	t.Parallel()

	conn := newTxConn("db1")
	s := buildSchema(conn)
	deadline := time.Now().Add(37 * time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	if err := RunTx(ctx, func(tx *Transaction) error {
		bound := s.On(tx).(*queryDAO[*artist, artistField, artistSort, string])
		if bound.ctxv != tx.ctx {
			t.Fatalf("On(tx) did not inherit the transaction context — precondition failed")
		}

		unbound := bound.Use(nil).(*queryDAO[*artist, artistField, artistSort, string])
		if unbound.tx != nil {
			t.Error("Use(nil).tx != nil — Use(nil) must unbind the transaction")
		}
		if unbound.ctxv != tx.ctx {
			t.Error("Use(nil) cleared the inherited transaction context; " +
				"retaining it is contract (ADR-0019 §2.1) — changing it is a behaviour change")
		}
		// The reading with teeth: the pool statement's context carries the
		// transaction's deadline.
		dl, ok := unbound.ctx().Deadline()
		if !ok || !dl.Equal(deadline) {
			t.Errorf("after Use(nil): deadline set=%v value=%v, want the transaction's %v",
				ok, dl, deadline)
		}
		return nil
	}); err != nil {
		t.Fatalf("RunTx: %v", err)
	}
}

// The contrast that gives the cell above its meaning: at the other two doors a
// nil transaction leaves NO transaction context behind, so neither carries a
// deadline into a pool statement.
func TestOnNilAndDAO_CarryNoTransactionDeadline(t *testing.T) {
	t.Parallel()

	s := buildSchema(newTxConn("db1"))
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(37*time.Minute))
	defer cancel()

	if err := RunTx(ctx, func(tx *Transaction) error {
		for name, d := range map[string]DAO[*artist, artistField, string]{
			"On(nil)": s.On(nil),
			"DAO()":   s.DAO(),
		} {
			q := d.(*queryDAO[*artist, artistField, artistSort, string])
			if q.ctxv != nil {
				t.Errorf("%s: ctxv = %v, want nil", name, q.ctxv)
			}
			if _, ok := q.ctx().Deadline(); ok {
				t.Errorf("%s: carries a deadline; only Use(nil) retains transaction context", name)
			}
			_ = tx
		}
		return nil
	}); err != nil {
		t.Fatalf("RunTx: %v", err)
	}
}

// SF1 (gold-man r0): the field-by-field comparison above is the fast lock but
// would not survive a struct refactor, and compares hooks by LENGTH. This is
// the durable one — it asserts BEHAVIOURAL equivalence: the same executor is
// reached and the same hooks fire, in the same order, through both doors.
func TestOnNil_BehavesIdenticallyToDAO(t *testing.T) {
	t.Parallel()

	run := func(acquire func(*Schema[*artist, artistField, artistSort, string], *[]string) DAO[*artist, artistField, string]) (int, int, []string) {
		var trace []string
		conn := newTxConn("db1")
		s := buildSchema(conn, Hooks[*artist, artistField, artistSort, string](
			recHook{name: "spy", trace: &trace}))
		if err := stageUpdate(acquire(s, &trace)); err != nil {
			t.Fatalf("Update: %v", err)
		}
		return conn.poolExecs, conn.beginCount, trace
	}

	pExec, pBegin, pTrace := run(func(s *Schema[*artist, artistField, artistSort, string], _ *[]string) DAO[*artist, artistField, string] {
		return s.DAO()
	})
	nExec, nBegin, nTrace := run(func(s *Schema[*artist, artistField, artistSort, string], _ *[]string) DAO[*artist, artistField, string] {
		return s.On(nil)
	})

	if nExec != pExec || nBegin != pBegin {
		t.Errorf("On(nil) routing = (pool %d, begin %d), DAO() = (pool %d, begin %d) — must match",
			nExec, nBegin, pExec, pBegin)
	}
	if len(nTrace) == 0 {
		t.Fatal("no hooks fired through DAO() — the instrument observes nothing, so the comparison below is vacuous")
	}
	if !slices.Equal(nTrace, pTrace) {
		t.Errorf("hook trace through On(nil) = %v, through DAO() = %v — must be identical", nTrace, pTrace)
	}
}
