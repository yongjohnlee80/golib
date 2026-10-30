//go:build integration

// Live cells for the typed vector distance, row locks, composite keys bound as arrays and the
// index lifecycle, against PostgreSQL 18 with pgvector. Each cell makes its own tables, named
// for it, and drops them.
//
//	go test -race -tags integration -run 'TestTypedANN_Live' ./dao/postgres/
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

// annSetup makes table (id bigint, model text, name text, vec vector) with n rows of dims
// random dimensions for model 'm', and n/4 for model 'other', on an admin pool of its own; it
// answers that pool.
func annSetup(t *testing.T, table string, n, dims int) *pgxConn {
	t.Helper()
	admin := openPG(t)
	ctx := context.Background()
	vec := "('[' || array_to_string(ARRAY(SELECT random()::real FROM generate_series(1, " + fmt.Sprint(dims) + ") WHERE g > 0), ',') || ']')::vector"
	for _, q := range []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		`DROP TABLE IF EXISTS ` + table,
		`CREATE TABLE ` + table + ` (id bigint PRIMARY KEY, model text NOT NULL, name text NOT NULL DEFAULT '', vec vector NOT NULL)`,
		`INSERT INTO ` + table + ` SELECT g, 'm', 'n' || g, ` + vec + ` FROM generate_series(1, ` + fmt.Sprint(n) + `) g`,
		`INSERT INTO ` + table + ` SELECT g, 'other', 'o' || g, ` + vec + ` FROM generate_series(` + fmt.Sprint(n+1) + `, ` + fmt.Sprint(n+n/4) + `) g`,
		`ANALYZE ` + table,
	} {
		if _, err := admin.ExecContext(ctx, q); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}
	t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), `DROP TABLE IF EXISTS `+table) })
	return admin
}

type annRow struct {
	ID   int64
	Name string
}

type annField string

const (
	afID   annField = "id"
	afName annField = "name"
)

const (
	bySure annSortKey = "sure"
	byCast annSortKey = "cast"
)

type annSortKey string

func annSchema(conn dao.DataConn, table string, vt dao.VectorType) *dao.Schema[*annRow, annField, annSortKey, int64] {
	return dao.New[*annRow, annField, annSortKey, int64](conn,
		dao.Table[*annRow, annField, annSortKey, int64](table),
		dao.ID[*annRow, annField, annSortKey, int64](afID),
		dao.Fields[*annRow, annField, annSortKey, int64](map[annField]dao.Field[*annRow]{
			afID:   {Column: "id", Scan: func(r *annRow) any { return &r.ID }, Value: func(r *annRow) any { return r.ID }},
			afName: {Column: "name", Scan: func(r *annRow) any { return &r.Name }, Value: func(r *annRow) any { return r.Name }},
		}),
		dao.SortParam[*annRow, annField, annSortKey, int64](bySure, dao.Distance(dao.C("vec"), dao.Cosine)),
		dao.SortParam[*annRow, annField, annSortKey, int64](byCast, dao.DistanceAs(dao.C("vec"), dao.Cosine, vt)),
	)
}

// capture records the SQL and arguments of the statements it sees.
type capture struct {
	dao.NopHook
	sql  []string
	args [][]any
}

func (c *capture) BeforeExec(_ context.Context, q *dao.QueryInfo) error {
	c.sql, c.args = append(c.sql, q.SQL), append(c.args, slices.Clone(q.Args))
	return nil
}

func randVec(dims int) Vector {
	v := make(Vector, dims)
	for i := range v {
		v[i] = rand.Float32()
	}
	return v
}

