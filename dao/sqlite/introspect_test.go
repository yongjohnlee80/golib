package sqlite

import (
	"context"
	"database/sql"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

// *sql.Rows must keep satisfying dao.RowsColumns (ADR-0012).
var _ dao.RowsColumns = (*sql.Rows)(nil)

// openMem opens an in-memory database with a single connection so every
// statement sees the same schema.
func openMem(t *testing.T) dao.DataConn {
	t.Helper()
	conn, err := Open(context.Background(), ":memory:", MaxOpenConns(1))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestIntrospection_Sqlite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn := openMem(t)

	for _, stmt := range []string{
		`CREATE TABLE artist (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			uri TEXT DEFAULT 'x',
			note TEXT
		)`,
		`CREATE VIEW artist_names AS SELECT name FROM artist`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}

	schemas, err := dao.ListSchemas(ctx, conn)
	if err != nil {
		t.Fatalf("ListSchemas: %v", err)
	}
	foundMain := false
	for _, s := range schemas {
		if s.Name == "main" {
			foundMain = true
		}
	}
	if !foundMain {
		t.Errorf("ListSchemas = %v, want to contain main", schemas)
	}

	tables, err := dao.ListTables(ctx, conn, "")
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if len(tables) != 2 ||
		tables[0].Name != "artist" || tables[0].Kind != dao.TableKindTable ||
		tables[1].Name != "artist_names" || tables[1].Kind != dao.TableKindView {
		t.Errorf("ListTables = %+v, want [artist table, artist_names view]", tables)
	}

	cols, err := dao.ListColumns(ctx, conn, "", "artist")
	if err != nil {
		t.Fatalf("ListColumns: %v", err)
	}
	if len(cols) != 4 {
		t.Fatalf("ListColumns = %+v, want 4 columns", cols)
	}
	id, name, uri, note := cols[0], cols[1], cols[2], cols[3]
	if id.Name != "id" || !id.PrimaryKey || id.Position != 1 {
		t.Errorf("id = %+v, want pk at position 1", id)
	}
	if name.Name != "name" || name.Nullable || name.PrimaryKey {
		t.Errorf("name = %+v, want NOT NULL non-pk", name)
	}
	if uri.Name != "uri" || !uri.HasDefault || uri.Default != "'x'" {
		t.Errorf("uri = %+v, want default 'x'", uri)
	}
	if note.Name != "note" || !note.Nullable || note.HasDefault {
		t.Errorf("note = %+v, want nullable no-default", note)
	}
}

func TestColumns_RawQuery_Sqlite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn := openMem(t)

	rows, err := conn.QueryContext(ctx, `SELECT 1 AS id, 'a' AS name`)
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
