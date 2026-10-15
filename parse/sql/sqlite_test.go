package sql_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/sql"
)

// The SQLite preset with one of its rules left out, so each case shows what the rule changes.
var (
	sqliteNoBrackets = sql.New(sql.TriggerBodies(), sql.Backticks(), sql.TclParameters())
	sqliteNoParams   = sql.New(sql.TriggerBodies(), sql.Backticks(), sql.BracketIdentifiers())
	sqliteNoTicks    = sql.New(sql.TriggerBodies(), sql.BracketIdentifiers(), sql.TclParameters())
)

// Each SQLite rule decides where a statement ends, and the splitter without it draws another
// boundary there.
func TestSQLiteSplitter_ReadsSQLitesTokens(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		src     string
		sqlite  []string
		without sql.SQL
		other   []string // what the splitter without the rule reads
	}{
		"a ; inside [a;b] is a byte of the name": {
			src:     "SELECT [a;b] FROM t; SELECT 2",
			sqlite:  []string{"SELECT [a;b] FROM t", "SELECT 2"},
			without: sqliteNoBrackets,
			other:   []string{"SELECT [a", "b] FROM t", "SELECT 2"},
		},
		"a quote inside brackets opens no string": {
			src:     "SELECT 1 AS [a'b]; DROP TABLE t; --'",
			sqlite:  []string{"SELECT 1 AS [a'b]", "DROP TABLE t"},
			without: sqliteNoBrackets,
			other:   []string{"SELECT 1 AS [a'b]; DROP TABLE t; --'"},
		},
		"a comment opener inside brackets opens no comment": {
			src:     "SELECT 1 AS [a/*b]; DROP TABLE t; --*/",
			sqlite:  []string{"SELECT 1 AS [a/*b]", "DROP TABLE t"},
			without: sqliteNoBrackets,
			other:   []string{"SELECT 1 AS [a/*b]; DROP TABLE t; --*/"},
		},
		"a bracketed name ends at the first ], which nothing escapes": {
			src:     "SELECT [a]];b]; SELECT 2",
			sqlite:  []string{"SELECT [a]]", "b]", "SELECT 2"},
			without: sqliteNoBrackets,
			other:   []string{"SELECT [a]]", "b]", "SELECT 2"},
		},
		"a ; inside a (…) suffix is a byte of the parameter": {
			src:     "SELECT $a(x;y); SELECT 2",
			sqlite:  []string{"SELECT $a(x;y)", "SELECT 2"},
			without: sqliteNoParams,
			other:   []string{"SELECT $a(x", "y)", "SELECT 2"},
		},
		"a quote inside a $ suffix opens no string": {
			src:     "SELECT $a(x'y); DROP TABLE t; --'",
			sqlite:  []string{"SELECT $a(x'y)", "DROP TABLE t"},
			without: sqliteNoParams,
			other:   []string{"SELECT $a(x'y); DROP TABLE t; --'"},
		},
		"a quote inside an @ suffix opens no string": {
			src:     "SELECT @a(x'y); DROP TABLE t; --'",
			sqlite:  []string{"SELECT @a(x'y)", "DROP TABLE t"},
			without: sqliteNoParams,
			other:   []string{"SELECT @a(x'y); DROP TABLE t; --'"},
		},
		"a quote inside a : suffix opens no string": {
			src:     "SELECT :a(x'y); DROP TABLE t; --'",
			sqlite:  []string{"SELECT :a(x'y)", "DROP TABLE t"},
			without: sqliteNoParams,
			other:   []string{"SELECT :a(x'y); DROP TABLE t; --'"},
		},
		"a quote inside a # suffix opens no string": {
			src:     "SELECT #a(x'y); DROP TABLE t; --'",
			sqlite:  []string{"SELECT #a(x'y)", "DROP TABLE t"},
			without: sqliteNoParams,
			other:   []string{"SELECT #a(x'y); DROP TABLE t; --'"},
		},
		"a comment opener inside a suffix opens no comment": {
			src:     "SELECT $a(/*); DROP TABLE t; --*/",
			sqlite:  []string{"SELECT $a(/*)", "DROP TABLE t"},
			without: sqliteNoParams,
			other:   []string{"SELECT $a(/*); DROP TABLE t; --*/"},
		},
		"a name carries :: before its suffix": {
			src:     "SELECT $a::b(x;y); SELECT 2",
			sqlite:  []string{"SELECT $a::b(x;y)", "SELECT 2"},
			without: sqliteNoParams,
			other:   []string{"SELECT $a::b(x", "y)", "SELECT 2"},
		},
		"a suffix follows its name directly": {
			src:     "SELECT $a (x;y)",
			sqlite:  []string{"SELECT $a (x", "y)"},
			without: sqliteNoParams,
			other:   []string{"SELECT $a (x", "y)"},
		},
		"a digit is a byte of its own, so the $ after it opens a parameter": {
			src:     "SELECT 1$a(x;y); SELECT 2",
			sqlite:  []string{"SELECT 1$a(x;y)", "SELECT 2"},
			without: sqliteNoParams,
			other:   []string{"SELECT 1$a(x", "y)", "SELECT 2"},
		},
		"a ; inside a backticked name is a byte of the name": {
			src:     "SELECT `a;b`; SELECT 2",
			sqlite:  []string{"SELECT `a;b`", "SELECT 2"},
			without: sqliteNoTicks,
			other:   []string{"SELECT `a", "b`", "SELECT 2"},
		},
		"$$x is a parameter, then statements": {
			src:     "SELECT $$x; SELECT 2 $$",
			sqlite:  []string{"SELECT $$x", "SELECT 2 $$"},
			without: sql.PostgreSQLSplitter(),
			other:   []string{"SELECT $$x; SELECT 2 $$"},
		},
		"$tag$ is a parameter": {
			src:     "SELECT $tag$a;b$tag$",
			sqlite:  []string{"SELECT $tag$a", "b$tag$"},
			without: sql.PostgreSQLSplitter(),
			other:   []string{"SELECT $tag$a;b$tag$"},
		},
		"named parameters": {
			src:     "SELECT :name, @name, #name, $name FROM t; SELECT 2",
			sqlite:  []string{"SELECT :name, @name, #name, $name FROM t", "SELECT 2"},
			without: sql.MySQLSplitter(),
			other:   []string{"SELECT :name, @name, #name, $name FROM t; SELECT 2"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := statementTexts(t, sql.SQLiteSplitter(), tc.src); !reflect.DeepEqual(got, tc.sqlite) {
				t.Errorf("SQLiteSplitter = %q, want %q", got, tc.sqlite)
			}
			if got := statementTexts(t, tc.without, tc.src); !reflect.DeepEqual(got, tc.other) {
				t.Errorf("without the rule = %q, want %q", got, tc.other)
			}
		})
	}
}

