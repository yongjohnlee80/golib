//go:build integration

// Integration tests for the Postgres reference driver. They require a reachable
// Postgres; set TEST_PGURL (e.g. "postgres://user:pass@localhost:5432/db") and run:
//
//	go test -tags integration ./dao/postgres/
//
// Each run (re)creates and drops a scratch table; nothing else is touched.
package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

type widget struct {
	ID   int
	Name string
	Qty  int
}

type widgetField string

const (
	wID   widgetField = "id"
	wName widgetField = "name"
	wQty  widgetField = "qty"
)

type widgetSort string

var widgetFields = map[widgetField]dao.Field[*widget]{
	wID:   {Column: "id", Scan: func(w *widget) any { return &w.ID }},
	wName: {Column: "name", Scan: func(w *widget) any { return &w.Name }},
	wQty:  {Column: "qty", Scan: func(w *widget) any { return &w.Qty }},
}

const ddl = `CREATE TABLE golib_dao_widget (
	id   serial PRIMARY KEY,
	name text UNIQUE NOT NULL,
	qty  int NOT NULL DEFAULT 0
)`

func setup(t *testing.T) (dao.DataConn, *dao.Schema[*widget, widgetField, widgetSort, int]) {
	t.Helper()
	url := os.Getenv("TEST_PGURL")
	if url == "" {
		t.Skip("TEST_PGURL not set; skipping postgres integration tests")
	}
	ctx := context.Background()
	conn, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	exec := func(sql string) {
		if _, err := conn.ExecContext(ctx, sql); err != nil {
			t.Fatalf("setup %q: %v", sql, err)
		}
	}
	exec(`DROP TABLE IF EXISTS golib_dao_widget`)
	exec(ddl)
	t.Cleanup(func() {
		_, _ = conn.ExecContext(ctx, `DROP TABLE IF EXISTS golib_dao_widget`)
		_ = conn.Close()
	})

	s := dao.New[*widget, widgetField, widgetSort, int](conn,
		dao.Table[*widget, widgetField, widgetSort, int]("golib_dao_widget"),
		dao.ID[*widget, widgetField, widgetSort, int](wID),
		dao.Fields[*widget, widgetField, widgetSort, int](widgetFields),
		dao.Default[*widget, widgetField, widgetSort, int](wID, wName, wQty),
		dao.Conflict[*widget, widgetField, widgetSort, int](wName),
	)
	return conn, s
}

