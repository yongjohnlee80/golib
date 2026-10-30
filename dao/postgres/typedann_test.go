package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

// In-process cells for the typed vector distance, row locks, composite keys bound as arrays and
// the index lifecycle's renderings. The live cells are typedann_integration_test.go.

// annConn is a DataConn with PostgreSQL's dialect that records every statement it is sent, on
// the pool or on a transaction, and answers no rows.
type annConn struct {
	sent []string
	args [][]any
}

type annRows struct{}

func (annRows) Next() bool        { return false }
func (annRows) Scan(...any) error { return nil }
func (annRows) Close() error      { return nil }
func (annRows) Err() error        { return nil }
func (c *annConn) record(q string, a []any) {
	c.sent, c.args = append(c.sent, q), append(c.args, a)
}
func (c *annConn) QueryContext(_ context.Context, q string, a ...any) (dao.Rows, error) {
	c.record(q, a)
	return annRows{}, nil
}
func (c *annConn) ExecContext(_ context.Context, q string, a ...any) (dao.Result, error) {
	c.record(q, a)
	return pgxResult{}, nil
}
func (c *annConn) Dialect() dao.Dialect                      { return PostgresDialect{} }
func (c *annConn) Begin(context.Context) (dao.TxConn, error) { return annTx{c}, nil }
func (c *annConn) Name() string                              { return "cap" }
func (c *annConn) Close() error                              { return nil }

type annTx struct{ c *annConn }

func (t annTx) QueryContext(ctx context.Context, q string, a ...any) (dao.Rows, error) {
	return t.c.QueryContext(ctx, q, a...)
}
func (t annTx) ExecContext(ctx context.Context, q string, a ...any) (dao.Result, error) {
	return t.c.ExecContext(ctx, q, a...)
}
func (annTx) Commit() error   { return nil }
func (annTx) Rollback() error { return nil }

type embRow struct {
	Doc  int64
	Ord  int
	Name string
}

type embField string

type embSort string

const byNear embSort = "near"

var embFields = map[embField]dao.Field[*embRow]{
	"doc":  {Column: "doc", Scan: func(r *embRow) any { return &r.Doc }, Value: func(r *embRow) any { return r.Doc }},
	"ord":  {Column: "ord", Scan: func(r *embRow) any { return &r.Ord }, Value: func(r *embRow) any { return r.Ord }},
	"name": {Column: "name", Scan: func(r *embRow) any { return &r.Name }, Value: func(r *embRow) any { return r.Name }},
}

func embSchema(c *annConn, table string, t dao.VectorType) *dao.Schema[*embRow, embField, embSort, int64] {
	return dao.New[*embRow, embField, embSort, int64](c,
		dao.Table[*embRow, embField, embSort, int64](table),
		dao.ID[*embRow, embField, embSort, int64]("doc"),
		dao.Fields[*embRow, embField, embSort, int64](embFields),
		dao.SortParam[*embRow, embField, embSort, int64](byNear, dao.DistanceAs(dao.C("vec"), dao.Cosine, t)),
	)
}

var (
	f32x4    = dao.VectorType{Kind: dao.VectorFloat32, Dims: 4}
	f16x3000 = dao.VectorType{Kind: dao.VectorFloat16, Dims: 3000}
)

// D1: the cast and the distance render exactly, for both kinds and every metric.
func TestTypedVectorDistanceRenders(t *testing.T) {
	var d PostgresDialect
	for _, tc := range []struct {
		m    dao.Metric
		t    dao.VectorType
		want string
	}{
		{dao.Cosine, f32x4, `("emb"."vec"::vector(4)) <=> $1::vector(4)`},
		{dao.L2, f32x4, `("emb"."vec"::vector(4)) <-> $1::vector(4)`},
		{dao.InnerProduct, f32x4, `("emb"."vec"::vector(4)) <#> $1::vector(4)`},
		{dao.Cosine, f16x3000, `("emb"."vec"::halfvec(3000)) <=> $1::halfvec(3000)`},
	} {
		if got := d.TypedVectorDistance(`"emb"."vec"`, tc.m, tc.t, "$1"); got != tc.want {
			t.Errorf("%s %+v: %s, want %s", tc.m, tc.t, got, tc.want)
		}
	}
}

