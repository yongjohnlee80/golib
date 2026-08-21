package mysql

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/dao"
)

// Integration tests, gated on TEST_MYSQL_DSN (skip cleanly when unset), e.g.
//
//	TEST_MYSQL_DSN='root:secret@tcp(localhost:3306)/example?parseTime=true'
//
// They create and drop their own tables (the dao/postgres convention).

type track struct {
	ID   int64
	Name string
	URI  string
}

type trackField string

const (
	tID   trackField = "id"
	tName trackField = "name"
	tURI  trackField = "uri"
)

type trackSort string

func testConn(t *testing.T) dao.DataConn {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set; skipping mysql integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// newTable creates a uniquely-named test table and returns its name plus a
// schema over it.
func newTable(t *testing.T, conn dao.DataConn) (string, *dao.Schema[*track, trackField, trackSort, int64]) {
	t.Helper()
	ctx := context.Background()
	table := fmt.Sprintf("dao_mysql_%d", time.Now().UnixNano())
	setup := fmt.Sprintf("CREATE TABLE %s ("+
		"id BIGINT AUTO_INCREMENT PRIMARY KEY, "+
		"name VARCHAR(120) NOT NULL, "+
		"uri VARCHAR(120) NOT NULL, "+
		"UNIQUE KEY uq_uri (uri))", table)
	if _, err := conn.ExecContext(ctx, setup); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table)
	})
	s := dao.New(conn,
		dao.Table[*track, trackField, trackSort, int64](table),
		dao.ID[*track, trackField, trackSort, int64](tID),
		dao.Fields[*track, trackField, trackSort, int64](map[trackField]dao.Field[*track]{
			tID:   {Column: "id", Scan: func(r *track) any { return &r.ID }},
			tName: {Column: "name", Scan: func(r *track) any { return &r.Name }, Value: func(r *track) any { return r.Name }},
			tURI:  {Column: "uri", Scan: func(r *track) any { return &r.URI }, Value: func(r *track) any { return r.URI }},
		}),
		dao.Default[*track, trackField, trackSort, int64](tID, tName, tURI),
		dao.Conflict[*track, trackField, trackSort, int64](tURI),
	)
	return table, s
}

func TestMysql_InsertLastInsertIDAndReads(t *testing.T) {
	conn := testConn(t)
	_, s := newTable(t, conn)

	id, err := s.DAO().Set(tName, "Alpha").Set(tURI, "alpha").Insert()
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id <= 0 {
		t.Fatalf("Insert id = %d, want > 0 (LastInsertId profile)", id)
	}

	got, err := s.DAO().With(tID, id).Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Alpha" || got.URI != "alpha" {
		t.Errorf("Get = %+v", got)
	}

	if err := s.DAO().With(tID, id).Set(tName, "Beta").Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	n, err := s.DAO().Count()
	if err != nil || n != 1 {
		t.Fatalf("Count = %d, %v", n, err)
	}
	if err := s.DAO().With(tID, id).Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestMysql_UpsertAndDuplicateTranslation(t *testing.T) {
	conn := testConn(t)
	_, s := newTable(t, conn)

	if _, err := s.DAO().Set(tName, "Alpha").Set(tURI, "alpha").Insert(); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// A plain duplicate insert translates to ErrDuplicate.
	if _, err := s.DAO().Set(tName, "Other").Set(tURI, "alpha").Insert(); !errors.Is(err, dao.ErrDuplicate) {
		t.Fatalf("duplicate Insert err = %v, want ErrDuplicate", err)
	}

	// Upsert on the unique key updates in place.
	if err := s.DAO().Set(tName, "Updated").Set(tURI, "alpha").Upsert(); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := s.DAO().With(tURI, "alpha").Get()
	if err != nil || got.Name != "Updated" {
		t.Fatalf("post-upsert Get = %+v, %v", got, err)
	}
}

func TestMysql_BatchSkipAndUpdate(t *testing.T) {
	conn := testConn(t)
	_, s := newTable(t, conn)

	b := s.DAO().Batch()
	b.AddRow(&track{Name: "A", URI: "a"}).AddRow(&track{Name: "B", URI: "b"})
	if err := b.Flush(); err != nil {
		t.Fatalf("batch insert: %v", err)
	}

	// SkipConflicts: the duplicate row is ignored, the fresh one lands.
	b2 := s.DAO().Batch()
	b2.AddRow(&track{Name: "A2", URI: "a"}).AddRow(&track{Name: "C", URI: "c"})
	if err := b2.SkipConflicts().Flush(); err != nil {
		t.Fatalf("batch skip: %v", err)
	}
	if got, _ := s.DAO().With(tURI, "a").Get(); got.Name != "A" {
		t.Errorf("skip-conflicts overwrote: %+v", got)
	}
	if n, _ := s.DAO().Count(); n != 3 {
		t.Errorf("Count = %d, want 3", n)
	}

	// OnConflictUpdate refreshes existing rows.
	b3 := s.DAO().Batch()
	b3.AddRow(&track{Name: "A3", URI: "a"})
	if err := b3.OnConflictUpdate(tURI).Flush(); err != nil {
		t.Fatalf("batch upsert: %v", err)
	}
	if got, _ := s.DAO().With(tURI, "a").Get(); got.Name != "A3" {
		t.Errorf("OnConflictUpdate did not refresh: %+v", got)
	}
}

func TestMysql_Transactions(t *testing.T) {
	conn := testConn(t)
	_, s := newTable(t, conn)
	ctx := context.Background()

	// Commit path.
	err := dao.RunTx(ctx, func(tx *dao.Transaction) error {
		_, err := s.On(tx).Set(tName, "T1").Set(tURI, "t1").Insert()
		return err
	})
	if err != nil {
		t.Fatalf("RunTx commit: %v", err)
	}

	// Rollback path.
	boom := errors.New("boom")
	err = dao.RunTx(ctx, func(tx *dao.Transaction) error {
		if _, err := s.On(tx).Set(tName, "T2").Set(tURI, "t2").Insert(); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("RunTx rollback err = %v, want boom", err)
	}
	if n, _ := s.DAO().Count(); n != 1 {
		t.Errorf("Count after rollback = %d, want 1", n)
	}
}

func TestMysql_IntrospectionAndColumns(t *testing.T) {
	conn := testConn(t)
	table, _ := newTable(t, conn)
	ctx := context.Background()

	tables, err := dao.ListTables(ctx, conn, "")
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	found := false
	for _, tb := range tables {
		if tb.Name == table && tb.Kind == dao.TableKindTable {
			found = true
		}
	}
	if !found {
		t.Errorf("ListTables missing %s", table)
	}

	cols, err := dao.ListColumns(ctx, conn, "", table)
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	if len(cols) != 3 || cols[0].Name != "id" || !cols[0].PrimaryKey || cols[1].Nullable {
		t.Errorf("ListColumns = %+v", cols)
	}

	rows, err := conn.QueryContext(ctx, "SELECT id, name FROM "+table)
	if err != nil {
		t.Fatalf("raw query: %v", err)
	}
	defer rows.Close()
	names, err := dao.Columns(rows)
	if err != nil || len(names) != 2 || names[0] != "id" || names[1] != "name" {
		t.Errorf("dao.Columns = %v, %v", names, err)
	}
}
