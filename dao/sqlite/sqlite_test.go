package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/logger"
)

// These tests run in-process against a real SQLite database (a temp file), so the
// whole dao engine is exercised end-to-end with no external server.

type widget struct {
	ID   int64
	Name string
	Qty  int64
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
	wName: {Column: "name", Scan: func(w *widget) any { return &w.Name }, Value: func(w *widget) any { return w.Name }},
	wQty:  {Column: "qty", Scan: func(w *widget) any { return &w.Qty }, Value: func(w *widget) any { return w.Qty }},
}

const ddl = `CREATE TABLE widget (
	id   INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT UNIQUE NOT NULL,
	qty  INTEGER NOT NULL DEFAULT 0
)`

func setup(t *testing.T, opts ...dao.Option[*widget, widgetField, widgetSort, int64]) (dao.DataConn, *dao.Schema[*widget, widgetField, widgetSort, int64]) {
	t.Helper()
	ctx := context.Background()
	conn, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := conn.ExecContext(ctx, ddl); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	base := []dao.Option[*widget, widgetField, widgetSort, int64]{
		dao.Table[*widget, widgetField, widgetSort, int64]("widget"),
		dao.ID[*widget, widgetField, widgetSort, int64](wID),
		dao.Fields[*widget, widgetField, widgetSort, int64](widgetFields),
		dao.Default[*widget, widgetField, widgetSort, int64](wID, wName, wQty),
		dao.Conflict[*widget, widgetField, widgetSort, int64](wName),
	}
	return conn, dao.New(conn, append(base, opts...)...)
}

func TestSQLite_CRUDAndReturning(t *testing.T) {
	t.Parallel()
	_, s := setup(t)

	id, err := s.DAO().Set(wName, "alpha").Set(wQty, int64(3)).Insert()
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id <= 0 {
		t.Fatalf("RETURNING id = %d, want > 0", id)
	}

	got, err := s.DAO().With(wName, "alpha").Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != id || got.Name != "alpha" || got.Qty != 3 {
		t.Fatalf("Get = %+v", got)
	}

	if err := s.DAO().With(wID, id).Set(wQty, int64(9)).Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got, _ = s.DAO().With(wID, id).Get(); got.Qty != 9 {
		t.Errorf("after Update qty = %d, want 9", got.Qty)
	}

	if n, err := s.DAO().Count(); err != nil || n != 1 {
		t.Errorf("Count = %d, %v", n, err)
	}
	if ok, _ := s.DAO().With(wName, "alpha").Exists(); !ok {
		t.Error("Exists = false, want true")
	}

	if err := s.DAO().With(wID, id).Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n, _ := s.DAO().Count(); n != 0 {
		t.Errorf("after Delete count = %d, want 0", n)
	}
}

func TestSQLite_DuplicateTranslates(t *testing.T) {
	t.Parallel()
	_, s := setup(t)

	if _, err := s.DAO().Set(wName, "dup").Insert(); err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	_, err := s.DAO().Set(wName, "dup").Insert()
	if !errors.Is(err, dao.ErrDuplicate) {
		t.Fatalf("duplicate Insert err = %v, want Is dao.ErrDuplicate", err)
	}
}

func TestSQLite_Upsert(t *testing.T) {
	t.Parallel()
	_, s := setup(t)

	if _, err := s.DAO().Set(wName, "u").Set(wQty, int64(1)).Insert(); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := s.DAO().Set(wName, "u").Set(wQty, int64(42)).Upsert(); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got, _ := s.DAO().With(wName, "u").Get(); got.Qty != 42 {
		t.Errorf("after Upsert qty = %d, want 42", got.Qty)
	}
	if n, _ := s.DAO().Count(); n != 1 {
		t.Errorf("upsert added a row, count = %d", n)
	}
}

func TestSQLite_BatchChunked(t *testing.T) {
	t.Parallel()
	_, s := setup(t)

	b := s.DAO().Batch() // SQLite has no COPY → chunked multi-row INSERT
	for i := 0; i < 250; i++ {
		b.AddRow(&widget{Name: "w-" + itoa(i), Qty: int64(i)})
	}
	if err := b.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if n, _ := s.DAO().Count(); n != 250 {
		t.Errorf("after batch count = %d, want 250", n)
	}
}

func TestSQLite_Transaction(t *testing.T) {
	t.Parallel()
	conn, s := setup(t)

	// Commit.
	err := dao.RunTx(context.Background(), []dao.DataConn{conn}, func(tx *dao.Transaction) error {
		if _, e := s.On(tx).Set(wName, "a").Insert(); e != nil {
			return e
		}
		_, e := s.On(tx).Set(wName, "b").Insert()
		return e
	})
	if err != nil {
		t.Fatalf("RunTx commit: %v", err)
	}
	if n, _ := s.DAO().Count(); n != 2 {
		t.Fatalf("after commit count = %d, want 2", n)
	}

	// Rollback on a duplicate inside the tx.
	err = dao.RunTx(context.Background(), []dao.DataConn{conn}, func(tx *dao.Transaction) error {
		if _, e := s.On(tx).Set(wName, "c").Insert(); e != nil {
			return e
		}
		_, e := s.On(tx).Set(wName, "a").Insert() // duplicate → error → rollback
		return e
	})
	if !errors.Is(err, dao.ErrDuplicate) {
		t.Fatalf("RunTx rollback err = %v, want Is ErrDuplicate", err)
	}
	if n, _ := s.DAO().Count(); n != 2 {
		t.Errorf("rolled-back tx added a row: count = %d, want 2", n)
	}
}

// capLogger records statements so we can assert SQL+args logging fires.
type capLogger struct {
	sqls []string
	args [][]any
}

func (c *capLogger) Log(_ logger.Severity, payload any) {
	m, ok := payload.(map[string]any)
	if !ok {
		return
	}
	if s, ok := m["sql"].(string); ok {
		c.sqls = append(c.sqls, s)
	}
	if a, ok := m["args"].([]any); ok {
		c.args = append(c.args, a)
	}
}

func TestSQLite_DebugLoggingCapturesSQLAndArgs(t *testing.T) {
	t.Parallel()
	log := &capLogger{}
	_, s := setup(t,
		dao.WithLogger[*widget, widgetField, widgetSort, int64](log),
		dao.Debug[*widget, widgetField, widgetSort, int64](true),
	)

	if _, err := s.DAO().Set(wName, "logme").Set(wQty, int64(7)).Insert(); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if len(log.sqls) == 0 {
		t.Fatal("debug logging captured no statement")
	}
	// The logged SQL is the builder's output and carries the staged args.
	last := log.sqls[len(log.sqls)-1]
	if last == "" || log.args[len(log.args)-1] == nil {
		t.Errorf("logged sql/args empty: sql=%q args=%v", last, log.args)
	}
}

// itoa avoids importing strconv in this test file.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