// Cell 3: the index's element and the query's ORDER BY share one expression, byte for byte, for
// both kinds; and the limits refuse.
func TestTheIndexAndTheQueryShareOneExpression(t *testing.T) {
	for _, vt := range []dao.VectorType{f32x4, f16x3000} {
		c := &annConn{}
		if _, err := embSchema(c, "emb", vt).DAO().OrderBy(dao.AscBy(byNear, Vector{1, 2, 3, 4})).Limit(5).Select(); err != nil {
			t.Fatalf("%+v: Select: %v", vt, err)
		}
		el, err := HNSWIndexElement("vec", dao.Cosine, vt)
		if err != nil {
			t.Fatal(err)
		}
		cast := PostgresDialect{}.VectorCast(`"vec"`, vt)
		ops := map[dao.VectorKind]string{dao.VectorFloat32: "vector_cosine_ops", dao.VectorFloat16: "halfvec_cosine_ops"}[vt.Kind]
		if want := "(" + cast + " " + ops + ")"; el.String() != want {
			t.Errorf("%+v: element %s, want %s", vt, el, want)
		}
		if !strings.Contains(c.sent[0], " ORDER BY "+cast+" <=> ") {
			t.Errorf("%+v: the ORDER BY of %q is not the element's expression %s", vt, c.sent[0], cast)
		}
	}
	for _, tc := range []struct {
		t  dao.VectorType
		ok bool
	}{
		{dao.VectorType{Kind: dao.VectorFloat32, Dims: 2000}, true},
		{dao.VectorType{Kind: dao.VectorFloat32, Dims: 2001}, false},
		{dao.VectorType{Kind: dao.VectorFloat16, Dims: 4000}, true},
		{dao.VectorType{Kind: dao.VectorFloat16, Dims: 4001}, false},
	} {
		_, err := HNSWIndexElement("vec", dao.Cosine, tc.t)
		if tc.ok != (err == nil) || (err != nil && !errors.Is(err, errs.ErrInvalidArgument)) {
			t.Errorf("HNSW %+v: %v", tc.t, err)
		}
	}
	for _, bad := range []dao.VectorType{{Kind: dao.VectorFloat32, Dims: 0}, {Kind: dao.VectorFloat16, Dims: 16001}, {Kind: 7, Dims: 4}} {
		if err := (PostgresDialect{}).CheckVectorType(bad); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("type %+v: %v, want ErrInvalidArgument", bad, err)
		}
	}
	// a type past the engine's limit fails the key's queries, never the declaration
	c := &annConn{}
	_, err := embSchema(c, "emb", dao.VectorType{Kind: dao.VectorFloat32, Dims: 16001}).DAO().OrderBy(dao.AscBy(byNear, Vector{1})).Select()
	if !errors.Is(err, dao.ErrUnsupported) || !errors.Is(err, errs.ErrInvalidArgument) || len(c.sent) != 0 {
		t.Errorf("16,001 dimensions: %v, sent %q; want ErrUnsupported and ErrInvalidArgument, nothing sent", err, c.sent)
	}
}

