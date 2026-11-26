package view

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/dao/postgres"
	"github.com/yongjohnlee80/golib/dao/sqlite"
	"github.com/yongjohnlee80/golib/vfs/memfs"
)

func collect(t *testing.T, v *View, conn dao.DataConn, args ...any) []string {
	t.Helper()
	rows, err := v.WithArgs(args...).Query(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	var docs []string
	for x, err := range rows.All() {
		if err != nil {
			t.Fatal(err)
		}
		doc, err := v.Parse(x)
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}
	return docs
}

// A real driver: the row comes back as the database's JSON text, and the argument is bound, not
// spliced. The quote in the argument would break the query if it were spliced into its text.
func TestDriver_SQLite(t *testing.T) {
	t.Parallel()
	conn, err := sqlite.Open(context.Background(), ":memory:", sqlite.MaxOpenConns(1))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	v := mustNew(t, `---
version: 1
name: sqlite_view
source: sqlite
args: [ids]
process: |
  SELECT json_object('entity_id', value, 'title', 'Track ' || value)
  FROM json_each(?)
export: "track_{{.entity_id}}.json"
---
{"id": "{{.entity_id}}", "title": "{{.title}}"}`)

	docs := collect(t, v, conn, `["1","o'brien \"q\""]`)
	want := []string{`{"id": "1", "title": "Track 1"}`, `{"id": "o'brien \"q\"", "title": "Track o'brien \"q\""}`}
	if strings.Join(docs, "\n") != strings.Join(want, "\n") {
		t.Errorf("docs:\n%s\nwant:\n%s", strings.Join(docs, "\n"), strings.Join(want, "\n"))
	}

	dst := memfs.New()
	n, err := v.WithArgs(`["7"]`).Export(context.Background(), conn, dst)
	if err != nil || n != 1 {
		t.Fatalf("exported %d, %v", n, err)
	}
	if got := readFile(t, dst, "track_7.json"); got != `{"id": "7", "title": "Track 7"}` {
		t.Errorf("track_7.json = %q", got)
	}
}

func TestDriver_SQLiteRefusesTwoColumns(t *testing.T) {
	t.Parallel()
	conn, err := sqlite.Open(context.Background(), ":memory:", sqlite.MaxOpenConns(1))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	v := mustNew(t, "---\nversion: 1\nname: v\nsource: sqlite\nprocess: SELECT '{}', '{}'\n---\n")
	rows, err := v.Query(context.Background(), conn)
	if err == nil {
		_, err = rows.Next()
	}
	if err == nil || err == io.EOF {
		t.Errorf("a two-column result was read: %v", err)
	}
}

// Postgres: a jsonb column scans as its JSON text, and a Go slice binds as an array, so one view
// serves "every track of a label" and "these tracks" alike. Runs when TEST_PGURL is set.
func TestDriver_Postgres(t *testing.T) {
	t.Parallel()
	url := os.Getenv("TEST_PGURL")
	if url == "" {
		t.Skip("TEST_PGURL not set; skipping the postgres driver test")
	}
	conn, err := postgres.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	v := mustNew(t, `---
version: 1
name: pg_view
source: postgres
args: [entity_ids]
process: |
  SELECT jsonb_build_object('entity_id', id, 'bpm', 128, 'lyric', NULL, 'tags', jsonb_build_array('a', 'b'))
  FROM unnest($1::text[]) AS id
  ORDER BY id
---
{{.entity_id}}|{{.bpm}}|{{.lyric | default "instrumental"}}|{{range .tags}}{{.}}{{end}}`)

	docs := collect(t, v, conn, []string{"trk_2", "trk_1"})
	if got, want := strings.Join(docs, "\n"), "trk_1|128|instrumental|ab\ntrk_2|128|instrumental|ab"; got != want {
		t.Errorf("docs:\n%s\nwant:\n%s", got, want)
	}

	sqliteView := mustNew(t, "---\nversion: 1\nname: v\nsource: sqlite\nprocess: SELECT 1\n---\n")
	if _, err := sqliteView.Query(context.Background(), conn); err == nil {
		t.Error("a sqlite view ran on a postgres connection")
	}
}

// A query the database refuses as it starts has no result columns yet, and the driver holds the
// refusal until the rows are read. The view must report what the database said, and never call it
// a result of the wrong shape: an operator who forgot a grant would be sent to the view's SELECT
// list. Two refusals of that kind: a constant the planner can't compute, and a table the role may
// not read. Runs when TEST_PGURL is set.
func TestDriver_PostgresARefusalAtTheStartIsTheDatabasesError(t *testing.T) {
	t.Parallel()
	url := os.Getenv("TEST_PGURL")
	if url == "" {
		t.Skip("TEST_PGURL not set; skipping the postgres driver test")
	}
	ctx := context.Background()
	conn, err := postgres.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	refused := func(t *testing.T, conn dao.DataConn, process, want string) {
		t.Helper()
		v := mustNew(t, "---\nversion: 1\nname: refused\nsource: postgres\nprocess: "+process+"\n---\n")
		rows, err := v.Query(ctx, conn)
		if err == nil {
			_, err = rows.Next()
			_ = rows.Close()
		}
		switch {
		case err == nil || err == io.EOF:
			t.Fatalf("the refused query was read: %v", err)
		case errors.Is(err, ErrRow):
			t.Fatalf("the database's refusal was reported as a result of the wrong shape: %v", err)
		case !strings.Contains(err.Error(), want):
			t.Fatalf("the error is not the database's (%q): %v", want, err)
		}
	}

	t.Run("a constant that cannot be computed", func(t *testing.T) {
		refused(t, conn, "SELECT to_jsonb(1/0)", "division by zero")
	})

	t.Run("a table the role may not read", func(t *testing.T) {
		role, table := "view_refused_reader", "view_refused_rows"
		for _, q := range []string{
			"DROP TABLE IF EXISTS " + table,
			"DROP ROLE IF EXISTS " + role,
			"CREATE ROLE " + role,
			"CREATE TABLE " + table + " (doc jsonb)",
		} {
			if _, err := conn.ExecContext(ctx, q); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
		defer func() {
			_, _ = conn.ExecContext(ctx, "DROP TABLE IF EXISTS "+table)
			_, _ = conn.ExecContext(ctx, "DROP ROLE IF EXISTS "+role)
		}()
		// one connection, so the SET ROLE and the view's query share a session
		one, err := postgres.Open(ctx, url, postgres.MaxOpenConns(1))
		if err != nil {
			t.Fatal(err)
		}
		defer one.Close()
		if _, err := one.ExecContext(ctx, "SET ROLE "+role); err != nil {
			t.Fatal(err)
		}
		refused(t, one, "SELECT doc FROM "+table, "permission denied")
	})

	// a result that really has no column keeps its own refusal
	v := mustNew(t, "---\nversion: 1\nname: none\nsource: postgres\nprocess: SELECT FROM generate_series(1, 2)\n---\n")
	if rows, err := v.Query(ctx, conn); !errors.Is(err, ErrRow) {
		if err == nil {
			_ = rows.Close()
		}
		t.Errorf("a result with no column: %v, want ErrRow", err)
	}
}
