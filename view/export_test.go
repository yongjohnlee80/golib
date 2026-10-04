package view

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/vfs"
	"github.com/yongjohnlee80/golib/vfs/memfs"
)

func readFile(t *testing.T, fsys vfs.FS, name string) string {
	t.Helper()
	r, err := fsys.Open(context.Background(), name, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exportView(t *testing.T, export, body string) *View {
	t.Helper()
	return mustNew(t, "---\nversion: 1\nname: v\nsource: sqlite\nprocess: SELECT 1\nexport: "+export+"\n---\n"+body)
}

func TestExport_WritesEachDocumentAtItsName(t *testing.T) {
	t.Parallel()
	v := exportView(t, `"{{.kind}}/track_{{.id}}.md"`, "# {{.t}}\n")
	conn := &fakeConn{dialect: "sqlite", rows: []any{`{"kind":"tracks","id":1,"t":"A"}`, `{"kind":"tracks","id":2,"t":"B"}`, `{"kind":"x/y","id":3,"t":"C"}`}}
	dst := memfs.New()
	n, err := v.WithArgs().Export(context.Background(), conn, dst)
	if err != nil || n != 3 {
		t.Fatalf("exported %d, %v", n, err)
	}
	for name, want := range map[string]string{"tracks/track_1.md": "# A\n", "tracks/track_2.md": "# B\n", "x/y/track_3.md": "# C\n"} {
		if got := readFile(t, dst, name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestExport_NoExportDeclared(t *testing.T) {
	t.Parallel()
	v := mustNew(t, "---\nversion: 1\nname: v\nsource: sqlite\nprocess: SELECT 1\n---\n")
	conn := &fakeConn{dialect: "sqlite"}
	if _, err := v.WithArgs().Export(context.Background(), conn, memfs.New()); !errors.Is(err, ErrNoExport) {
		t.Errorf("err = %v, want ErrNoExport", err)
	}
	if conn.query != "" {
		t.Error("the query ran for a view with nothing to export")
	}
}

// A name rendered from database values cannot place a file outside the destination.
func TestExport_RefusesNamesThatLeaveTheDestination(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"../escape", "/abs", "a/../../b", ""} {
		v := exportView(t, `"{{.id}}"`, "x")
		conn := &fakeConn{dialect: "sqlite", rows: []any{`{"id":"ok"}`, `{"id":"` + id + `"}`}}
		dst := memfs.New()
		n, err := v.WithArgs().Export(context.Background(), conn, dst)
		if err == nil {
			t.Errorf("%q: exported", id)
			continue
		}
		if n != 1 || !strings.Contains(err.Error(), "row 2") {
			t.Errorf("%q: exported %d, %v; want the first written and the second named", id, n, err)
		}
	}
}

// A document whose render fails partway is never left half written: the old file stays.
func TestExport_AFailedRenderLeavesTheOldDocument(t *testing.T) {
	t.Parallel()
	v := exportView(t, `"{{.id}}.md"`, "{{.t}} {{index .list 3}}")
	dst := memfs.New()
	if _, err := dst.WriteFile(context.Background(), "1.md", strings.NewReader("old")); err != nil {
		t.Fatal(err)
	}
	conn := &fakeConn{dialect: "sqlite", rows: []any{`{"id":1,"t":"new","list":[]}`}}
	if _, err := v.WithArgs().Export(context.Background(), conn, dst); err == nil {
		t.Fatal("exported a document whose render failed")
	}
	if got := readFile(t, dst, "1.md"); got != "old" {
		t.Errorf("1.md = %q, want the old content", got)
	}
}

func TestExport_StopsAtTheFirstError(t *testing.T) {
	t.Parallel()
	v := exportView(t, `"{{.id}}.md"`, "x")
	for name, tc := range map[string]struct {
		conn *fakeConn
		args []any
		n    int
	}{
		"bad row":         {&fakeConn{dialect: "sqlite", rows: []any{`{"id":1}`, nil, `{"id":3}`}}, nil, 1},
		"name not JSON":   {&fakeConn{dialect: "sqlite", rows: []any{`{"id":1}`, `[1,`}}, nil, 1},
		"name render":     {&fakeConn{dialect: "sqlite", rows: []any{`7`}}, nil, 0},
		"refused query":   {&fakeConn{dialect: "postgres"}, nil, 0},
		"wrong arg count": {&fakeConn{dialect: "sqlite"}, []any{1}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dst := memfs.New()
			n, err := v.WithArgs(tc.args...).Export(context.Background(), tc.conn, dst)
			if err == nil || n != tc.n {
				t.Errorf("exported %d, %v; want %d and an error", n, err, tc.n)
			}
		})
	}
}

// The export name is text: a value printed into it is not escaped for the body's format.
func TestExport_NameIsNotEscapedForTheBody(t *testing.T) {
	t.Parallel()
	v := exportView(t, `"{{.id}}.json"`, `{"t": "{{.t}}"}`)
	conn := &fakeConn{dialect: "sqlite", rows: []any{`{"id":"a&b","t":"q\"q"}`}}
	dst := memfs.New()
	if _, err := v.WithArgs().Export(context.Background(), conn, dst); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, dst, "a&b.json"); got != `{"t": "q\"q"}` {
		t.Errorf("a&b.json = %q", got)
	}
}

func TestExport_ContextCancelled(t *testing.T) {
	t.Parallel()
	v := exportView(t, `"{{.id}}.md"`, "x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn := &fakeConn{dialect: "sqlite", rows: []any{`{"id":1}`}}
	if _, err := v.WithArgs().Export(ctx, conn, memfs.New()); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