// A word is read whole under TclParameters, so the $ inside one is a byte of the word, not a
// parameter whose suffix would hide the ;.
func TestTclParameters_ADollarInsideAWordIsTheWords(t *testing.T) {
	t.Parallel()
	got := statementTexts(t, sql.New(sql.TclParameters()), "SELECT a$b(x;y); SELECT 2")
	if want := []string{"SELECT a$b(x", "y)", "SELECT 2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// What SQLite's tokenizer refuses, the splitter refuses: an unclosed [, a prefix with no name, and
// a suffix cut off by whitespace or the end of the text. A refusal the text running out caused is
// unterminated; one the next character caused is a syntax error. Either names the token's first
// character, and Validate and StatementAt refuse it too.
func TestSQLiteSplitter_RefusesWhatSQLiteRefuses(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		src        string
		at         int // the token's offset
		incomplete bool
	}{
		"an unclosed [":                     {"SELECT [a; SELECT 2", 7, true},
		"an unclosed backtick":              {"SELECT `a; SELECT 2", 7, true},
		"# is no comment":                   {"# a comment", 0, false},
		"# after a statement is no comment": {"SELECT 1 # ; DROP TABLE t;", 9, false},
		"a :: cast":                         {"SELECT x::text", 8, false},
		"an @> operator":                    {"SELECT a @> b", 9, false},
		"a $ before a space":                {"SELECT $ ", 7, false},
		"a $ at the end":                    {"SELECT $", 7, true},
		"a name of :: alone":                {"SELECT $::", 7, true},
		"a suffix cut by a space":           {"SELECT $a(x y)", 7, false},
		"a suffix cut by a line break":      {"SELECT $a(x\ny)", 7, false},
		"a suffix cut by the end":           {"SELECT :a(x", 7, true},
	} {
		t.Run(name, func(t *testing.T) {
			sp := sql.SQLiteSplitter()
			_, err := sp.Parse([]byte(tc.src))
			var se parse.SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("Parse(%q) = %v, want a syntax error", tc.src, err)
			}
			if se.Incomplete != tc.incomplete || errors.Is(err, parse.ErrUnterminated) != tc.incomplete ||
				errors.Is(err, parse.ErrSyntax) == tc.incomplete {
				t.Errorf("%v, want incomplete %v", err, tc.incomplete)
			}
			if se.Pos.Offset != tc.at {
				t.Errorf("the error names offset %d, want %d", se.Pos.Offset, tc.at)
			}
			if err := sp.Validate([]byte(tc.src)); err == nil {
				t.Error("Validate accepted it")
			}
			if _, ok, err := sp.StatementAt([]byte(tc.src), 0); err == nil || ok {
				t.Error("StatementAt offered a statement")
			}
		})
	}
}