// D2 (and cell 14's rendering): the lock names the table's unqualified last part; the
// refusals send nothing.
func TestRowLockRendersAndRefuses(t *testing.T) {
	ctx := context.Background()
	c := &annConn{}
	s := embSchema(c, "app.lease", f32x4)
	tx := dao.Begin(ctx)
	if _, err := s.On(tx, dao.WithRowLock(dao.LockForUpdate)).With("doc", 1).Get(); err != nil && !errors.Is(err, dao.ErrNoRows) {
		t.Fatalf("Get: %v", err)
	}
	if got := c.sent[len(c.sent)-1]; !strings.HasSuffix(got, ` LIMIT $2 FOR UPDATE OF "lease"`) {
		t.Errorf("Get under FOR UPDATE: %s", got)
	}
	if _, err := s.On(tx, dao.WithRowLock(dao.LockForShare)).Select(); err != nil {
		t.Fatal(err)
	}
	if got := c.sent[len(c.sent)-1]; !strings.HasSuffix(got, ` FOR SHARE OF "lease"`) {
		t.Errorf("Select under FOR SHARE: %s", got)
	}
	_ = tx.Rollback()

	for name, tc := range map[string]struct {
		run  func(d dao.DAO[*embRow, embField, int64]) error
		pool bool
		lock dao.RowLock
		want error
	}{
		"a pool DAO": {func(d dao.DAO[*embRow, embField, int64]) error { _, err := d.Select(); return err }, true, dao.LockForUpdate, dao.ErrRowLockNeedsTx},
		"Count":      {func(d dao.DAO[*embRow, embField, int64]) error { _, err := d.Count(); return err }, false, dao.LockForUpdate, dao.ErrUnsupported},
		"Exists":     {func(d dao.DAO[*embRow, embField, int64]) error { _, err := d.Exists(); return err }, false, dao.LockForUpdate, dao.ErrUnsupported},
		"CountDistinct": {func(d dao.DAO[*embRow, embField, int64]) error {
			_, err := d.(dao.DistinctCounter[embField]).CountDistinct("name")
			return err
		}, false, dao.LockForUpdate, dao.ErrUnsupported},
		"SelectDistinct": {func(d dao.DAO[*embRow, embField, int64]) error {
			_, err := d.(dao.DistinctSelector[*embRow, embField]).SelectDistinct("name")
			return err
		}, false, dao.LockForUpdate, dao.ErrUnsupported},
		"Insert": {func(d dao.DAO[*embRow, embField, int64]) error { _, err := d.Set("name", "x").Insert(); return err }, false, dao.LockForUpdate, dao.ErrUnsupported},
		"Update": {func(d dao.DAO[*embRow, embField, int64]) error { return d.With("doc", 1).Set("name", "x").Update() }, false, dao.LockForUpdate, dao.ErrUnsupported},
		"Delete": {func(d dao.DAO[*embRow, embField, int64]) error { return d.With("doc", 1).Delete() }, false, dao.LockForUpdate, dao.ErrUnsupported},
		"Batch": {func(d dao.DAO[*embRow, embField, int64]) error {
			return d.Batch().Add(map[embField]any{"name": "x"}).Flush()
		}, false, dao.LockForUpdate, dao.ErrUnsupported},
		"a zero lock": {func(d dao.DAO[*embRow, embField, int64]) error { _, err := d.Select(); return err }, false, 0, errs.ErrInvalidArgument},
	} {
		t.Run(name, func(t *testing.T) {
			c := &annConn{}
			s := embSchema(c, "app.lease", f32x4)
			d := s.DAO(dao.WithRowLock(tc.lock))
			if !tc.pool {
				d = s.On(dao.Begin(ctx), dao.WithRowLock(tc.lock))
			}
			if err := tc.run(d); !errors.Is(err, tc.want) {
				t.Fatalf("err %v, want %v", err, tc.want)
			}
			if len(c.sent) != 0 {
				t.Fatalf("sent %q; want nothing", c.sent)
			}
		})
	}
}