func truncate(t *testing.T, conn dao.DataConn) {
	t.Helper()
	if _, err := conn.ExecContext(context.Background(), `TRUNCATE golib_dao_widget RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func TestPG_CRUDRoundTrip(t *testing.T) {
	conn, s := setup(t)
	truncate(t, conn)

	// Insert with RETURNING id.
	id, err := s.DAO().Set(wName, "alpha").Set(wQty, 3).Insert()
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id <= 0 {
		t.Fatalf("RETURNING id = %d, want > 0", id)
	}

	// Get it back.
	got, err := s.DAO().With(wName, "alpha").Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != id || got.Name != "alpha" || got.Qty != 3 {
		t.Fatalf("Get = %+v", got)
	}

	// Update.
	if err := s.DAO().With(wID, id).Set(wQty, 9).Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = s.DAO().With(wID, id).Get()
	if got.Qty != 9 {
		t.Errorf("after Update qty = %d, want 9", got.Qty)
	}

	// Count / Exists.
	if n, err := s.DAO().Count(); err != nil || n != 1 {
		t.Errorf("Count = %d, %v", n, err)
	}
	if ok, err := s.DAO().With(wName, "alpha").Exists(); err != nil || !ok {
		t.Errorf("Exists = %v, %v", ok, err)
	}

	// Delete.
	if err := s.DAO().With(wID, id).Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n, _ := s.DAO().Count(); n != 0 {
		t.Errorf("after Delete count = %d, want 0", n)
	}
}

func TestPG_DuplicateTranslatesToErrDuplicate(t *testing.T) {
	conn, s := setup(t)
	truncate(t, conn)

	if _, err := s.DAO().Set(wName, "dup").Insert(); err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	_, err := s.DAO().Set(wName, "dup").Insert()
	if !errors.Is(err, dao.ErrDuplicate) {
		t.Fatalf("duplicate Insert err = %v, want Is dao.ErrDuplicate (SQLSTATE 23505)", err)
	}
	var ce *dao.ConstraintError
	if errors.As(err, &ce) && ce.Constraint == "" {
		t.Errorf("ConstraintError should carry the unique-index name, got %+v", ce)
	}
}

func TestPG_Upsert(t *testing.T) {
	conn, s := setup(t)
	truncate(t, conn)

	if _, err := s.DAO().Set(wName, "u").Set(wQty, 1).Insert(); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// Upsert on the unique name → updates qty.
	if err := s.DAO().Set(wName, "u").Set(wQty, 42).Upsert(); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, _ := s.DAO().With(wName, "u").Get()
	if got.Qty != 42 {
		t.Errorf("after Upsert qty = %d, want 42", got.Qty)
	}
	if n, _ := s.DAO().Count(); n != 1 {
		t.Errorf("upsert should not add a row, count = %d", n)
	}
}

func TestPG_BatchCopy(t *testing.T) {
	conn, s := setup(t)
	truncate(t, conn)

	b := s.DAO().Batch().ForceCopy() // native pgx CopyFrom path
	for i := 0; i < 500; i++ {
		b.Add(map[widgetField]any{wName: "copy-" + itoa(i), wQty: i})
	}
	if err := b.Flush(); err != nil {
		t.Fatalf("COPY Flush: %v", err)
	}
	if n, err := s.DAO().Count(); err != nil || n != 500 {
		t.Fatalf("after COPY count = %d, %v; want 500", n, err)
	}
}

func TestPG_BatchChunkedInsert(t *testing.T) {
	conn, s := setup(t)
	truncate(t, conn)

	b := s.DAO().Batch().ForceInsert() // chunked multi-row INSERT path
	for i := 0; i < 300; i++ {
		b.Add(map[widgetField]any{wName: "ins-" + itoa(i), wQty: i})
	}
	if err := b.Flush(); err != nil {
		t.Fatalf("chunked Flush: %v", err)
	}
	if n, _ := s.DAO().Count(); n != 300 {
		t.Errorf("after chunked insert count = %d, want 300", n)
	}
}

func TestPG_Transaction(t *testing.T) {
	conn, s := setup(t)
	truncate(t, conn)

	// Commit path.
	err := dao.RunTx(context.Background(), func(tx *dao.Transaction) error {
		if _, e := s.On(tx).Set(wName, "tx-a").Insert(); e != nil {
			return e
		}
		_, e := s.On(tx).Set(wName, "tx-b").Insert()
		return e
	})
	if err != nil {
		t.Fatalf("RunTx commit: %v", err)
	}
	if n, _ := s.DAO().Count(); n != 2 {
		t.Errorf("after committed tx count = %d, want 2", n)
	}

	// Rollback path: a duplicate inside the tx aborts both inserts.
	err = dao.RunTx(context.Background(), func(tx *dao.Transaction) error {
		if _, e := s.On(tx).Set(wName, "tx-c").Insert(); e != nil {
			return e
		}
		_, e := s.On(tx).Set(wName, "tx-a").Insert() // duplicate → error → rollback
		return e
	})
	if !errors.Is(err, dao.ErrDuplicate) {
		t.Fatalf("RunTx rollback err = %v, want Is ErrDuplicate", err)
	}
	if n, _ := s.DAO().Count(); n != 2 {
		t.Errorf("rolled-back tx should not have added tx-c: count = %d, want 2", n)
	}
}

// itoa is a tiny strconv.Itoa to avoid the import in a test file.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
