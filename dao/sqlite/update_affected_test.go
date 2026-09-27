package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/dao"
)

type casRow struct {
	ID    int64
	State string
}

type casField string

const (
	casID    casField = "id"
	casState casField = "state"
)

type casSort string

// The compare-and-set UpdateAffected exists for, against the real driver: the
// first conditioned update changes the row, the second finds the condition
// false and says so with 0.
func TestUpdateAffected_ACompareAndSetAgainstTheRealDriver(t *testing.T) {
	ctx := context.Background()
	conn := openMem(t)
	table := fmt.Sprintf("casrow_%d", time.Now().UnixNano())
	if _, err := conn.ExecContext(ctx, "CREATE TABLE "+table+" (id BIGINT PRIMARY KEY, state VARCHAR(16) NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = conn.ExecContext(context.Background(), "DROP TABLE "+table) })
	if _, err := conn.ExecContext(ctx, "INSERT INTO "+table+" (id, state) VALUES (1, 'open')"); err != nil {
		t.Fatal(err)
	}
	s := dao.New[*casRow, casField, casSort, int64](conn,
		dao.Table[*casRow, casField, casSort, int64](table),
		dao.ID[*casRow, casField, casSort, int64](casID),
		dao.Fields[*casRow, casField, casSort, int64](map[casField]dao.Field[*casRow]{
			casID:    {Column: "id", Scan: func(r *casRow) any { return &r.ID }},
			casState: {Column: "state", Scan: func(r *casRow) any { return &r.State }, Value: func(r *casRow) any { return r.State }},
		}))
	claim := func() (int64, error) {
		return dao.UpdateAffected(s.OnCtx(ctx).With(casID, int64(1)).With(casState, "open").Set(casState, "closed"))
	}
	if n, err := claim(); err != nil || n != 1 {
		t.Fatalf("first claim: %d, %v; want 1 row changed", n, err)
	}
	if n, err := claim(); err != nil || n != 0 {
		t.Fatalf("second claim: %d, %v; want 0 — the row is no longer open", n, err)
	}
	// Setting a row to the values it already has: PostgreSQL and SQLite count
	// the MATCHED row; MySQL counts CHANGED rows unless the DSN asks for found
	// rows (clientFoundRows=true). A compare-and-set whose Set always changes
	// the row is unaffected; this pins the difference so it stays documented.
	n, err := dao.UpdateAffected(s.OnCtx(ctx).With(casID, int64(1)).With(casState, "closed").Set(casState, "closed"))
	if err != nil || n != 1 {
		t.Errorf("an unchanged-values update: %d, %v; want 1 on sqlite", n, err)
	}
}
