package postgres

// Executed cells for the search pieces on a real PostgreSQL with pgvector:
// bound vectors through both paths that carry arguments (the pool and a
// transaction), the three metrics' orders, and full-text matching and ranking
// with weight classes and text-search configurations. They run wherever
// TEST_PGURL points (golib-test.sh --target vm43 points it at a scratch
// database on autodb-r3-pg, PostgreSQL 18 with pgvector) and skip without it.
// Each test owns a schema of its own and drops it.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"math"
	"reflect"
	"sort"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/search/query"
)

// searchSchema makes a fresh schema with pgvector available, and drops it
// when the test ends.
func searchSchema(t *testing.T) (dao.DataConn, string) {
	t.Helper()
	conn := testConn(t) // skips when TEST_PGURL is unset
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		t.Fatalf("pgvector is not available on this server: %v", err)
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	schema := "golib_search_" + hex.EncodeToString(b)
	if _, err := conn.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	return conn, schema
}

func mustExec(t *testing.T, conn dao.DataConn, q string) {
	t.Helper()
	if _, err := conn.ExecContext(context.Background(), q); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// --- vectors ------------------------------------------------------------------

type vrow struct {
	ID    int64
	Label string
	Emb   Vector
}

type vfield string

const (
	vID    vfield = "id"
	vLabel vfield = "label"
	vEmb   vfield = "emb"
)

type vsort string

const (
	vByCosine vsort = "cosine"
	vByL2     vsort = "l2"
	vByIP     vsort = "ip"
)

func vectorSchema(conn dao.DataConn, table string) *dao.Schema[*vrow, vfield, vsort, int64] {
	emb := dao.T(table, vEmb)
	return dao.New(conn,
		dao.Table[*vrow, vfield, vsort, int64](table),
		dao.ID[*vrow, vfield, vsort, int64](vID),
		dao.Fields[*vrow, vfield, vsort, int64](map[vfield]dao.Field[*vrow]{
			vID:    {Expr: dao.T(table, vID), Scan: func(r *vrow) any { return &r.ID }, Value: func(r *vrow) any { return r.ID }},
			vLabel: {Expr: dao.T(table, vLabel), Scan: func(r *vrow) any { return &r.Label }, Value: func(r *vrow) any { return r.Label }},
			vEmb:   {Expr: emb, Scan: func(r *vrow) any { return &r.Emb }, Value: func(r *vrow) any { return r.Emb }},
		}),
		dao.SortParam[*vrow, vfield, vsort, int64](vByCosine, dao.Distance(emb, dao.Cosine)),
		dao.SortParam[*vrow, vfield, vsort, int64](vByL2, dao.Distance(emb, dao.L2)),
		dao.SortParam[*vrow, vfield, vsort, int64](vByIP, dao.Distance(emb, dao.InnerProduct)),
	)
}

// vectorFixture are rows whose orders under each metric differ, so a swapped
// operator orders them wrongly.
var vectorFixture = []vrow{
	{1, "east", Vector{1, 0, 0}},
	{2, "far-east", Vector{10, 0.5, 0}},
	{3, "north", Vector{0, 1, 0}},
	{4, "near-origin", Vector{0.1, 0.1, 0}},
	{5, "diagonal", Vector{0.7, 0.7, 0.1}},
}

// expectedOrder computes, in Go, the ids nearest first to q under m.
func expectedOrder(q Vector, m dao.Metric) []int64 {
	dist := func(v Vector) float64 {
		var dot, nq, nv, l2 float64
		for i := range v {
			dot += float64(v[i]) * float64(q[i])
			nq += float64(q[i]) * float64(q[i])
			nv += float64(v[i]) * float64(v[i])
			d := float64(v[i]) - float64(q[i])
			l2 += d * d
		}
		switch m {
		case dao.L2:
			return math.Sqrt(l2)
		case dao.InnerProduct:
			return -dot
		}
		return 1 - dot/math.Sqrt(nq*nv)
	}
	rows := append([]vrow(nil), vectorFixture...)
	sort.SliceStable(rows, func(i, j int) bool { return dist(rows[i].Emb) < dist(rows[j].Emb) })
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func ids(rs []*vrow) []int64 {
	out := make([]int64, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}

// The probe the vector type ships on: a bound Vector written, read back and
// ordered by through a DAO, on the pool and inside a transaction.
func TestVectorPG_BoundOnThePoolAndInATransaction(t *testing.T) {
	conn, schema := searchSchema(t)
	table := schema + ".vec_item"
	mustExec(t, conn, "CREATE TABLE "+table+" (id bigint PRIMARY KEY, label text NOT NULL, emb vector(3))")
	s := vectorSchema(conn, table)
	ctx := context.Background()

	// Pool path.
	for _, r := range vectorFixture[:3] {
		if _, err := s.DAO().Set(vID, r.ID).Set(vLabel, r.Label).Set(vEmb, r.Emb).Insert(); err != nil {
			t.Fatalf("insert on the pool: %v", err)
		}
	}
	// Transaction path.
	err := dao.RunTx(ctx, func(tx *dao.Transaction) error {
		for _, r := range vectorFixture[3:] {
			if _, err := s.On(tx).Set(vID, r.ID).Set(vLabel, r.Label).Set(vEmb, r.Emb).Insert(); err != nil {
				return err
			}
		}
		got, err := s.On(tx).OrderBy(dao.AscBy(vByL2, Vector{0, 0, 0})).Limit(1).Select(vID, vEmb)
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].ID != 4 || !reflect.DeepEqual(got[0].Emb, Vector{0.1, 0.1, 0}) {
			t.Errorf("in the transaction: nearest the origin %+v, want id 4 with its vector", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("transaction: %v", err)
	}

	// Read back on the pool, exactly as written.
	got, err := s.DAO().OrderBy(dao.AscBy(vByL2, Vector{1, 0, 0})).Select(vID, vLabel, vEmb)
	if err != nil {
		t.Fatalf("select on the pool: %v", err)
	}
	byID := map[int64]Vector{}
	for _, r := range got {
		byID[r.ID] = r.Emb
	}
	for _, r := range vectorFixture {
		if !reflect.DeepEqual(byID[r.ID], r.Emb) {
			t.Errorf("id %d read back %v, wrote %v", r.ID, byID[r.ID], r.Emb)
		}
	}
	if got[0].ID != 1 {
		t.Errorf("nearest [1 0 0] by L2 is %d, want 1", got[0].ID)
	}
}

func TestVectorPG_EachMetricOrdersAsComputed(t *testing.T) {
	conn, schema := searchSchema(t)
	table := schema + ".vec_item"
	mustExec(t, conn, "CREATE TABLE "+table+" (id bigint PRIMARY KEY, label text NOT NULL, emb vector(3))")
	s := vectorSchema(conn, table)
	for _, r := range vectorFixture {
		if _, err := s.DAO().Set(vID, r.ID).Set(vLabel, r.Label).Set(vEmb, r.Emb).Insert(); err != nil {
			t.Fatal(err)
		}
	}
	q := Vector{1, 0.2, 0}
	for key, m := range map[vsort]dao.Metric{vByCosine: dao.Cosine, vByL2: dao.L2, vByIP: dao.InnerProduct} {
		got, err := s.DAO().OrderBy(dao.AscBy(key, q)).Select(vID)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if want := expectedOrder(q, m); !reflect.DeepEqual(ids(got), want) {
			t.Errorf("%s order %v, want %v", key, ids(got), want)
		}
	}
	// The three orders are not all the same, or this cell could not tell a
	// swapped operator.
	if reflect.DeepEqual(expectedOrder(q, dao.Cosine), expectedOrder(q, dao.L2)) {
		t.Fatal("the fixture orders cosine and L2 alike; it cannot catch a swap")
	}
}

// --- full text ----------------------------------------------------------------

type drow struct {
	ID int64
	WS string
}

type dfield string

const (
	dID dfield = "id"
	dWS dfield = "ws"
)

type dsort string

const dByRank dsort = "rank"

// docTable has the weighted tsvector the consumer's migration would declare:
// the breadcrumb at class A, the body at class D, under "simple", and an
// English one for the configuration cell.
func docTable(t *testing.T, conn dao.DataConn, table string) {
	mustExec(t, conn, "CREATE TABLE "+table+` (
		id bigint PRIMARY KEY,
		ws text NOT NULL,
		crumb text NOT NULL,
		body text NOT NULL,
		tsv tsvector GENERATED ALWAYS AS (
			setweight(to_tsvector('simple', crumb), 'A') || setweight(to_tsvector('simple', body), 'D')) STORED,
		tsv_en tsvector GENERATED ALWAYS AS (to_tsvector('english', body)) STORED)`)
}

func docSchema(conn dao.DataConn, table string, rank dao.ParamExpr) *dao.Schema[*drow, dfield, dsort, int64] {
	return dao.New(conn,
		dao.Table[*drow, dfield, dsort, int64](table),
		dao.ID[*drow, dfield, dsort, int64](dID),
		dao.Fields[*drow, dfield, dsort, int64](map[dfield]dao.Field[*drow]{
			dID: {Expr: dao.T(table, dID), Scan: func(r *drow) any { return &r.ID }, Value: func(r *drow) any { return r.ID }},
			dWS: {Expr: dao.T(table, dWS), Scan: func(r *drow) any { return &r.WS }, Value: func(r *drow) any { return r.WS }},
		}),
		dao.SortParam[*drow, dfield, dsort, int64](dByRank, rank),
	)
}

func docIDs(rs []*drow) []int64 {
	out := make([]int64, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}

func TestFullTextPG_MatchAndRankByClass(t *testing.T) {
	conn, schema := searchSchema(t)
	table := schema + ".doc"
	docTable(t, conn, table)
	mustExec(t, conn, "INSERT INTO "+table+` (id, ws, crumb, body) VALUES
		(1, 'a', 'alpha guide', 'nothing here'),
		(2, 'a', 'misc', 'the alpha appears in the body'),
		(3, 'b', 'alpha elsewhere', 'alpha alpha'),
		(4, 'a', 'beta', 'no match')`)
	ix := dao.FullTextIndex{Name: "tsv", Table: table, Columns: []string{"crumb", "body"}, Classes: []byte("AD")}

	// Match filters, the other predicate is in the same WHERE, before the LIMIT.
	plain := docSchema(conn, table, dao.RankQuery(ix))
	got, err := plain.DAO().WithPredicate(dao.Match(ix, "alpha")).With(dWS, "a").OrderBy(dao.AscBy(dByRank, "alpha")).Limit(10).Select(dID)
	if err != nil {
		t.Fatal(err)
	}
	if ids := docIDs(got); len(ids) != 2 || !(ids[0] == 1 || ids[0] == 2) {
		t.Fatalf("matches in ws a: %v, want 1 and 2", ids)
	}
	got, err = plain.DAO().WithPredicate(dao.Match(ix, "alpha")).Limit(1).Select(dID)
	if err != nil || len(got) != 1 {
		t.Fatalf("LIMIT after the match: %v %v", got, err)
	}

	// The weights rank by class: the breadcrumb (A) match first with (1.0,
	// 0.1), the body (D) match first with (0.1, 1.0).
	for _, tc := range []struct {
		weights []float64
		first   int64
	}{{[]float64{1.0, 0.1}, 1}, {[]float64{0.1, 1.0}, 2}} {
		s := docSchema(conn, table, dao.RankQuery(ix, tc.weights...))
		got, err := s.DAO().WithPredicate(dao.Match(ix, "alpha")).With(dWS, "a").OrderBy(dao.AscBy(dByRank, "alpha")).Select(dID)
		if err != nil {
			t.Fatalf("%v: %v", tc.weights, err)
		}
		if ids := docIDs(got); len(ids) != 2 || ids[0] != tc.first {
			t.Errorf("weights %v: order %v, want %d first", tc.weights, ids, tc.first)
		}
	}
}

func TestFullTextPG_ConfigurationsDiffer(t *testing.T) {
	conn, schema := searchSchema(t)
	table := schema + ".doc"
	docTable(t, conn, table)
	mustExec(t, conn, "INSERT INTO "+table+` (id, ws, crumb, body) VALUES (1, 'a', 'x', 'she was running fast')`)
	simple := dao.FullTextIndex{Name: "tsv", Table: table, Columns: []string{"crumb", "body"}}
	english := dao.FullTextIndex{Name: "tsv_en", Table: table, Columns: []string{"body"}, Config: "english"}
	s := docSchema(conn, table, dao.RankQuery(simple))
	got, err := s.DAO().WithPredicate(dao.Match(english, "runs")).Select(dID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("english: 'runs' should stem to match 'running', got %v", docIDs(got))
	}
	got, err = s.DAO().WithPredicate(dao.Match(simple, "runs")).Select(dID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("simple: 'runs' must not match 'running', got %v", docIDs(got))
	}
}

// Query text from search/query.TSQuery matches literally: a prefix, and terms
// holding a quote, a backslash, & and :, each find only their document, and a
// term built to look like tsquery syntax matches nothing. Inside a term the
// configuration's parser still splits on punctuation, so "c\d" is the phrase
// c <-> d, as SQLite's tokenizer would read it: the characters are words'
// separators, never operators. Each document's words are its own, so a term
// can only find the one it was written for.
func TestFullTextPG_TSQueryMatchesLiterally(t *testing.T) {
	conn, schema := searchSchema(t)
	table := schema + ".doc"
	docTable(t, conn, table)
	mustExec(t, conn, "INSERT INTO "+table+` (id, ws, crumb, body) VALUES
		(1, 'a', 'x', 'rock & roll forever'),
		(2, 'a', 'x', 'o''brien wrote it'),
		(3, 'a', 'x', 'the path c\d here'),
		(4, 'a', 'x', 'an e:f ratio'),
		(5, 'a', 'x', 'only y here')`)
	ix := dao.FullTextIndex{Name: "tsv", Table: table, Columns: []string{"crumb", "body"}}
	s := docSchema(conn, table, dao.RankQuery(ix))
	for _, tc := range []struct {
		terms []query.Term
		want  []int64
	}{
		{[]query.Term{{Text: "rock&roll"}}, []int64{1}},
		{[]query.Term{{Text: "o'brien"}}, []int64{2}},
		{[]query.Term{{Text: `c\d`}}, []int64{3}},
		{[]query.Term{{Text: "e:f"}}, []int64{4}},
		{[]query.Term{{Text: "wro", Prefix: true}}, []int64{2}},
		{[]query.Term{{Text: `x' | 'y`}}, nil},
		{[]query.Term{{Text: "only"}, {Text: "y"}}, []int64{5}},
	} {
		q, err := query.TSQuery(tc.terms)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.DAO().WithPredicate(dao.Match(ix, q)).OrderBy(dao.AscBy(dByRank, q)).Select(dID)
		if err != nil {
			t.Errorf("%q: %v", q, err)
			continue
		}
		if ids := docIDs(got); !reflect.DeepEqual(ids, tc.want) && !(len(ids) == 0 && len(tc.want) == 0) {
			t.Errorf("%q matched %v, want %v", q, ids, tc.want)
		}
	}
}
