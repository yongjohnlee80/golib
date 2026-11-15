package dao

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// snippeterDialect ranks and snips against the bound query, as PostgreSQL does.
type snippeterDialect struct{ rankerDialect }

func (snippeterDialect) FullTextQuerySnippet(ix FullTextIndex, col int, m SnippetMarks, ph string) (string, error) {
	if strings.Contains(m.Open, `"`) {
		return "", errs.Wrap(errs.ErrInvalidArgument, "a quote in a mark")
	}
	return "qsnip(" + ix.Name + ", " + strconv.Itoa(col) + ", " + ph + ")", nil
}

const aSnip artistField = "snippet"

var snipMarks = SnippetMarks{Open: "[", Close: "]", Ellipsis: "…", Tokens: 16}

// snipSchema is the artist schema with a Param field, aSnip, of e.
func snipSchema(conn DataConn, e ParamExpr, extra ...Option[*artist, artistField, artistSort, string]) *Schema[*artist, artistField, artistSort, string] {
	fields := map[artistField]Field[*artist]{}
	for k, f := range artistFields {
		fields[k] = f
	}
	fields[aSnip] = Field[*artist]{Param: e, ReadOnly: true, Scan: func(a *artist) any { return &a.LabelGroup }}
	opts := append([]Option[*artist, artistField, artistSort, string]{Fields[*artist, artistField, artistSort, string](fields)}, extra...)
	return buildSchema(conn, opts...)
}

// A bound field's values come first, as the statement reads: the SELECT's, then
// the WHERE's, the ORDER BY's and the LIMIT's, each numbered once, with the
// snippet bound to its own value. On a FullTexter engine (SQLite's shape) the
// snippet binds nothing, and the placeholder count still equals the arguments.
func TestSnippetQuery_BindsInTheOrderTheStatementReads(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{d: snippeterDialect{}}
	d := snipSchema(conn, SnippetQuery(classIX, "body", snipMarks), rankParam(RankQuery(classIX, 1, 0.1))).DAO().
		WithPredicate(Match(classIX, "match")).With(aName, "n").OrderBy(AscBy("rank", "rank"))
	_, err := Bind(d, aSnip, "snip").Limit(5).Select(aID, aSnip)
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT artist.id, qsnip(tsv, 1, $1) FROM "artist" WHERE tsv @@ $2 AND artist.name = $3 ORDER BY qrank(tsv, AD, $4) ASC LIMIT $5`; conn.lastQuery != want {
		t.Errorf("snippeter sql = %s\nwant          %s", conn.lastQuery, want)
	}
	if want := []any{"snip", "match", "n", "rank", int64(5)}; !reflect.DeepEqual(conn.lastArgs, want) {
		t.Errorf("snippeter args = %v, want %v", conn.lastArgs, want)
	}

	conn = &fakeConn{d: questionFT{}}
	d = snipSchema(conn, SnippetQuery(testIX, "body", snipMarks), rankParam(RankQuery(testIX))).DAO().
		WithPredicate(Match(testIX, "q")).OrderBy(AscBy("rank", "q"))
	_, err = Bind(d, aSnip, "q").Limit(5).Select(aID, aSnip)
	if err != nil {
		t.Fatal(err)
	}
	if want := `SELECT artist.id, snip(chunk_fts) FROM "artist" WHERE chunk_fts MATCH ? ORDER BY rank(chunk_fts) ASC LIMIT ?`; conn.lastQuery != want {
		t.Errorf("FullTexter sql = %s", conn.lastQuery)
	}
	if got := strings.Count(conn.lastQuery, "?"); got != len(conn.lastArgs) {
		t.Errorf("%d placeholders, %d args %v", got, len(conn.lastArgs), conn.lastArgs)
	}
}

// Get and Iterate select a bound field as Select does.
func TestFieldParam_EveryReadBindsIt(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{d: snippeterDialect{}}
	s := snipSchema(conn, SnippetQuery(classIX, "body", snipMarks))
	_, _ = Bind(s.DAO().With(aName, "n"), aSnip, "q").Get(aID, aSnip)
	if want := `SELECT artist.id, qsnip(tsv, 1, $1) FROM "artist" WHERE artist.name = $2 LIMIT $3`; conn.lastQuery != want {
		t.Errorf("Get sql = %s", conn.lastQuery)
	}
	it, err := Bind(s.DAO().With(aName, "n"), aSnip, "q").Iterate(aSnip)
	if err != nil {
		t.Fatal(err)
	}
	_ = it.Close()
	if want := `SELECT qsnip(tsv, 1, $1) FROM "artist" WHERE artist.name = $2`; conn.lastQuery != want {
		t.Errorf("Iterate sql = %s", conn.lastQuery)
	}
	if want := []any{"q", "n"}; !reflect.DeepEqual(conn.lastArgs, want) {
		t.Errorf("Iterate args = %v", conn.lastArgs)
	}
}

// What a query gets wrong is an error naming the field, before anything is
// sent; a Param field is never in the implicit default columns.
func TestFieldParam_QueryRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		run  func(DAO[*artist, artistField, string]) error
		says string
	}{
		{"selected without Bind", func(d DAO[*artist, artistField, string]) error { _, err := d.Select(aID, aSnip); return err }, `field "snippet" binds 1 value(s)`},
		{"bound with two values", func(d DAO[*artist, artistField, string]) error {
			_, err := Bind(d, aSnip, "a", "b").Select(aSnip)
			return err
		}, `field "snippet" takes 1 value(s), got 2`},
		{"bound with none", func(d DAO[*artist, artistField, string]) error { _, err := Bind(d, aSnip).Select(aSnip); return err }, "takes 1 value(s), got 0"},
		{"a plain field bound", func(d DAO[*artist, artistField, string]) error { _, err := Bind(d, aName, "q").Select(aID); return err }, `field "name" binds no values`},
		{"an unknown field bound", func(d DAO[*artist, artistField, string]) error {
			_, err := Bind(d, "nope", "q").Select(aID)
			return err
		}, "nope"},
		{"counted distinct", func(d DAO[*artist, artistField, string]) error {
			_, err := CountDistinct(Bind(d, aSnip, "q"), aSnip)
			return err
		}, "is a Param field"},
		{"filtered on", func(d DAO[*artist, artistField, string]) error { _, err := d.With(aSnip, "q").Select(aID); return err }, ""},
	} {
		conn := &fakeConn{d: snippeterDialect{}}
		err := tc.run(snipSchema(conn, SnippetQuery(classIX, "body", snipMarks)).DAO())
		if err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: %v, want an error saying %q", tc.name, err, tc.says)
		}
		if conn.lastQuery != "" {
			t.Errorf("%s: sent %s", tc.name, conn.lastQuery)
		}
	}
	// no Default: every field but the Param one
	conn := &fakeConn{d: snippeterDialect{}}
	fields := map[artistField]Field[*artist]{
		aID:   artistFields[aID],
		aSnip: {Param: SnippetQuery(classIX, "body", snipMarks), ReadOnly: true, Scan: func(a *artist) any { return &a.LabelGroup }},
	}
	s := New(conn, Table[*artist, artistField, artistSort, string]("artist"), ID[*artist, artistField, artistSort, string](aID),
		Fields[*artist, artistField, artistSort, string](fields))
	if _, err := s.DAO().Select(); err != nil || conn.lastQuery != `SELECT artist.id FROM "artist"` {
		t.Errorf("the implicit defaults: %q %v", conn.lastQuery, err)
	}
}

// An engine that cannot render the field, or refuses the declaration's marks,
// fails the query selecting it with ErrUnsupported, never a panic; a schema
// that never selects it works as before.
func TestFieldParam_AnEngineThatCannotRenderItFailsTheQuery(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		d     Dialect
		marks SnippetMarks
	}{
		"no full text":          {returningDialect{}, snipMarks},
		"a matcher only":        {matcherDialect{}, snipMarks},
		"a mark it can't quote": {snippeterDialect{}, SnippetMarks{Open: `"`, Close: "]", Tokens: 16}},
		"FullTexter refusing":   {ftDialect{}, SnippetMarks{Open: "[", Close: "]"}},
	} {
		conn := &fakeConn{d: tc.d}
		s := snipSchema(conn, SnippetQuery(classIX, "body", tc.marks))
		if _, err := Bind(s.DAO(), aSnip, "q").Select(aID, aSnip); !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), `field "snippet"`) {
			t.Errorf("%s: %v, want ErrUnsupported naming the field", name, err)
		}
		if _, err := s.DAO().Select(aID); err != nil {
			t.Errorf("%s: a query not selecting it: %v", name, err)
		}
	}
}

