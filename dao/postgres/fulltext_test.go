package postgres

import (
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

var pgIX = dao.FullTextIndex{Name: "tsv", Table: "app.chunk", Columns: []string{"crumb", "body"}, Classes: []byte("AD")}

func TestFullTextPG_Rendering(t *testing.T) {
	t.Parallel()
	d := PostgresDialect{}
	if got, want := d.FullTextMatch(pgIX, "$3"), `"app"."chunk"."tsv" @@ to_tsquery('simple'::regconfig, $3)`; got != want {
		t.Errorf("match\n got: %s\nwant: %s", got, want)
	}
	if got, want := d.FullTextQueryRank(pgIX, nil, "$2"), `-ts_rank_cd("app"."chunk"."tsv", to_tsquery('simple'::regconfig, $2))`; got != want {
		t.Errorf("rank, no weights\n got: %s\nwant: %s", got, want)
	}
	ix := pgIX
	ix.Config = "english"
	if got, want := d.FullTextMatch(ix, "$1"), `"app"."chunk"."tsv" @@ to_tsquery('english'::regconfig, $1)`; got != want {
		t.Errorf("english\n got: %s\nwant: %s", got, want)
	}
	ix.Config = "myschema.my_cfg"
	if got, want := d.FullTextMatch(ix, "$1"), `"app"."chunk"."tsv" @@ to_tsquery('myschema.my_cfg'::regconfig, $1)`; got != want {
		t.Errorf("qualified\n got: %s\nwant: %s", got, want)
	}
	// An index that would fail Validate, rendered directly, never puts its text
	// in the statement.
	ix.Config = "x'); DROP TABLE t; --"
	if got, want := d.FullTextMatch(ix, "$1"), `"app"."chunk"."tsv" @@ to_tsquery(NULL::regconfig, $1)`; got != want {
		t.Errorf("invalid config\n got: %s\nwant: %s", got, want)
	}
}

// Weights land in their column's class slot, the rest keep PostgreSQL's
// defaults: the arrays of the ADR's examples.
func TestFullTextPG_WeightsGoToTheirClass(t *testing.T) {
	t.Parallel()
	d := PostgresDialect{}
	for _, tc := range []struct {
		weights []float64
		want    string
	}{
		{[]float64{1.0, 0.1}, `-ts_rank_cd('{0.1, 0.2, 0.4, 1.0}', "app"."chunk"."tsv", to_tsquery('simple'::regconfig, $1))`},
		{[]float64{0.1, 1.0}, `-ts_rank_cd('{1.0, 0.2, 0.4, 0.1}', "app"."chunk"."tsv", to_tsquery('simple'::regconfig, $1))`},
		{[]float64{2.5, 0.75}, `-ts_rank_cd('{0.75, 0.2, 0.4, 2.5}', "app"."chunk"."tsv", to_tsquery('simple'::regconfig, $1))`},
	} {
		if got := d.FullTextQueryRank(pgIX, tc.weights, "$1"); got != tc.want {
			t.Errorf("%v\n got: %s\nwant: %s", tc.weights, got, tc.want)
		}
	}
	ix := pgIX
	ix.Classes = []byte("CB")
	if got, want := d.FullTextQueryRank(ix, []float64{9, 8}, "$1"), `-ts_rank_cd('{0.1, 9.0, 8.0, 1.0}', "app"."chunk"."tsv", to_tsquery('simple'::regconfig, $1))`; got != want {
		t.Errorf("classes C and B\n got: %s\nwant: %s", got, want)
	}
}

// PostgreSQL has matching and query ranking, not FullTexter: its rank and
// snippet would need the query bound again.
func TestFullTextPG_CapabilityHonesty(t *testing.T) {
	t.Parallel()
	var d dao.Dialect = PostgresDialect{}
	if dao.SupportsFullText(d) {
		t.Error("PostgresDialect claims dao.FullTexter")
	}
	if _, ok := d.(dao.FullTextMatcher); !ok {
		t.Error("PostgresDialect lacks dao.FullTextMatcher")
	}
	if _, ok := d.(dao.FullTextQueryRanker); !ok {
		t.Error("PostgresDialect lacks dao.FullTextQueryRanker")
	}
}