// The SQLite rules belong to SQLite. Each script below reads exactly as it did on the PostgreSQL
// and MySQL presets, which have none of them.
func TestSQLiteRules_LeaveTheOtherPresetsAsTheyWere(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		sp   sql.SQL
		src  string
		want []string
	}{
		"postgresql: [ is a subscript":            {sql.PostgreSQLSplitter(), "SELECT [a;b] FROM t; SELECT 2", []string{"SELECT [a", "b] FROM t", "SELECT 2"}},
		"postgresql: a quote after [ opens":       {sql.PostgreSQLSplitter(), "SELECT 1 AS [a'b]; DROP TABLE t; --'", []string{"SELECT 1 AS [a'b]; DROP TABLE t; --'"}},
		"postgresql: $$ quotes":                   {sql.PostgreSQLSplitter(), "SELECT $$x; SELECT 2 $$", []string{"SELECT $$x; SELECT 2 $$"}},
		"postgresql: $a has no suffix":            {sql.PostgreSQLSplitter(), "SELECT $a(x;y)", []string{"SELECT $a(x", "y)"}},
		"postgresql: :: is a cast":                {sql.PostgreSQLSplitter(), "SELECT x::text; SELECT 2", []string{"SELECT x::text", "SELECT 2"}},
		"postgresql: # and @> are operators":      {sql.PostgreSQLSplitter(), "SELECT a # b, c @> d; SELECT 2", []string{"SELECT a # b, c @> d", "SELECT 2"}},
		"postgresql: a backtick is a byte":        {sql.PostgreSQLSplitter(), "SELECT `a;b`; SELECT 2", []string{"SELECT `a", "b`", "SELECT 2"}},
		"mysql: [ is a byte":                      {sql.MySQLSplitter(), "SELECT [a;b] FROM t; SELECT 2", []string{"SELECT [a", "b] FROM t", "SELECT 2"}},
		"mysql: a quote after [ opens":            {sql.MySQLSplitter(), "SELECT 1 AS [a'b]; DROP TABLE t; --'", []string{"SELECT 1 AS [a'b]; DROP TABLE t; --'"}},
		"mysql: a user variable has no suffix":    {sql.MySQLSplitter(), "SELECT @a(x'y); DROP TABLE t; --'", []string{"SELECT @a(x'y); DROP TABLE t; --'"}},
		"mysql: # is a comment":                   {sql.MySQLSplitter(), "SELECT 1 # ; DROP TABLE t;\nSELECT 2", []string{"SELECT 1 # ; DROP TABLE t;\nSELECT 2"}},
		"mysql: :: is two colons":                 {sql.MySQLSplitter(), "SELECT x::text; SELECT 2", []string{"SELECT x::text", "SELECT 2"}},
		"mysql: a lone # comment is no statement": {sql.MySQLSplitter(), "# a comment", []string{}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := statementTexts(t, tc.sp, tc.src); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%q, want %q", got, tc.want)
			}
		})
	}
}

// SQLite scripts on which a reading by other rules begins a statement somewhere SQLite does not.
// Each is read behind a probe statement, so its first statement is one that follows another; the
// probe is dropped and the offsets shifted back. nil starts means the script is refused.
func TestSQLiteSplitter_StatementStartsOnAdversarialScripts(t *testing.T) {
	t.Parallel()
	const probe = "SELECT 0;\n"
	for _, tc := range []struct {
		script string
		starts []int
	}{
		{"SELECT 1 AS [a'b]; DROP TABLE t; --'", []int{0, 19}},
		{"SELECT [a;b] FROM t; SELECT 2", []int{0, 21}},
		{"SELECT $a(x'y); DROP TABLE t; --'", []int{0, 16}},
		{"SELECT `a;b`; SELECT 2", []int{0, 14}},
		{"# a comment", nil},
		{"SELECT 1 # ; DROP TABLE t;", nil},
		{"SELECT x::text", nil},
		{"SELECT :name, @name FROM t; SELECT 2", []int{0, 28}},
		{"SELECT $$x; SELECT 2 $$", []int{0, 12}},
	} {
		t.Run(tc.script, func(t *testing.T) {
			stmts, err := sql.SQLiteSplitter().Parse([]byte(probe + tc.script))
			if tc.starts == nil {
				if err == nil {
					t.Errorf("read as %d statements, want a lexical error", len(stmts))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(stmts) == 0 || stmts[0].Pos.Offset != 0 {
				t.Fatalf("the probe was not the first statement: %+v", stmts)
			}
			starts := []int{}
			for _, st := range stmts[1:] {
				starts = append(starts, st.Pos.Offset-len(probe))
			}
			if !reflect.DeepEqual(starts, tc.starts) {
				t.Errorf("starts %v, want %v", starts, tc.starts)
			}
		})
	}
}