// Declarations wrong on every engine panic at the call or at New.
func TestFieldParam_DeclarationRefusals(t *testing.T) {
	t.Parallel()
	mustFatal(t, "a column not in the index", func() { SnippetQuery(classIX, "title", snipMarks) })
	mustFatal(t, "an invalid config at New", func() {
		ix := classIX
		ix.Config = "sim'ple"
		snipSchema(&fakeConn{d: snippeterDialect{}}, SnippetQuery(ix, "body", snipMarks))
	})
	for name, tc := range map[string]struct {
		f    Field[*artist]
		says string
	}{
		"Param with Column":  {Field[*artist]{Param: SnippetQuery(classIX, "body", snipMarks), Column: "x", ReadOnly: true}, "sets Param with Column or Expr"},
		"Param with Expr":    {Field[*artist]{Param: SnippetQuery(classIX, "body", snipMarks), Expr: T("artist", "x"), ReadOnly: true}, "sets Param with Column or Expr"},
		"Param not ReadOnly": {Field[*artist]{Param: SnippetQuery(classIX, "body", snipMarks)}, "sets Param and is not ReadOnly"},
	} {
		fields := map[artistField]Field[*artist]{aID: artistFields[aID], aSnip: tc.f}
		mustPanic(t, name, tc.says, func() {
			New(&fakeConn{d: snippeterDialect{}}, Table[*artist, artistField, artistSort, string]("artist"),
				Fields[*artist, artistField, artistSort, string](fields))
		})
	}
	mustPanic(t, "a Param ID", "is a Param field", func() {
		fields := map[artistField]Field[*artist]{aSnip: {Param: SnippetQuery(classIX, "body", snipMarks), ReadOnly: true}}
		New(&fakeConn{d: snippeterDialect{}}, Table[*artist, artistField, artistSort, string]("artist"),
			ID[*artist, artistField, artistSort, string](aSnip), Fields[*artist, artistField, artistSort, string](fields))
	})
}

// mustPanic runs f and fails unless it panics saying says: the declaration's own check, not a
// later one the bad field also trips.
func mustPanic(t *testing.T, name, says string, f func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("%s: no panic", name)
		} else if !strings.Contains(fmt.Sprint(r), says) {
			t.Errorf("%s: panicked %q, want it to say %q", name, fmt.Sprint(r), says)
		}
	}()
	f()
}