// explain is the plan of sql with args, with seqscan disabled for the transaction: what decides
// whether the index CAN serve the statement, never which plan a small table's costs prefer.
func explain(t *testing.T, conn *pgxConn, sql string, args []any) string {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, "EXPLAIN (COSTS OFF) "+sql, args...)
	if err != nil {
		t.Fatalf("EXPLAIN %s: %v", sql, err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n")
}

func buildIndex(t *testing.T, conn *pgxConn, s IndexSpec, o BuildOptions) {
	t.Helper()
	p := mustPin(t, conn)
	if err := CreateIndexConcurrently(bg(t), p, s, o); err != nil {
		t.Fatalf("CreateIndexConcurrently %s: %v", s.Name, err)
	}
	p.Discard()
}

func hnswSpec(t *testing.T, table, name string, m dao.Metric, vt dao.VectorType) IndexSpec {
	t.Helper()
	el, err := HNSWIndexElement("vec", m, vt)
	if err != nil {
		t.Fatal(err)
	}
	return IndexSpec{Table: table, Name: name, Method: "hnsw", Element: el, WhereEq: []EqText{{Column: "model", Value: "m"}}}
}

// Cell 1. The typed distance's ORDER BY is the partial HNSW index's expression, so the index can
// serve it; plain Distance's can't (the control). For vector(4) and halfvec(3000).
func TestTypedANN_Live1_TheTypedDistanceUsesTheIndex(t *testing.T) {
	for _, tc := range []struct {
		vt dao.VectorType
		n  int
	}{{f32x4, 400}, {f16x3000, 120}} {
		t.Run(fmt.Sprintf("%s(%d)", vectorTypeName(tc.vt.Kind), tc.vt.Dims), func(t *testing.T) {
			table := "ann_c1_" + vectorTypeName(tc.vt.Kind)
			admin := annSetup(t, table, tc.n, tc.vt.Dims)
			buildIndex(t, admin, hnswSpec(t, table, table+"_hnsw", dao.Cosine, tc.vt), BuildOptions{})
			s := annSchema(admin, table, tc.vt)
			for _, key := range []annSortKey{byCast, bySure} {
				h := &capture{}
				if _, err := s.DAO(dao.WithHooks(h)).WithPredicate(dao.Eq("model", "m")).
					OrderBy(dao.AscBy(key, randVec(tc.vt.Dims))).Limit(10).Select(afID); err != nil {
					t.Fatalf("%s: %v", key, err)
				}
				plan := explain(t, admin, h.sql[0], h.args[0])
				uses := strings.Contains(plan, table+"_hnsw")
				if key == byCast && !uses {
					t.Errorf("DistanceAs: the plan doesn't use the index:\n%s", plan)
				}
				if key == bySure && uses {
					t.Errorf("Distance (the control) uses the index, so the cell can't tell:\n%s", plan)
				}
			}
		})
	}
}

// Cell 2. With no index, DistanceAs answers the exact order: for vector, the same as Distance's;
// for halfvec, an order whose full-precision distances never fall by more than half precision's
// rounding.
func TestTypedANN_Live2_TheAnswerIsTheExactOne(t *testing.T) {
	admin := annSetup(t, "ann_c2", 300, 4)
	s := annSchema(admin, "ann_c2", f32x4)
	q := randVec(4)
	sure, cast := orderOf(t, s, bySure, q), orderOf(t, s, byCast, q)
	if !slices.Equal(sure, cast) {
		t.Errorf("vector(4): DistanceAs's order %v differs from Distance's %v", cast[:10], sure[:10])
	}

	admin16 := annSetup(t, "ann_c2h", 60, 3000)
	s16 := annSchema(admin16, "ann_c2h", f16x3000)
	q16 := randVec(3000)
	order := orderOf(t, s16, byCast, q16)
	vecs := readVecs(t, admin16, "ann_c2h")
	prev := -1.0
	for _, id := range order {
		d := cosineDist(vecs[id], q16)
		if d < prev-1e-3 {
			t.Fatalf("halfvec(3000): row %d's exact distance %.6f falls below the previous %.6f by more than rounding", id, d, prev)
		}
		prev = math.Max(prev, d)
	}
}

func orderOf(t *testing.T, s *dao.Schema[*annRow, annField, annSortKey, int64], key annSortKey, q Vector) []int64 {
	t.Helper()
	rows, err := s.DAO().WithPredicate(dao.Eq("model", "m")).OrderBy(dao.AscBy(key, q)).Limit(50).Select(afID)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func readVecs(t *testing.T, conn *pgxConn, table string) map[int64]Vector {
	t.Helper()
	rows, err := conn.pool.Query(context.Background(), "SELECT id, vec::text FROM "+table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int64]Vector{}
	for rows.Next() {
		var id int64
		var v Vector
		if err := rows.Scan(&id, &v); err != nil {
			t.Fatal(err)
		}
		out[id] = v
	}
	return out
}

func cosineDist(a, b Vector) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return 1 - dot/math.Sqrt(na*nb)
}

// leaseRow and its schema are cell 4's and 14's: a lease table, optionally schema-qualified,
// joined to a note table.
type leaseRow struct {
	ID   int64
	Note string
}

func leaseSchema(conn dao.DataConn, table string) *dao.Schema[*leaseRow, string, string, int64] {
	return dao.New[*leaseRow, string, string, int64](conn,
		dao.Table[*leaseRow, string, string, int64](table),
		dao.ID[*leaseRow, string, string, int64]("id"),
		dao.Fields[*leaseRow, string, string, int64](map[string]dao.Field[*leaseRow]{
			"id":   {Column: table + ".id", Scan: func(r *leaseRow) any { return &r.ID }},
			"note": {Column: "ann_note.note", Join: "note", ReadOnly: true, Scan: func(r *leaseRow) any { return &r.Note }},
		}),
		dao.OptionalJoin[*leaseRow, string, string, int64]("note", "JOIN ann_note ON ann_note.id = "+table+".id"),
	)
}

// waitsOnALock reports whether a backend waits on a row lock the cell's other transaction holds,
// read from pg_locks: the observable, never a sleep.
func waitsOnALock(t *testing.T, conn *pgxConn) bool {
	t.Helper()
	var n int
	if err := conn.pool.QueryRow(context.Background(), "SELECT count(*) FROM pg_locks WHERE NOT granted AND locktype = 'transactionid'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waited 10 s for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Cells 4 and 14. A locked Get blocks a second locked Get of the row until the first commits; a
// plain read doesn't block; a joined table's row isn't locked. On a plain and a
// schema-qualified table.
func TestTypedANN_Live4_RowLocksHold(t *testing.T) {
	for _, table := range []string{"ann_lease", "ann_app.ann_lease"} {
		t.Run(table, func(t *testing.T) {
			admin := openPG(t)
			ctx := context.Background()
			for _, q := range []string{
				`CREATE SCHEMA IF NOT EXISTS ann_app`,
				`DROP TABLE IF EXISTS ` + table + `, ann_note`,
				`CREATE TABLE ` + table + ` (id bigint PRIMARY KEY)`,
				`CREATE TABLE ann_note (id bigint PRIMARY KEY, note text NOT NULL)`,
				`INSERT INTO ` + table + ` VALUES (1)`,
				`INSERT INTO ann_note VALUES (1, 'n')`,
			} {
				if _, err := admin.ExecContext(ctx, q); err != nil {
					t.Fatalf("%s: %v", q, err)
				}
			}
			t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), `DROP TABLE IF EXISTS `+table+`, ann_note`) })
			s := leaseSchema(admin, table)
			tx1 := dao.Begin(ctx)
			defer func() { _ = tx1.Rollback() }()
			if _, err := s.On(tx1, dao.WithRowLock(dao.LockForUpdate)).With("id", 1).Get("id", "note"); err != nil {
				t.Fatalf("tx1's locked Get: %v", err)
			}
			// a plain read doesn't block
			if _, err := s.DAO().With("id", 1).Get("id"); err != nil {
				t.Fatalf("a plain read: %v", err)
			}
			// the joined row isn't locked: NOWAIT takes it at once
			if _, err := admin.pool.Exec(ctx, "BEGIN; SELECT 1 FROM ann_note WHERE id = 1 FOR UPDATE NOWAIT; ROLLBACK"); err != nil {
				t.Fatalf("the joined row was locked: %v", err)
			}
			got := make(chan error, 1)
			go func() {
				tx2 := dao.Begin(ctx)
				defer func() { _ = tx2.Rollback() }()
				_, err := s.On(tx2, dao.WithRowLock(dao.LockForUpdate)).With("id", 1).Get("id")
				got <- err
			}()
			until(t, "tx2 to wait on tx1's lock", func() bool { return waitsOnALock(t, admin) })
			select {
			case err := <-got:
				t.Fatalf("tx2's locked Get returned while tx1 held the lock: %v", err)
			default:
			}
			if err := tx1.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := <-got; err != nil {
				t.Fatalf("tx2 after tx1's commit: %v", err)
			}
		})
	}
}

