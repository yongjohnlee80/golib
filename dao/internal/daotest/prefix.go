package daotest

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/dao"
)

type pathRow struct {
	ID   int64
	Path string
}

type pathField string

const (
	pID   pathField = "id"
	pPath pathField = "path"
)

// prefixPaths are paths a wildcard or a mishandled escape would confuse: each
// prefix below must find only the paths that literally start with it.
var prefixPaths = []string{
	"a%b/x", "a%b/y", "aXb/z", "a%c/q",
	"a_b/x", "aZb/x",
	`a\b/x`, `a\\b/x`, "ab/x",
	"a!b/x", "a!!b/x", "a!%b/x",
	"plain/x", "plain/y", "plainer/x",
}

// LiteralPrefix checks dao.HasPrefix on conn's engine: a prefix holding %, _,
// a backslash or ! matches only paths that start with exactly that text.
// table is the DDL creating a table under the given name with columns id (an
// integer key) and path (text).
func LiteralPrefix(t *testing.T, conn dao.DataConn, table func(name string) string) {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("golib_prefix_%d", time.Now().UnixNano())
	if _, err := conn.ExecContext(ctx, table(name)); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(func() { _, _ = conn.ExecContext(context.Background(), "DROP TABLE "+name) })
	s := dao.New[*pathRow, pathField, string, int64](conn,
		dao.Table[*pathRow, pathField, string, int64](name),
		dao.ID[*pathRow, pathField, string, int64](pID),
		dao.Fields[*pathRow, pathField, string, int64](map[pathField]dao.Field[*pathRow]{
			pID:   {Expr: dao.C(pID), Scan: func(r *pathRow) any { return &r.ID }, Value: func(r *pathRow) any { return r.ID }},
			pPath: {Expr: dao.C(pPath), Scan: func(r *pathRow) any { return &r.Path }, Value: func(r *pathRow) any { return r.Path }},
		}),
	)
	for i, p := range prefixPaths {
		if _, err := s.DAO().Set(pID, int64(i+1)).Set(pPath, p).Insert(); err != nil {
			t.Fatalf("insert %q: %v", p, err)
		}
	}
	col := conn.Dialect().QuoteIdent(string(pPath))
	for _, prefix := range []string{"a%b/", "a_b/", `a\b/`, "a!b/", "a!!b/", "a!%b/", "plain/", ""} {
		got, err := s.DAO().WithPredicate(dao.HasPrefix(col, prefix)).Select(pPath)
		if err != nil {
			t.Fatalf("HasPrefix(%q): %v", prefix, err)
		}
		var paths, want []string
		for _, r := range got {
			paths = append(paths, r.Path)
		}
		for _, p := range prefixPaths {
			if len(p) >= len(prefix) && p[:len(prefix)] == prefix {
				want = append(want, p)
			}
		}
		sort.Strings(paths)
		sort.Strings(want)
		if !reflect.DeepEqual(paths, want) {
			t.Errorf("HasPrefix(%q) = %q, want %q", prefix, paths, want)
		}
	}
}
