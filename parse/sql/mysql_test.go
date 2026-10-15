package sql_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/sql"
)

// statementTexts are the statements' texts, as Parse returns them.
func statementTexts(t *testing.T, sp sql.SQL, src string) []string {
	t.Helper()
	stmts, err := sp.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	out := []string{}
	for _, s := range stmts {
		out = append(out, s.Text)
	}
	return out
}

// Each MySQL rule decides where a statement ends. The splitter without it draws another boundary,
// so every case shows what the rule changes, not only that the preset agrees with itself.
func TestMySQLSplitter_ReadsMySQLsComments(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		src         string
		mysql, bare []string
	}{
		"a # comment hides its semicolons": {
			src:   "SELECT 1 # ; DROP TABLE t;\nSELECT 2",
			mysql: []string{"SELECT 1 # ; DROP TABLE t;\nSELECT 2"},
			bare:  []string{"SELECT 1 #", "DROP TABLE t", "SELECT 2"},
		},
		"a lone # comment is no statement": {
			src:   "# DROP TABLE t",
			mysql: []string{},
			bare:  []string{"# DROP TABLE t"},
		},
		"-- before a letter is two minus signs": {
			src:   "SELECT balance--1; SELECT 2",
			mysql: []string{"SELECT balance--1", "SELECT 2"},
			bare:  []string{"SELECT balance--1; SELECT 2"},
		},
		"-- before a space is a comment": {
			src:   "SELECT 1 -- c; x\n; SELECT 2",
			mysql: []string{"SELECT 1 -- c; x", "SELECT 2"},
			bare:  []string{"SELECT 1 -- c; x", "SELECT 2"},
		},
		"-- at the end is a comment": {
			src:   "SELECT 1--",
			mysql: []string{"SELECT 1--"},
			bare:  []string{"SELECT 1--"},
		},
		"a backslash escapes a single quote": {
			src:   `SELECT 'a\';b'; SELECT 2 -- '`,
			mysql: []string{`SELECT 'a\';b'`, `SELECT 2 -- '`},
			bare:  []string{`SELECT 'a\'`, `b'; SELECT 2 -- '`},
		},
		"a backslash escapes a double quote": {
			src:   `SELECT "a\";b"; SELECT 2 -- "`,
			mysql: []string{`SELECT "a\";b"`, `SELECT 2 -- "`},
			bare:  []string{`SELECT "a\"`, `b"; SELECT 2 -- "`},
		},
		"a backticked name hides its semicolons": {
			src:   "SELECT `a;b`; SELECT 2",
			mysql: []string{"SELECT `a;b`", "SELECT 2"},
			bare:  []string{"SELECT `a", "b`", "SELECT 2"},
		},
		"a hint is a comment": {
			src:   "SELECT 1 /*+ ; DROP TABLE t; */; SELECT 2",
			mysql: []string{"SELECT 1 /*+ ; DROP TABLE t; */", "SELECT 2"},
			bare:  []string{"SELECT 1 /*+ ; DROP TABLE t; */", "SELECT 2"},
		},
		"a lone hint is no statement": {
			src:   "/*+ DROP TABLE t */",
			mysql: []string{},
			bare:  []string{},
		},
		"an executable comment's body is live": {
			src:   "/*!50000 SELECT 1; SELECT 2 */",
			mysql: []string{"SELECT 1", "SELECT 2 */"},
			bare:  []string{},
		},
	} {
		if got := statementTexts(t, sql.MySQLSplitter(), tc.src); !reflect.DeepEqual(got, tc.mysql) {
			t.Errorf("%s: MySQLSplitter = %q, want %q", name, got, tc.mysql)
		}
		if got := statementTexts(t, sql.SQL{}, tc.src); !reflect.DeepEqual(got, tc.bare) {
			t.Errorf("%s: the zero splitter = %q, want %q", name, got, tc.bare)
		}
	}
}

// A statement in an executable comment begins at its own first character, after the opener and
// its version digits.
func TestMySQLSplitter_AnExecutableBodyBeginsAfterItsOpener(t *testing.T) {
	t.Parallel()
	src := "/*!50000 SELECT 1; SELECT 2 */"
	stmts, err := sql.MySQLSplitter().Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 2 || stmts[0].Pos.Offset != 9 || stmts[1].Pos.Offset != 19 {
		t.Fatalf("statements %+v, want them at offsets 9 and 19", stmts)
	}
}

// Each preset is New with its engine's options.
func TestPresets_AreTheirEnginesOptions(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ got, want sql.SQL }{
		"postgresql": {sql.PostgreSQLSplitter(), sql.New(sql.DollarQuotes(), sql.NestedBlockComments(), sql.EStringEscapes())},
		"mysql": {sql.MySQLSplitter(), sql.New(sql.Backticks(), sql.HashComments(), sql.MySQLDashComments(),
			sql.BackslashEscapes(), sql.ExecutableComments())},
		"sqlite": {sql.SQLiteSplitter(), sql.New(sql.TriggerBodies(), sql.Backticks(), sql.BracketIdentifiers(),
			sql.TclParameters())},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: %+v, want %+v", name, tc.got, tc.want)
		}
	}
}

// An executable comment left open is malformed, as an unclosed block comment is: Parse, Validate
// and StatementAt all refuse it, where the server would.
func TestMySQLSplitter_RefusesAnUnclosedExecutableComment(t *testing.T) {
	t.Parallel()
	sp := sql.MySQLSplitter()
	open := []byte("/*!50000 SELECT 1")
	_, perr := sp.Parse(open)
	verr := sp.Validate(open)
	_, ok, aerr := sp.StatementAt(open, 10)
	for name, err := range map[string]error{"Parse": perr, "Validate": verr, "StatementAt": aerr} {
		var se parse.SyntaxError
		if !errors.As(err, &se) || !se.Incomplete || se.Pos.Offset != 0 {
			t.Errorf("%s: %v, want an incomplete syntax error at the opener", name, err)
		}
	}
	if ok {
		t.Error("StatementAt offered a statement in a malformed script")
	}
	if _, err := sp.Parse([]byte("/*!50000 SELECT 1 */")); err != nil {
		t.Errorf("a closed executable comment: %v", err)
	}
}
