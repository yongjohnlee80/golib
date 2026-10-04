package dao

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// matcherDialect has only matching, as PostgreSQL does without FullTexter.
type matcherDialect struct{ returningDialect }

func (matcherDialect) FullTextMatch(ix FullTextIndex, ph string) string { return ix.Name + " @@ " + ph }

// rankerDialect also ranks against the bound query.
type rankerDialect struct{ matcherDialect }

func (rankerDialect) FullTextQueryRank(ix FullTextIndex, ws []float64, ph string) string {
	return "qrank(" + ix.Name + ", " + string(ix.Classes) + ", " + ph + ")"
}

var classIX = FullTextIndex{Name: "tsv", Table: "chunk", Columns: []string{"crumb", "body"}, Classes: []byte("AD")}

func TestFullTextIndex_Validate(t *testing.T) {
	t.Parallel()
	ok := []FullTextIndex{
		{Columns: []string{"a"}},
		{Config: "simple"}, {Config: "english"}, {Config: "myschema.my_cfg"}, {Config: "_x9"},
		{Config: strings.Repeat("a", 63)},
		{Columns: []string{"a", "b"}, Classes: []byte("AD")},
		{Columns: []string{"a", "b", "c", "d"}, Classes: []byte("DCBA")},
	}
	for _, ix := range ok {
		if err := ix.Validate(); err != nil {
			t.Errorf("%+v: %v", ix, err)
		}
	}
	bad := map[string]FullTextIndex{
		"a quote":           {Config: "sim'ple"},
		"a double quote":    {Config: `"simple"`},
		"a semicolon":       {Config: "a;b"},
		"a space":           {Config: "a b"},
		"uppercase":         {Config: "English"},
		"three parts":       {Config: "a.b.c"},
		"an empty part":     {Config: "a."},
		"a leading digit":   {Config: "1abc"},
		"too long":          {Config: strings.Repeat("a", 64)},
		"classes too short": {Columns: []string{"a", "b"}, Classes: []byte("A")},
		"a class E":         {Columns: []string{"a"}, Classes: []byte("E")},
		"a lowercase class": {Columns: []string{"a"}, Classes: []byte("a")},
		"a class twice":     {Columns: []string{"a", "b"}, Classes: []byte("AA")},
	}
	for name, ix := range bad {
		if err := ix.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("%s: %v, want ErrInvalidArgument", name, err)
		}
	}
}

// Match needs only a matcher now: an engine with matching and no FullTexter
// renders it, and other predicates number on.
func TestMatch_NeedsOnlyAMatcher(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{d: matcherDialect{}}
	if _, err := buildSchema(conn).DAO().WithPredicate(Match(testIX, "q")).With(aName, "x").Select(aID); err != nil {
		t.Fatal(err)
	}
	if want := `SELECT artist.id FROM "artist" WHERE chunk_fts @@ $1 AND artist.name = $2`; conn.lastQuery != want {
		t.Errorf("sql = %s", conn.lastQuery)
	}
}

// A Match-only schema with a bad configuration fails at admission, nothing
// sent, with the same error Validate gives.
func TestMatch_AnInvalidConfigFailsBeforeAnythingIsSent(t *testing.T) {
	t.Parallel()
	bad := FullTextIndex{Name: "tsv", Table: "chunk", Config: "sim'ple"}
	for _, d := range []Dialect{matcherDialect{}, ftDialect{}, returningDialect{}} {
		conn := &fakeConn{d: d}
		_, err := buildSchema(conn).DAO().WithPredicate(Match(bad, "q")).Select(aID)
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("%T: %v, want ErrInvalidArgument", d, err)
		}
		if conn.lastQuery != "" || conn.lastExec != "" {
			t.Errorf("%T: dispatched %q %q", d, conn.lastQuery, conn.lastExec)
		}
	}
	if err := bad.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("Validate = %v", err)
	}
}

func rankParam(e ParamExpr) Option[*artist, artistField, artistSort, string] {
	return SortParam[*artist, artistField, artistSort, string]("rank", e)
}

