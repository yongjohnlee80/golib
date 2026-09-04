package dao

import (
	"context"
	"testing"
)

// The On(nil) contract (ADR-0019).
//
// A nil *Transaction passed to Schema.On means "no transaction is held" and
// routes to the pool, exactly as Schema.DAO does. It is load-bearing: every
// executor-parameter helper in every consumer — `func f(tx *dao.Transaction)`
// passing tx straight to On(tx) — is built on it, so a future maintainer
// "fixing" the fallthrough into a panic or an error would break all of them at
// once. These tests are the lock; the doc comment alone is not one.
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
