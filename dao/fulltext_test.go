package dao

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

var testIX = FullTextIndex{Name: "chunk_fts", Table: "chunk", Key: "id", Columns: []string{"title", "body"}}

func mustFatal(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		var fatal errs.Fatal
		r := recover()
		if err, _ := r.(error); !errors.As(err, &fatal) {
			t.Errorf("%s: recovered %v, want an errs.Fatal", name, r)
		}
	}()
	f()
}

// An engine without full-text search: the query fails with ErrUnsupported,
// in a group as alone, and never reaches the database.
func TestMatch_AnEngineWithoutFullTextIsUnsupported(t *testing.T) {
	t.Parallel()
	if SupportsFullText(returningDialect{}) {
		t.Fatal("the test dialect claims full-text search; this test would show nothing")
	}
	for name, p := range map[string]Predicate{
		"alone":     Match(testIX, "x"),
		"in an Or":  Or(Eq("a", 1), Match(testIX, "x")),
		"in an And": And(Or(Match(testIX, "x"))),
	} {
		conn := newConn()
		if _, err := buildSchema(conn).DAO().WithPredicate(p).Select(aID); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
		if conn.lastQuery != "" {
			t.Errorf("%s: the query reached the database: %q", name, conn.lastQuery)
		}
	}
}

func TestFullText_DeclarationRefusals(t *testing.T) {
	t.Parallel()
	mustFatal(t, "too few weights", func() { Rank(testIX, 1) })
	mustFatal(t, "a NaN weight", func() { Rank(testIX, 1, math.NaN()) })
	mustFatal(t, "an infinite weight", func() { Rank(testIX, math.Inf(1), 1) })
	mustFatal(t, "a column not in the index", func() { Snippet(testIX, "tags", SnippetMarks{Tokens: 8}) })
	// a declaration on an engine without full-text search is a mistake in the code
	mustFatal(t, "a Rank sort on an engine without it", func() {
		buildSchema(newConn(), SortExpr[*artist, artistField, artistSort, string]("rank", Rank(testIX)))
	})
	mustFatal(t, "a FullTextJoin on an engine without it", func() {
		buildSchema(newConn(), OptionalJoinExpr[*artist, artistField, artistSort, string]("fts", FullTextJoin(testIX)))
	})
	if r := Rank(testIX); !r.isSet() {
		t.Error("Rank with no weights is equal weights, not a refusal")
	}
}

// A full-text piece inside Cmp renders at query time, so an engine without
// the capability is refused with ErrUnsupported there, never a panic.
func TestCmp_AFullTextPieceOnAnEngineWithoutItIsUnsupported(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]Predicate{
		"Rank on the left":         Cmp(Rank(testIX), OpLt, Int(0)),
		"inside a Coalesce":        Cmp(C("a"), OpEq, Coalesce(Snippet(testIX, "body", SnippetMarks{Tokens: 8}), C("b"))),
		"in a group":               Or(Cmp(Int(1), OpEq, Rank(testIX))),
		"plain columns still pass": nil,
	} {
		conn := newConn()
		if p == nil {
			if _, err := buildSchema(conn).DAO().WithPredicate(Cmp(C("a"), OpLte, C("b"))).Select(aID); err != nil {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: panicked (%v); want ErrUnsupported", name, r)
				}
			}()
			if _, err := buildSchema(conn).DAO().WithPredicate(p).Select(aID); !errors.Is(err, ErrUnsupported) {
				t.Errorf("%s: %v, want ErrUnsupported", name, err)
			}
		}()
		if conn.lastQuery != "" {
			t.Errorf("%s: the query reached the database: %q", name, conn.lastQuery)
		}
	}
}

// ftDialect has full-text search, rendered as plain markers so a test can
// read what each piece became.
type ftDialect struct{ returningDialect }

func (ftDialect) FullTextJoin(ix FullTextIndex) string { return "JOIN " + ix.Name }
func (ftDialect) FullTextMatch(ix FullTextIndex, ph string) string {
	return ix.Name + " MATCH " + ph
}
func (ftDialect) FullTextRank(ix FullTextIndex, _ []float64) string { return "rank(" + ix.Name + ")" }
func (ftDialect) FullTextSnippet(ix FullTextIndex, col int, m SnippetMarks) (string, error) {
	if m.Tokens == 0 {
		return "", errs.Wrap(errs.ErrInvalidArgument, "no tokens")
	}
	return "snip(" + ix.Name + ")", nil
}

// On an engine that has it, a full-text piece composes: inside Coalesce,
// inside Cmp, next to a bound Match, with the numbering running on.
func TestCmp_AFullTextPieceComposesOnAnEngineWithIt(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{d: ftDialect{}}
	_, err := buildSchema(conn).DAO().
		WithPredicate(Match(testIX, "q")).
		WithPredicate(Cmp(Coalesce(Snippet(testIX, "body", SnippetMarks{Tokens: 8}), C("b")), OpNe, C("a"))).
		With(aName, "n").
		Select(aID)
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT artist.id FROM "artist" WHERE chunk_fts MATCH $1 AND COALESCE(snip(chunk_fts), "b") <> "a" AND artist.name = $2`
	if conn.lastQuery != want {
		t.Errorf("sql = %q\nwant  %q", conn.lastQuery, want)
	}
	// a snippet the engine refuses (here: no tokens) is refused before it renders
	conn = &fakeConn{d: ftDialect{}}
	if _, err := buildSchema(conn).DAO().WithPredicate(Cmp(Snippet(testIX, "body", SnippetMarks{}), OpEq, C("a"))).Select(aID); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("a snippet the engine cannot render: %v, want its error", err)
	}
}

// Search is an entry point too: a search operator that builds a Match (the
// RawOp route) on an engine without full-text search fails with
// ErrUnsupported, the same as WithPredicate, and never renders as "1 = 0".
func TestSearch_AMatchOperatorOnAnEngineWithoutFullTextIsUnsupported(t *testing.T) {
	t.Parallel()
	text := Search[*artist, artistField, artistSort, string](RawOp("text", func(q string) Predicate { return Match(testIX, q) }))
	conn := newConn()
	if _, err := buildSchema(conn, text).DAO().Search("text:plover").Select(aID); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Search(text:plover) = %v, want ErrUnsupported", err)
	}
	if conn.lastQuery != "" {
		t.Errorf("the query reached the database: %q", conn.lastQuery)
	}
	// and on an engine that has it, the same operator is the bound Match
	conn = &fakeConn{d: ftDialect{}}
	if _, err := buildSchema(conn, text).DAO().Search("text:plover").With(aName, "n").Select(aID); err != nil ||
		conn.lastQuery != `SELECT artist.id FROM "artist" WHERE chunk_fts MATCH $1 AND artist.name = $2` {
		t.Errorf("on a full-text engine: %v, %q", err, conn.lastQuery)
	}
}

// Every predicate enters a query through addWhere, which is what makes the
// capability check hold for every entry point: an append anywhere else would
// be a way around it.
func TestEveryPredicateEntersThroughAddWhere(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("query_dao.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), "q.where = append("); n != 1 {
		t.Errorf("query_dao.go appends to q.where in %d places; only addWhere may", n)
	}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "query_dao.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "q.where = append(") {
			t.Errorf("%s appends to a query's predicates; only addWhere may", f)
		}
	}
}