// On an engine with a query ranker, RankQuery binds the query; on one with only
// FullTexter it renders the plain rank and binds nothing, so the argument count
// equals the placeholder count.
func TestRankQuery_BindsWhereTheEngineNeedsIt(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{d: rankerDialect{}}
	_, err := buildSchema(conn, rankParam(RankQuery(classIX, 1, 0.1))).DAO().
		WithPredicate(Match(classIX, "q")).OrderBy(AscBy("rank", "q")).Limit(5).Select(aID)
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT artist.id FROM "artist" WHERE tsv @@ $1 ORDER BY qrank(tsv, AD, $2) ASC LIMIT $3`; conn.lastQuery != want {
		t.Errorf("ranker sql = %s", conn.lastQuery)
	}
	if !reflect.DeepEqual(conn.lastArgs, []any{"q", "q", int64(5)}) {
		t.Errorf("ranker args = %v", conn.lastArgs)
	}

	conn = &fakeConn{d: questionFT{}}
	_, err = buildSchema(conn, rankParam(RankQuery(testIX, 1, 0.1))).DAO().
		WithPredicate(Match(testIX, "q")).OrderBy(AscBy("rank", "q")).Limit(5).Select(aID)
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT artist.id FROM "artist" WHERE chunk_fts MATCH ? ORDER BY rank(chunk_fts) ASC LIMIT ?`; conn.lastQuery != want {
		t.Errorf("FullTexter sql = %s", conn.lastQuery)
	}
	if got := strings.Count(conn.lastQuery, "?"); got != len(conn.lastArgs) {
		t.Errorf("%d placeholders, %d args %v", got, len(conn.lastArgs), conn.lastArgs)
	}
}

// questionFT is SQLite-shaped: FullTexter and "?" placeholders.
type questionFT struct{ ftDialect }

func (questionFT) Placeholder(int) string { return "?" }

func TestRankQuery_DeclarationRefusals(t *testing.T) {
	t.Parallel()
	mustFatal(t, "too few weights", func() { RankQuery(classIX, 1) })
	mustFatal(t, "a NaN weight", func() { RankQuery(classIX, 1, nan()) })
	mustFatal(t, "an invalid config at New", func() {
		ix := classIX
		ix.Config = "sim'ple"
		buildSchema(&fakeConn{d: rankerDialect{}}, rankParam(RankQuery(ix)))
	})
	mustFatal(t, "weights without classes on a query ranker", func() {
		ix := classIX
		ix.Classes = nil
		buildSchema(&fakeConn{d: rankerDialect{}}, rankParam(RankQuery(ix, 1, 0.1)))
	})
	for name, classes := range map[string]string{"too short": "A", "out of range": "AE", "repeated": "AA"} {
		ix := classIX
		ix.Classes = []byte(classes)
		mustFatal(t, "classes "+name, func() { buildSchema(&fakeConn{d: rankerDialect{}}, rankParam(RankQuery(ix, 1, 0.1))) })
	}
	// No weights: classes are not needed.
	ix := classIX
	ix.Classes = nil
	buildSchema(&fakeConn{d: rankerDialect{}}, rankParam(RankQuery(ix)))
	// SQLite ignores classes and config when they are valid.
	buildSchema(&fakeConn{d: ftDialect{}}, rankParam(RankQuery(classIX, 1, 0.1)))
	// An engine with neither: the key fails its queries, never panics.
	conn := newConn()
	if _, err := buildSchema(conn, rankParam(RankQuery(classIX))).DAO().OrderBy(AscBy("rank", "q")).Select(aID); !errors.Is(err, ErrUnsupported) {
		t.Errorf("no full text: %v, want ErrUnsupported", err)
	}
}

// The other declarations check the index too, at New.
func TestFullText_AnInvalidIndexPanicsInEveryDeclaration(t *testing.T) {
	t.Parallel()
	ix := testIX
	ix.Config = "a;b"
	mustFatal(t, "Rank", func() {
		buildSchema(&fakeConn{d: ftDialect{}}, SortExpr[*artist, artistField, artistSort, string]("rank", Rank(ix)))
	})
	mustFatal(t, "FullTextJoin", func() {
		buildSchema(&fakeConn{d: ftDialect{}}, OptionalJoinExpr[*artist, artistField, artistSort, string]("fts", FullTextJoin(ix)))
	})
	if err := Rank(ix).check(ftDialect{}); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("Rank's needs: %v", err)
	}
	if err := Snippet(ix, "body", SnippetMarks{Tokens: 8}).check(ftDialect{}); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("Snippet's needs: %v", err)
	}
}

func nan() float64 { var z float64; return z / z }