// D4 (cell 12's rendering): 500 keys are two bound arrays; empty keys match nothing with no
// parameter; a wrong width or type is refused before anything is sent.
func TestKeysInBindsOneArrayPerColumn(t *testing.T) {
	cols := []dao.KeyColumn{{Col: "doc", Elem: dao.ArrayInt64}, {Col: "ord", Elem: dao.ArrayInt32}}
	keys := make([][]any, 500)
	for i := range keys {
		keys[i] = []any{int64(i), i % 7}
	}
	c := &annConn{}
	if _, err := embSchema(c, "emb", f32x4).DAO().WithPredicate(dao.KeysIn(cols, keys)).Select(); err != nil {
		t.Fatal(err)
	}
	if want := `WHERE (doc, ord) IN (SELECT * FROM unnest($1::bigint[], $2::integer[]))`; !strings.Contains(c.sent[0], want) {
		t.Errorf("SQL %s; want %s", c.sent[0], want)
	}
	if len(c.args[0]) != 2 {
		t.Fatalf("args %d; want 2", len(c.args[0]))
	}
	docs, ords := c.args[0][0].([]int64), c.args[0][1].([]int32)
	if len(docs) != 500 || docs[499] != 499 || ords[499] != 499%7 {
		t.Errorf("arrays %d %v %v", len(docs), docs[499], ords[499])
	}

	c = &annConn{}
	if _, err := embSchema(c, "emb", f32x4).DAO().WithPredicate(dao.KeysIn(cols, nil)).Select(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.sent[0], "WHERE 1 = 0") || len(c.args[0]) != 0 {
		t.Errorf("empty keys: %s %v", c.sent[0], c.args[0])
	}
	for name, k := range map[string][][]any{
		"a short key":     {{int64(1)}},
		"text for int64":  {{"1", 1}},
		"past int32":      {{int64(1), int64(1) << 40}},
		"a float for int": {{1.5, 1}},
	} {
		c := &annConn{}
		if _, err := embSchema(c, "emb", f32x4).DAO().WithPredicate(dao.KeysIn(cols, k)).Select(); !errors.Is(err, errs.ErrInvalidArgument) || len(c.sent) != 0 {
			t.Errorf("%s: %v, sent %q", name, err, c.sent)
		}
	}
}

// D3: the predicate literal escapes \ and ' whatever standard_conforming_strings is, and
// refuses control characters.
func TestEscapeLiteral(t *testing.T) {
	for in, want := range map[string]string{
		`openai|m||4`: `E'openai|m||4'`,
		`a\b'c\'`:     `E'a\\b''c\\'''`,
		`é`:           `E'é'`,
	} {
		if got, err := escapeLiteral(in); err != nil || got != want {
			t.Errorf("%q: %s %v, want %s", in, got, err, want)
		}
	}
	for _, bad := range []string{"a\x00b", "a\nb", "a\x7fb", "\xff"} {
		if _, err := escapeLiteral(bad); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("%q: %v, want ErrInvalidArgument", bad, err)
		}
	}
}

// D3: a spec renders exactly, and checks every part before anything is sent.
func TestIndexSpecRendersAndChecks(t *testing.T) {
	el, err := HNSWIndexElement("vec", dao.Cosine, f32x4)
	if err != nil {
		t.Fatal(err)
	}
	s := IndexSpec{Table: "app.rag_embedding", Name: "rag_embedding_hnsw_ab12", Method: "hnsw", Element: el,
		WhereEq: []EqText{{Column: "model", Value: `openai|m||4`}}}
	if err := s.check(); err != nil {
		t.Fatal(err)
	}
	want := `CREATE INDEX CONCURRENTLY "rag_embedding_hnsw_ab12" ON "app"."rag_embedding" USING hnsw (("vec"::vector(4)) vector_cosine_ops) WHERE "model" = E'openai|m||4'`
	if got := s.createSQL(true, s.Table, s.Name); got != want {
		t.Errorf("create:\n got  %s\n want %s", got, want)
	}
	for name, mut := range map[string]func(*IndexSpec){
		"an upper-case name":    func(s *IndexSpec) { s.Name = "Bad" },
		"a 64-byte name":        func(s *IndexSpec) { s.Name = strings.Repeat("a", 64) },
		"a quoted name":         func(s *IndexSpec) { s.Name = `a"b` },
		"a three-part table":    func(s *IndexSpec) { s.Table = "a.b.c" },
		"gin":                   func(s *IndexSpec) { s.Method = "gin" },
		"no element":            func(s *IndexSpec) { s.Element = IndexElement{} },
		"a bad predicate value": func(s *IndexSpec) { s.WhereEq = []EqText{{Column: "model", Value: "a\nb"}} },
		"a bad predicate col":   func(s *IndexSpec) { s.WhereEq = []EqText{{Column: "Model", Value: "a"}} },
	} {
		bad := s
		mut(&bad)
		if err := bad.check(); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("%s: %v, want ErrInvalidArgument", name, err)
		}
	}
	if _, err := MarkSession(context.Background(), nil, "UPPER"); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("a bad token: %v", err)
	}
	if _, err := BackendAlive(context.Background(), &annConn{}, Backend{PID: 0, Token: "t"}); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("pid 0: %v", err)
	}
}
