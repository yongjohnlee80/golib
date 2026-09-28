// Package daotest holds engine-independent checks that each driver's tests run
// against its real engine, so a capability is proven on every engine by one
// body rather than a copy per driver.
package daotest

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/dao"
)

// Engine is what QueryCapabilities needs to know about the engine under test.
type Engine struct {
	// Table is the DDL creating the test table under the given name, with
	// columns id (a generated key), uri (unique), name (nullable), n and cap
	// (both integers, NOT NULL).
	Table func(name string) string
	// Writers says the engine takes concurrent writers, so Incr is also
	// checked under contention.
	Writers bool
}

type row struct {
	ID     int64
	URI    string
	Name   sql.NullString
	N, Cap int64
}

type field string

const (
	fID   field = "id"
	fURI  field = "uri"
	fName field = "name"
	fN    field = "n"
	fCap  field = "cap"
)

type sortKey string

const byURI sortKey = "uri"

// QueryCapabilities checks Cmp, Incr, UpsertOnly, SelectDistinct and
// CountDistinct against conn's engine.
func QueryCapabilities(t *testing.T, conn dao.DataConn, e Engine) {
	t.Helper()
	ctx := context.Background()
	table := fmt.Sprintf("golib_qcap_%d", time.Now().UnixNano())
	if _, err := conn.ExecContext(ctx, e.Table(table)); err != nil {
		t.Fatalf("create %s: %v", table, err)
	}
	t.Cleanup(func() { _, _ = conn.ExecContext(context.Background(), "DROP TABLE "+table) })
	s := dao.New[*row, field, sortKey, int64](conn,
		dao.Table[*row, field, sortKey, int64](table),
		dao.ID[*row, field, sortKey, int64](fID),
		dao.Fields[*row, field, sortKey, int64](map[field]dao.Field[*row]{
			fID:   {Expr: dao.C(fID), Scan: func(r *row) any { return &r.ID }},
			fURI:  {Expr: dao.C(fURI), Scan: func(r *row) any { return &r.URI }},
			fName: {Expr: dao.C(fName), Scan: func(r *row) any { return &r.Name }},
			fN:    {Expr: dao.C(fN), Scan: func(r *row) any { return &r.N }},
			fCap:  {Expr: dao.C(fCap), Scan: func(r *row) any { return &r.Cap }},
		}),
		dao.Default[*row, field, sortKey, int64](fID, fURI, fName, fN, fCap),
		dao.SortMap[*row, field, sortKey, int64](map[sortKey]string{byURI: "uri"}),
		dao.Conflict[*row, field, sortKey, int64](fURI))
	for _, r := range []struct {
		uri  string
		name any
		n, c int64
	}{{"u1", "a", 1, 5}, {"u2", "a", 7, 5}, {"u3", "b", 3, 3}, {"u4", nil, 9, 1}} {
		if _, err := s.OnCtx(ctx).Set(fURI, r.uri).Set(fName, r.name).Set(fN, r.n).Set(fCap, r.c).Insert(); err != nil {
			t.Fatalf("seed %s: %v", r.uri, err)
		}
	}
	get := func(uri string) *row {
		t.Helper()
		r, err := s.OnCtx(ctx).With(fURI, uri).Get()
		if err != nil {
			t.Fatalf("get %s: %v", uri, err)
		}
		return r
	}

	// Cmp: the rows whose n is at most their cap
	rows, err := s.OnCtx(ctx).WithPredicate(dao.Cmp(dao.C(fN), dao.OpLte, dao.C(fCap))).OrderBy(dao.Asc(byURI)).Select(fURI)
	if err != nil {
		t.Fatalf("Cmp: %v", err)
	}
	if got := uris(rows); got != "[u1 u3]" {
		t.Errorf("Cmp(n <= cap) = %s, want [u1 u3]", got)
	}

	// SelectDistinct and CountDistinct: NULL is no value
	names, err := dao.SelectDistinct(s.OnCtx(ctx).WithPredicate(dao.IsNotNull("name")), fName)
	if err != nil {
		t.Fatalf("SelectDistinct: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("SelectDistinct(name) = %d rows, want 2 (a, b)", len(names))
	}
	if n, err := dao.CountDistinct(s.OnCtx(ctx), fName); err != nil || n != 2 {
		t.Errorf("CountDistinct(name) = %d, %v; want 2, NULL not counted", n, err)
	}

	// Incr: the engine adds, and under contention every increment lands
	if err := s.OnCtx(ctx).With(fURI, "u1").Set(fN, dao.Incr(10)).Update(); err != nil {
		t.Fatalf("Incr: %v", err)
	}
	if got := get("u1").N; got != 11 {
		t.Errorf("after Incr(10), n = %d, want 11", got)
	}
	if e.Writers {
		const k = 20
		var wg sync.WaitGroup
		errc := make(chan error, k)
		for range k {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errc <- s.OnCtx(ctx).With(fURI, "u3").Set(fN, dao.Incr(1)).Update()
			}()
		}
		wg.Wait()
		close(errc)
		for err := range errc {
			if err != nil {
				t.Fatalf("a concurrent Incr: %v", err)
			}
		}
		if got := get("u3").N; got != 3+k {
			t.Errorf("after %d concurrent Incr(1), n = %d, want %d", k, got, 3+k)
		}
	}

	// UpsertOnly: a conflict updates the named field and keeps the rest
	if err := dao.UpsertOnly(s.OnCtx(ctx).Set(fURI, "u2").Set(fName, "z").Set(fN, 100).Set(fCap, 100), fName); err != nil {
		t.Fatalf("UpsertOnly(name): %v", err)
	}
	if r := get("u2"); r.Name.String != "z" || r.N != 7 || r.Cap != 5 {
		t.Errorf("after UpsertOnly(name) on u2: %+v, want name z, n 7, cap 5", r)
	}
	if err := dao.UpsertOnly(s.OnCtx(ctx).Set(fURI, "u2").Set(fName, "q").Set(fN, 1).Set(fCap, 1)); err != nil {
		t.Fatalf("UpsertOnly(): %v", err)
	}
	if r := get("u2"); r.Name.String != "z" || r.N != 7 {
		t.Errorf("after UpsertOnly() on u2: %+v, want the row kept (name z, n 7)", r)
	}
	if err := dao.UpsertOnly(s.OnCtx(ctx).Set(fURI, "u5").Set(fName, "n").Set(fN, 4).Set(fCap, 4), fName); err != nil {
		t.Fatalf("UpsertOnly on a new row: %v", err)
	}
	if r := get("u5"); r.N != 4 || r.Cap != 4 {
		t.Errorf("a new row through UpsertOnly: %+v, want every staged value inserted", r)
	}
}

func uris(rs []*row) string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.URI
	}
	return fmt.Sprint(out)
}
