package dao

import (
	"errors"
	"math"
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
