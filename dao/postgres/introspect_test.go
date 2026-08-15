package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/dao"
)

// pgxRows must satisfy dao.RowsColumns (ADR-0012).
var _ dao.RowsColumns = (*pgxRows)(nil)

// testConn opens a connection from TEST_PGURL, skipping when unset (the
// package's env-gated integration convention).
func testConn(t *testing.T) dao.DataConn {
	t.Helper()
	dsn := os.Getenv("TEST_PGURL")
	if dsn == "" {
		t.Skip("TEST_PGURL not set; skipping postgres integration test")
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

func TestIntrospection_Postgres(t *testing.T) {
	ctx := context.Background()
	conn := testConn(t)

	table := fmt.Sprintf("dao_introspect_%d", time.Now().UnixNano())
	setup := fmt.Sprintf(`CREATE TABLE public.%s (
		id BIGSERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		uri TEXT DEFAULT 'x',
		note TEXT
	)`, table)
	if _, err := conn.ExecContext(ctx, setup); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "DROP TABLE IF EXISTS public."+table)
	})

	schemas, err := dao.ListSchemas(ctx, conn)
	if err != nil {
		t.Fatalf("ListSchemas: %v", err)
	}
	foundPublic := false
	for _, s := range schemas {
		if s.Name == "public" {
			foundPublic = true
		}
	}
	if !foundPublic {
		t.Errorf("ListSchemas = %v, want to contain public", schemas)
	}

	tables, err := dao.ListTables(ctx, conn, "")
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	foundTable := false
	for _, tb := range tables {
		if tb.Name == table && tb.Kind == dao.TableKindTable && tb.Schema == "public" {
			foundTable = true
		}
	}
	if !foundTable {
		t.Errorf("ListTables missing %s: %+v", table, tables)
	}

	cols, err := dao.ListColumns(ctx, conn, "public", table)
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	if len(cols) != 4 {
		t.Fatalf("ListColumns = %+v, want 4 columns", cols)
	}
	id, name, uri, note := cols[0], cols[1], cols[2], cols[3]
	if id.Name != "id" || !id.PrimaryKey || id.Nullable || !id.HasDefault {
		t.Errorf("id = %+v, want serial pk", id)
	}
	if name.Name != "name" || name.Nullable {
		t.Errorf("name = %+v, want NOT NULL", name)
	}
	if uri.Name != "uri" || !uri.HasDefault {
		t.Errorf("uri = %+v, want default", uri)
	}
	if note.Name != "note" || !note.Nullable || note.HasDefault {
		t.Errorf("note = %+v, want nullable no-default", note)
	}
}

func TestColumns_RawQuery_Postgres(t *testing.T) {
	ctx := context.Background()
	conn := testConn(t)

	rows, err := conn.QueryContext(ctx, `SELECT 1 AS id, 'a'::text AS name`)
	if err != nil {
		t.Fatalf("QueryContext: %v", err)
	}
	defer rows.Close()

	cols, err := dao.Columns(rows)
	if err != nil {
		t.Fatalf("dao.Columns: %v", err)
	}
	if len(cols) != 2 || cols[0] != "id" || cols[1] != "name" {
		t.Errorf("Columns = %v, want [id name]", cols)
	}
}
