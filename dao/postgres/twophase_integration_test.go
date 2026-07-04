//go:build integration

// Two-phase-commit integration tests (ADR-0005 §2.3 / acceptance criterion 6).
// They require TEST_PGURL and a server started with max_prepared_transactions > 0
// (postgres defaults it to 0; e.g. docker: -c max_prepared_transactions=10). When
// the server has it disabled the tests skip with an explanatory message.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

// openTP opens a named test connection, skipping unless TEST_PGURL is set.
func openTP(t *testing.T, name string) dao.DataConn {
	t.Helper()
	url := os.Getenv("TEST_PGURL")
	if url == "" {
		t.Skip("TEST_PGURL not set; skipping postgres integration tests")
	}
	conn, err := OpenNamed(context.Background(), name, url)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// requirePreparedTx skips unless the server allows prepared transactions.
func requirePreparedTx(t *testing.T, conn dao.DataConn) {
	t.Helper()
	rows, err := conn.QueryContext(context.Background(), "SHOW max_prepared_transactions")
	if err != nil {
		t.Fatalf("SHOW max_prepared_transactions: %v", err)
	}
	defer rows.Close()
	var v string
	if rows.Next() {
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}
	if v == "" || v == "0" {
		t.Skipf("server has max_prepared_transactions=%q; enable it (e.g. postgres -c max_prepared_transactions=10) to run 2PC tests", v)
	}
}

func mustExecTP(t *testing.T, conn dao.DataConn, sql string) {
	t.Helper()
	if _, err := conn.ExecContext(context.Background(), sql); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func tpSchema(t *testing.T, conn dao.DataConn, table string) *dao.Schema[*widget, widgetField, widgetSort, int] {
	t.Helper()
	mustExecTP(t, conn, "DROP TABLE IF EXISTS "+table)
	mustExecTP(t, conn, "CREATE TABLE "+table+" (id serial PRIMARY KEY, name text NOT NULL, qty int NOT NULL DEFAULT 0)")
	t.Cleanup(func() { mustExecTP(t, conn, "DROP TABLE IF EXISTS "+table) })

	return dao.New[*widget, widgetField, widgetSort, int](conn,
		dao.Table[*widget, widgetField, widgetSort, int](table),
		dao.ID[*widget, widgetField, widgetSort, int](wID),
		dao.Fields[*widget, widgetField, widgetSort, int](widgetFields),
		dao.Default[*widget, widgetField, widgetSort, int](wID, wName, wQty),
	)
}

func countRows(t *testing.T, conn dao.DataConn, table string) int {
	t.Helper()
	rows, err := conn.QueryContext(context.Background(), "SELECT count(*) FROM "+table)
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	defer rows.Close()
	n := 0
	if rows.Next() {
		var c int64
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan count: %v", err)
		}
		n = int(c)
	}
	return n
}

// preparedXactCount returns how many golib-dao prepared transactions the server
// currently holds — after every test this must be zero (nothing leaked/wedged).
func preparedXactCount(t *testing.T, conn dao.DataConn) int {
	t.Helper()
	rows, err := conn.QueryContext(context.Background(),
		"SELECT count(*) FROM pg_prepared_xacts WHERE gid LIKE 'golib-dao-%'")
	if err != nil {
		t.Fatalf("pg_prepared_xacts: %v", err)
	}
	defer rows.Close()
	n := 0
	if rows.Next() {
		var c int64
		_ = rows.Scan(&c)
		n = int(c)
	}
	return n
}

func TestTwoPhase_CommitAcrossTwoConnections(t *testing.T) {
	connA := openTP(t, "pg-a")
	requirePreparedTx(t, connA)
	connB := openTP(t, "pg-b")

	sa := tpSchema(t, connA, "tp_widgets_a")
	sb := tpSchema(t, connB, "tp_widgets_b")

	err := dao.RunTx(context.Background(), []dao.DataConn{connA, connB}, func(tx *dao.Transaction) error {
		tx.TwoPhase()
		if _, err := sa.On(tx).Set(wName, "alpha").Set(wQty, 1).Insert(); err != nil {
			return err
		}
		if _, err := sb.On(tx).Set(wName, "beta").Set(wQty, 2).Insert(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("two-phase RunTx: %v", err)
	}

	if got := countRows(t, connA, "tp_widgets_a"); got != 1 {
		t.Errorf("table a rows = %d, want 1", got)
	}
	if got := countRows(t, connB, "tp_widgets_b"); got != 1 {
		t.Errorf("table b rows = %d, want 1", got)
	}
	if n := preparedXactCount(t, connA); n != 0 {
		t.Errorf("%d golib-dao prepared transactions left on the server after commit", n)
	}
}

func TestTwoPhase_BodyErrorLeavesNothingAnywhere(t *testing.T) {
	connA := openTP(t, "pg-a")
	requirePreparedTx(t, connA)
	connB := openTP(t, "pg-b")

	sa := tpSchema(t, connA, "tp_widgets_a")
	sb := tpSchema(t, connB, "tp_widgets_b")

	boom := errors.New("business rule failed")
	err := dao.RunTx(context.Background(), []dao.DataConn{connA, connB}, func(tx *dao.Transaction) error {
		tx.TwoPhase()
		if _, err := sa.On(tx).Set(wName, "alpha").Set(wQty, 1).Insert(); err != nil {
			return err
		}
		if _, err := sb.On(tx).Set(wName, "beta").Set(wQty, 2).Insert(); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the body error", err)
	}

	if got := countRows(t, connA, "tp_widgets_a"); got != 0 {
		t.Errorf("table a rows = %d, want 0 (rolled back)", got)
	}
	if got := countRows(t, connB, "tp_widgets_b"); got != 0 {
		t.Errorf("table b rows = %d, want 0 (rolled back)", got)
	}
	if n := preparedXactCount(t, connA); n != 0 {
		t.Errorf("%d golib-dao prepared transactions left on the server after rollback", n)
	}
}

func TestTwoPhase_PrepareFailureRollsBackPrepared(t *testing.T) {
	connA := openTP(t, "pg-a")
	requirePreparedTx(t, connA)
	connB := openTP(t, "pg-b")

	sa := tpSchema(t, connA, "tp_widgets_a")
	sb := tpSchema(t, connB, "tp_widgets_b")

	// Force phase one to fail on connB: an errored statement leaves the whole
	// Postgres transaction in an aborted state, so its PREPARE TRANSACTION
	// fails while connA's succeeds — exercising the rollback-of-prepared path.
	err := dao.RunTx(context.Background(), []dao.DataConn{connA, connB}, func(tx *dao.Transaction) error {
		tx.TwoPhase()
		if _, err := sa.On(tx).Set(wName, "alpha").Set(wQty, 1).Insert(); err != nil {
			return err
		}
		if _, err := sb.On(tx).Set(wName, "beta").Set(wQty, 2).Insert(); err != nil {
			return err
		}
		// Poison connB's transaction; swallow the error so Commit still runs.
		if _, err := sb.On(tx).WithPredicate(dao.Raw("no_such_column = ?", 1)).Select(); err == nil {
			return fmt.Errorf("expected the poison statement to fail")
		}
		return nil // body succeeds; Commit's phase one must now fail on pg-b
	})

	var ce *dao.CommitError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *dao.CommitError from phase one", err)
	}
	if len(ce.AlreadyDurable) != 0 {
		t.Errorf("AlreadyDurable = %v, want none", ce.AlreadyDurable)
	}
	if got := countRows(t, connA, "tp_widgets_a"); got != 0 {
		t.Errorf("table a rows = %d, want 0 (prepared then rolled back)", got)
	}
	if got := countRows(t, connB, "tp_widgets_b"); got != 0 {
		t.Errorf("table b rows = %d, want 0", got)
	}
	if n := preparedXactCount(t, connA); n != 0 {
		t.Errorf("%d golib-dao prepared transactions left on the server", n)
	}
}
