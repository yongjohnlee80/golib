package sql_test

import (
	"reflect"
	"testing"

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
		"a doubled backtick is one": {
			src:   "SELECT `a``;b`; SELECT 2",
			mysql: []string{"SELECT `a``;b`", "SELECT 2"},
			bare:  []string{"SELECT `a``", "b`", "SELECT 2"},
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
		"sqlite": {sql.SQLiteSplitter(), sql.New(sql.TriggerBodies())},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: %+v, want %+v", name, tc.got, tc.want)
		}
	}
}