// Cell 5, live: a pool DAO with a row lock sends nothing and answers ErrRowLockNeedsTx.
func TestTypedANN_Live5_ARowLockOnThePoolSendsNothing(t *testing.T) {
	admin := openPG(t)
	h := &capture{}
	_, err := leaseSchema(admin, "ann_none").DAO(dao.WithHooks(h), dao.WithRowLock(dao.LockForUpdate)).Select("id")
	if !errors.Is(err, dao.ErrRowLockNeedsTx) || len(h.sql) != 0 {
		t.Fatalf("%v, sent %q", err, h.sql)
	}
}

// Cells 6 and 7. A concurrent build is valid, with the reference's definition; a build of the
// same name with another operator class isn't the reference's; the drop removes it, and a
// second drop is no error; with a session transaction open, both are refused first.
func TestTypedANN_Live6_TheConcurrentBuild(t *testing.T) {
	admin := annSetup(t, "ann_c6", 200, 4)
	spec := hnswSpec(t, "ann_c6", "ann_c6_hnsw", dao.Cosine, f32x4)
	buildIndex(t, admin, spec, BuildOptions{})
	st, err := ReadIndexState(bg(t), admin, "", "ann_c6_hnsw")
	if err != nil || !st.Exists || !st.Valid {
		t.Fatalf("after the build: %+v %v", st, err)
	}
	ref, err := ReferenceDefinition(bg(t), mustPin(t, admin), spec)
	if err != nil {
		t.Fatal(err)
	}
	if st.Def != ref {
		t.Fatalf("the built index's definition %+v differs from the reference %+v", st.Def, ref)
	}
	if ref.Method != "hnsw" || ref.OpClass != "vector_cosine_ops" || !strings.Contains(ref.Predicate, "'m'") {
		t.Errorf("the reference: %+v", ref)
	}
	p := mustPin(t, admin)
	if err := DropIndexConcurrently(bg(t), p, "", "ann_c6_hnsw", BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := DropIndexConcurrently(bg(t), p, "", "ann_c6_hnsw", BuildOptions{}); err != nil {
		t.Fatalf("a second drop: %v", err)
	}
	if st, _ := ReadIndexState(bg(t), admin, "", "ann_c6_hnsw"); st.Exists {
		t.Fatal("the index is still there after the drop")
	}

	// cell 7: the same name with another operator class
	buildIndex(t, admin, hnswSpec(t, "ann_c6", "ann_c6_hnsw", dao.L2, f32x4), BuildOptions{})
	st, err = ReadIndexState(bg(t), admin, "", "ann_c6_hnsw")
	if err != nil || st.Def == ref {
		t.Fatalf("an L2 index reads as the cosine reference: %+v %v", st, err)
	}

	pt := mustPin(t, admin)
	tx, err := pt.BeginSessionTx(bg(t), dao.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := CreateIndexConcurrently(bg(t), pt, spec, BuildOptions{}); !errors.Is(err, errs.ErrPrecondition) {
		t.Errorf("a build with a transaction open: %v, want ErrPrecondition", err)
	}
	if err := DropIndexConcurrently(bg(t), pt, "", "ann_c6_hnsw", BuildOptions{}); !errors.Is(err, errs.ErrPrecondition) {
		t.Errorf("a drop with a transaction open: %v, want ErrPrecondition", err)
	}
}

// Cell 8. A build cancelled midway (CancelBackend from another connection, the build's session
// marked) leaves an index that exists and is invalid; the drop removes it.
func TestTypedANN_Live8_ACancelledBuildLeavesAnInvalidIndex(t *testing.T) {
	admin := annSetup(t, "ann_c8", 150000, 8)
	spec := hnswSpec(t, "ann_c8", "ann_c8_hnsw", dao.Cosine, dao.VectorType{Kind: dao.VectorFloat32, Dims: 8})
	p := mustPin(t, admin)
	b, err := MarkSession(bg(t), p, "c8:1")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- CreateIndexConcurrently(context.Background(), p, spec, BuildOptions{}) }()
	until(t, "the build to run", func() bool {
		var n int
		_ = admin.pool.QueryRow(context.Background(), "SELECT count(*) FROM pg_stat_activity WHERE pid = $1 AND query LIKE 'CREATE INDEX%'", b.PID).Scan(&n)
		return n > 0
	})
	if ok, err := CancelBackend(context.Background(), admin, b); err != nil || !ok {
		t.Fatalf("CancelBackend: %v %v", ok, err)
	}
	if err := <-done; err == nil {
		t.Fatal("the cancelled build succeeded; the table is too small for the cell")
	}
	st, err := ReadIndexState(context.Background(), admin, "", "ann_c8_hnsw")
	if err != nil || !st.Exists || st.Valid {
		t.Fatalf("after the cancel: %+v %v; want an invalid index", st, err)
	}
	if err := DropIndexConcurrently(context.Background(), mustPin(t, admin), "", "ann_c8_hnsw", BuildOptions{}); err != nil {
		t.Fatal(err)
	}
}

// Cell 9. MarkSession's backend is the session's; it is alive while the session lives and gone
// after Discard; the wrong start, or an unmarked session, is never alive and never signalled.
func TestTypedANN_Live9_TheBackend(t *testing.T) {
	admin := openPG(t)
	p := mustPin(t, admin)
	b, err := MarkSession(bg(t), p, "c9:1")
	if err != nil {
		t.Fatal(err)
	}
	if alive, err := BackendAlive(bg(t), admin, b); err != nil || !alive {
		t.Fatalf("a live marked session: %v %v", alive, err)
	}
	wrong := b
	wrong.Start = b.Start.Add(time.Microsecond)
	if alive, _ := BackendAlive(bg(t), admin, wrong); alive {
		t.Error("another backend_start is alive")
	}
	if sent, _ := CancelBackend(bg(t), admin, wrong); sent {
		t.Error("another backend_start was signalled")
	}
	// an unmarked session: its own pid and start, read directly
	other, err := admin.pool.Acquire(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Release()
	var ob Backend
	if err := other.QueryRow(bg(t), "SELECT pid, backend_start FROM pg_stat_activity WHERE pid = pg_backend_pid()").Scan(&ob.PID, &ob.Start); err != nil {
		t.Fatal(err)
	}
	ob.Token = "c9:1"
	if alive, _ := BackendAlive(bg(t), admin, ob); alive {
		t.Error("an unmarked session is alive to BackendAlive")
	}
	if sent, _ := TerminateBackend(bg(t), admin, ob); sent {
		t.Error("an unmarked session was signalled")
	}
	if _, err := other.Exec(bg(t), "SELECT 1"); err != nil {
		t.Fatalf("the unmarked session was disturbed: %v", err)
	}
	p.Discard()
	until(t, "the destroyed session's backend to exit", func() bool {
		alive, err := BackendAlive(context.Background(), admin, b)
		return err == nil && !alive
	})
}

// Cell 10. The reference build leaves no table behind, and its session is destroyed at Discard,
// not recycled.
func TestTypedANN_Live10_TheReferenceBuildLeavesNothing(t *testing.T) {
	admin := annSetup(t, "ann_c10", 50, 4)
	p := mustPin(t, admin)
	b, err := MarkSession(bg(t), p, "c10:1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReferenceDefinition(bg(t), p, hnswSpec(t, "ann_c10", "ann_c10_hnsw", dao.Cosine, f32x4)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := admin.pool.QueryRow(bg(t), "SELECT count(*) FROM pg_class WHERE relname LIKE 'golib_ref_%'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("reference tables left: %d %v", n, err)
	}
	p.Discard()
	until(t, "the reference session's backend to exit (destroyed, not recycled)", func() bool {
		var n int
		_ = admin.pool.QueryRow(context.Background(), "SELECT count(*) FROM pg_stat_activity WHERE pid = $1", b.PID).Scan(&n)
		return n == 0
	})
}

// Cell 11. A predicate value holding \, ' and \' builds a partial index whose predicate is the
// value, under standard_conforming_strings on and off: a bound WHERE model = $1 can use it.
func TestTypedANN_Live11_ThePredicateLiteral(t *testing.T) {
	const value = `a\b'c\'`
	for _, scs := range []string{"on", "off"} {
		t.Run("standard_conforming_strings="+scs, func(t *testing.T) {
			admin := annSetup(t, "ann_c11", 20, 4)
			if _, err := admin.ExecContext(bg(t), "UPDATE ann_c11 SET model = $1 WHERE id <= 5", value); err != nil {
				t.Fatal(err)
			}
			el, err := ColumnElement("id")
			if err != nil {
				t.Fatal(err)
			}
			spec := IndexSpec{Table: "ann_c11", Name: "ann_c11_part", Method: "btree", Element: el, WhereEq: []EqText{{Column: "model", Value: value}}}
			p := mustPin(t, admin)
			if _, err := p.SimpleQuery(bg(t), "SET standard_conforming_strings = "+scs, func(ExtendedMessage) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if err := CreateIndexConcurrently(bg(t), p, spec, BuildOptions{}); err != nil {
				t.Fatalf("the build: %v", err)
			}
			plan := explain(t, admin, "SELECT id FROM ann_c11 WHERE model = $1 AND id > 0", []any{value})
			if !strings.Contains(plan, "ann_c11_part") {
				t.Fatalf("the partial index's predicate is not the value: a bound match can't use it:\n%s", plan)
			}
		})
	}
	for _, bad := range []string{"a\x00b", "a\nb", "a\x7fb"} {
		el, _ := ColumnElement("id")
		s := IndexSpec{Table: "ann_c11", Name: "ann_c11_bad", Method: "btree", Element: el, WhereEq: []EqText{{Column: "model", Value: bad}}}
		if err := CreateIndexConcurrently(bg(t), mustPin(t, openPG(t)), s, BuildOptions{}); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("%q: %v, want ErrInvalidArgument", bad, err)
		}
	}
}

// Cell 12. KeysIn over 500 keys sends two parameters and answers exactly the keyed rows.
func TestTypedANN_Live12_KeysIn(t *testing.T) {
	admin := annSetup(t, "ann_c12", 2000, 4)
	keys := make([][]any, 0, 500)
	want := map[int64]bool{}
	for i := int64(1); i <= 1000; i += 2 {
		keys = append(keys, []any{i, "n" + fmt.Sprint(i)})
		want[i] = true
	}
	keys = append(keys, []any{int64(2), "not-its-name"}) // a key whose second column doesn't match
	h := &capture{}
	rows, err := annSchema(admin, "ann_c12", f32x4).DAO(dao.WithHooks(h)).
		WithPredicate(dao.KeysIn([]dao.KeyColumn{{Col: "id", Elem: dao.ArrayInt64}, {Col: "name", Elem: dao.ArrayText}}, keys)).Select(afID)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.args[0]) != 2 {
		t.Errorf("%d parameters; want 2", len(h.args[0]))
	}
	got := map[int64]bool{}
	for _, r := range rows {
		got[r.ID] = true
	}
	if len(got) != len(want) {
		t.Fatalf("%d rows; want %d", len(got), len(want))
	}
	for id := range want {
		if !got[id] {
			t.Fatalf("row %d is missing", id)
		}
	}
}

// Cell 13. The build's timeouts are set explicitly: under a pool whose statement_timeout is
// 20 ms, a build with none completes and is valid; with 20 ms of its own, it's cut short and the
// index is invalid.
func TestTypedANN_Live13_ExplicitTimeouts(t *testing.T) {
	admin := annSetup(t, "ann_c13", 40000, 8)
	url := pgURL(t)
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	short, err := Open(context.Background(), url+sep+"options=-c%20statement_timeout%3D20")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = short.Close() })
	spec := hnswSpec(t, "ann_c13", "ann_c13_hnsw", dao.Cosine, dao.VectorType{Kind: dao.VectorFloat32, Dims: 8})
	if err := CreateIndexConcurrently(bg(t), mustPin(t, short), spec, BuildOptions{}); err != nil {
		t.Fatalf("a build with no timeout, under a 20 ms pool: %v", err)
	}
	if st, err := ReadIndexState(bg(t), admin, "", "ann_c13_hnsw"); err != nil || !st.Valid {
		t.Fatalf("after the untimed build: %+v %v", st, err)
	}
	if err := DropIndexConcurrently(bg(t), mustPin(t, admin), "", "ann_c13_hnsw", BuildOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := CreateIndexConcurrently(bg(t), mustPin(t, admin), spec, BuildOptions{StatementTimeout: 20 * time.Millisecond}); err == nil {
		t.Fatal("a build with a 20 ms timeout finished; the table is too small for the cell")
	}
	if st, err := ReadIndexState(bg(t), admin, "", "ann_c13_hnsw"); err != nil || !st.Exists || st.Valid {
		t.Fatalf("after the timed-out build: %+v %v; want an invalid index", st, err)
	}
}
