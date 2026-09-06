//go:build integration

// BROKEN UNDER THE `integration` TAG, and has been for a while. The dao.RunTx
// call below passes `[]dao.DataConn`, which is the OLD executor signature, so
// this file does not compile: `go vet -tags integration ./...` inside this
// module fails at the RunTx call.
//
// go.mod requires golib v0.5.9 and an earlier version of this note claimed the
// pin was why the old signature was still correct. It is not: go.mod ALSO
// carries `replace github.com/yongjohnlee80/golib => ../..`, and a replace
// beats a require, so this module builds against the LOCAL tree and gets the
// current variadic signature. The pin is inert.
//
// The fix is `dao.RunTx(ctx, fn)`. It is left alone here because these tests
// need real BigQuery credentials to run at all, so nobody has been able to
// confirm the migrated call against a live dataset.
//
// Integration tests for the BigQuery driver. They run against a REAL dataset and
// are gated on credentials, so they are excluded from normal builds (the
// `integration` build tag) and skip when the env is unset.
//
//	BQ_TEST_PROJECT=my-proj BQ_TEST_DATASET=my_dataset \
//	  go test -tags=integration ./dao/bigquery/...
//
// Auth uses Application Default Credentials (GOOGLE_APPLICATION_CREDENTIALS or
// `gcloud auth application-default login`). The suite creates and drops its own
// table, so it needs dataset write + DDL permission.
package bigquery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

type row struct {
	ID   string
	Name string
	N    int64
}

type rowField string

const (
	fID   rowField = "id"
	fName rowField = "name"
	fN    rowField = "n"
)

type rowSort string

const tableName = "golib_dao_it"

func rowFields() map[rowField]dao.Field[*row] {
	return map[rowField]dao.Field[*row]{
		fID:   {Column: "id", Scan: func(r *row) any { return &r.ID }, Value: func(r *row) any { return r.ID }},
		fName: {Column: "name", Scan: func(r *row) any { return &r.Name }, Value: func(r *row) any { return r.Name }},
		fN:    {Column: "n", Scan: func(r *row) any { return &r.N }, Value: func(r *row) any { return r.N }},
	}
}

func itConn(t *testing.T) (dao.DataConn, string) {
	t.Helper()
	project, dataset := os.Getenv("BQ_TEST_PROJECT"), os.Getenv("BQ_TEST_DATASET")
	if project == "" || dataset == "" {
		t.Skip("BQ_TEST_PROJECT/BQ_TEST_DATASET not set; skipping BigQuery integration tests")
	}
	conn, err := Open(context.Background(), project, dataset)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return conn, dataset
}

func exec(t *testing.T, conn dao.DataConn, sql string) {
	t.Helper()
	if _, err := conn.ExecContext(context.Background(), sql); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func TestIntegration_CRUDAndCapabilities(t *testing.T) {
	conn, _ := itConn(t)
	defer conn.Close()

	exec(t, conn, fmt.Sprintf("DROP TABLE IF EXISTS `%s`", tableName))
	exec(t, conn, fmt.Sprintf("CREATE TABLE `%s` (id STRING, name STRING, n INT64)", tableName))
	defer exec(t, conn, fmt.Sprintf("DROP TABLE IF EXISTS `%s`", tableName))

	s := dao.New[*row, rowField, rowSort, string](conn,
		dao.Table[*row, rowField, rowSort, string](tableName),
		dao.ID[*row, rowField, rowSort, string](fID),
		dao.Fields[*row, rowField, rowSort, string](rowFields()),
		dao.Default[*row, rowField, rowSort, string](fID, fName, fN),
		dao.Conflict[*row, rowField, rowSort, string](fID),
	)

	// Insert returns (zero ID, nil): BigQuery generates no server-side id.
	id, err := s.DAO().Set(fID, "a").Set(fName, "Alpha").Set(fN, int64(1)).Insert()
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id != "" {
		t.Errorf("Insert id = %q, want zero (no generated id on BigQuery)", id)
	}
	if _, err := s.DAO().Set(fID, "b").Set(fName, "Beta").Set(fN, int64(2)).Insert(); err != nil {
		t.Fatalf("Insert b: %v", err)
	}

	// Count + Select read back.
	n, err := s.DAO().Count()
	if err != nil || n != 2 {
		t.Fatalf("Count = %d, %v; want 2", n, err)
	}
	got, err := s.DAO().With(fID, "a").Get()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Alpha" || got.N != 1 {
		t.Errorf("Get = %+v", got)
	}

	// Update + Delete via DML.
	if err := s.DAO().With(fID, "a").Set(fName, "Alpha2").Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := s.DAO().With(fID, "b").Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if n, _ := s.DAO().Count(); n != 1 {
		t.Errorf("post-delete Count = %d, want 1", n)
	}

	// Capability gates: all must be dao.ErrUnsupported, and none may panic.
	if err := s.DAO().Set(fID, "c").Set(fName, "C").Upsert(); !errors.Is(err, dao.ErrUnsupported) {
		t.Errorf("Upsert err = %v, want ErrUnsupported", err)
	}
	if _, err := conn.Begin(context.Background()); !errors.Is(err, dao.ErrUnsupported) {
		t.Errorf("Begin err = %v, want ErrUnsupported", err)
	}
	txErr := dao.RunTx(context.Background(), []dao.DataConn{conn}, func(tx *dao.Transaction) error {
		_, e := s.On(tx).Set(fID, "d").Set(fName, "D").Insert()
		return e
	})
	if !errors.Is(txErr, dao.ErrUnsupported) {
		t.Errorf("RunTx err = %v, want ErrUnsupported", txErr)
	}
}
