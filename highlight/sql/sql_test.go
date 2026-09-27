package sql_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/highlight/sql"
)

// styleOf is the style the highlighter gives the first occurrence of part in
// text, read at part's first byte; Normal where no span covers it.
func styleOf(t *testing.T, spans []highlight.Span, text, part string) highlight.Style {
	t.Helper()
	i := strings.Index(text, part)
	if i < 0 {
		t.Fatalf("%q is not in %q", part, text)
	}
	for _, s := range spans {
		if s.Start <= i && i < s.End {
			return s.Style
		}
	}
	return highlight.Normal
}

func TestEachConstructHasItsStyle(t *testing.T) {
	cases := []struct {
		dialect sql.Dialect
		text    string
		part    string
		want    highlight.Style
	}{
		{sql.PostgreSQL, "SELECT id FROM users", "SELECT", highlight.Keyword},
		{sql.PostgreSQL, "select id from users", "from", highlight.Keyword},
		{sql.PostgreSQL, "SELECT id FROM t WHERE id = 1", "WHERE", highlight.Keyword},
		{sql.MySQL, "INSERT INTO t VALUES (1) ON DUPLICATE KEY UPDATE n = 2", "DUPLICATE", highlight.Keyword},
		{sql.PostgreSQL, "SELECT id FROM users", "users", highlight.Normal},
		{sql.PostgreSQL, "CASE WHEN a THEN 1 END", "WHEN", highlight.ControlFlow},
		{sql.PostgreSQL, "CREATE TABLE t (id BIGINT, doc JSONB)", "JSONB", highlight.DataType},
		{sql.PostgreSQL, "SELECT count(*) FROM t", "count", highlight.Function},
		{sql.PostgreSQL, "SELECT now () ", "now", highlight.Function},
		{sql.PostgreSQL, "WHERE x IS NULL", "NULL", highlight.Constant},
		{sql.PostgreSQL, "WHERE name = 'ann'", "'ann'", highlight.String},
		{sql.PostgreSQL, "SELECT 'it''s'", "s'", highlight.String},
		{sql.PostgreSQL, "WHERE name = 'ann' AND id = 1", "AND", highlight.Keyword},
		{sql.MySQL, `WHERE name = "a\"b" OR id = 1`, "OR", highlight.Keyword},
		{sql.PostgreSQL, `SELECT E'a\nb'`, `\n`, highlight.SpecialChar},
		{sql.PostgreSQL, `SELECT 'a\nb'`, `\n`, highlight.String},
		{sql.PostgreSQL, "LIMIT 42", "42", highlight.DecVal},
		{sql.PostgreSQL, "SELECT 3.5", "3.5", highlight.Float},
		{sql.PostgreSQL, "SELECT 1e9", "1e9", highlight.Float},
		{sql.PostgreSQL, "SELECT 0x1f", "0x1f", highlight.BaseN},
		{sql.PostgreSQL, "SELECT 1 -- the one", "-- the", highlight.Comment},
		{sql.PostgreSQL, "-- TODO: index this", "TODO", highlight.Alert},
		{sql.PostgreSQL, "SELECT /* why */ 1", "why", highlight.Comment},
		{sql.PostgreSQL, "WHERE id = $1", "$1", highlight.Variable},
		{sql.PostgreSQL, "SELECT x::int", "::", highlight.Operator},
		{sql.PostgreSQL, "SELECT x::int", "int", highlight.DataType},
		{sql.PostgreSQL, "WHERE a <> b", "<>", highlight.Operator},
		{sql.PostgreSQL, "WHERE doc ? 'k'", "?", highlight.Operator},
		{sql.PostgreSQL, `SELECT "Select" FROM t`, `Select"`, highlight.Normal},
		{sql.PostgreSQL, "SELECT $$ body $$", "body", highlight.String},
		{sql.PostgreSQL, "SELECT $fn$ it's $fn$ + 1", "it's", highlight.String},
		{sql.PostgreSQL, "SELECT $fn$ x $fn$ + 1", "+", highlight.Operator},
		{sql.SQLite, "WHERE id = ?", "?", highlight.Variable},
		{sql.SQLite, "WHERE id = ?2", "?2", highlight.Variable},
		{sql.SQLite, "WHERE id = :id", ":id", highlight.Variable},
		{sql.SQLite, "WHERE id = @id", "@id", highlight.Variable},
		{sql.SQLite, "WHERE id = $id", "$id", highlight.Variable},
		{sql.SQLite, "SELECT [order] FROM t", "order", highlight.Normal},
		{sql.SQLite, "SELECT `select` FROM t", "select`", highlight.Normal},
		{sql.SQLite, "SELECT x'ff00'", "ff00", highlight.BaseN},
		{sql.SQLite, "PRAGMA foreign_keys", "PRAGMA", highlight.Keyword},
		{sql.MySQL, "SELECT `from` FROM t", "from`", highlight.Normal},
		{sql.MySQL, `SELECT "ann"`, `"ann"`, highlight.String},
		{sql.MySQL, `SELECT 'a\'b'`, `\'`, highlight.SpecialChar},
		{sql.MySQL, "SELECT 1 # a comment", "a comment", highlight.Comment},
		{sql.MySQL, "SELECT 1 -- a comment", "a comment", highlight.Comment},
		{sql.MySQL, "SELECT 1--2", "--2", highlight.Operator},
		{sql.MySQL, "SET @total = 0", "@total", highlight.Variable},
		{sql.PostgreSQL, "SELECT 1--2", "--2", highlight.Comment},
	}
	for _, c := range cases {
		spans, state := sql.Highlighter(c.dialect).HighlightBlock(c.text, 0)
		if got := styleOf(t, spans, c.text, c.part); got != c.want {
			t.Errorf("dialect %d, %q: %q is %v, want %v (spans %v)", c.dialect, c.text, c.part, got, c.want, spans)
		}
		if state != 0 {
			t.Errorf("dialect %d, %q: left state %d open on a complete line", c.dialect, c.text, state)
		}
	}
}

// highlightLines runs lines through one highlighter, carrying the state.
func highlightLines(h highlight.Highlighter, lines []string) ([][]highlight.Span, highlight.State) {
	var out [][]highlight.Span
	var state highlight.State
	for _, ln := range lines {
		var spans []highlight.Span
		spans, state = h.HighlightBlock(ln, state)
		out = append(out, spans)
	}
	return out, state
}

// What is left open on one line carries to the next, and closes where it
// closes: a block comment, PostgreSQL's nested one, a dollar-quoted body.
func TestAnOpenConstructCarriesToTheNextLine(t *testing.T) {
	cases := []struct {
		name    string
		dialect sql.Dialect
		lines   []string
		// part of the LAST line and its style there
		part string
		want highlight.Style
		// inside is a part of a middle line that must still be inside
		inside, insideLine string
		insideStyle        highlight.Style
	}{
		{"block comment", sql.PostgreSQL,
			[]string{"SELECT 1 /* a", "still comment", "done */ FROM t"}, "FROM", highlight.Keyword,
			"still", "still comment", highlight.Comment},
		{"nested comment", sql.PostgreSQL,
			[]string{"/* outer /* inner */", "SELECT still", "*/ SELECT"}, "SELECT", highlight.Keyword,
			"SELECT", "SELECT still", highlight.Comment},
		{"sqlite does not nest", sql.SQLite,
			[]string{"/* outer /* inner */", "SELECT now"}, "SELECT", highlight.Keyword,
			"", "", 0},
		{"dollar body", sql.PostgreSQL,
			[]string{"CREATE FUNCTION f() RETURNS int AS $body$", "SELECT 'x' $inner$", "$body$ LANGUAGE sql"},
			"LANGUAGE", highlight.Keyword, "SELECT", "SELECT 'x' $inner$", highlight.String},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spans, end := highlightLines(sql.Highlighter(c.dialect), c.lines)
			last := c.lines[len(c.lines)-1]
			if got := styleOf(t, spans[len(spans)-1], last, c.part); got != c.want {
				t.Errorf("last line %q: %q is %v, want %v", last, c.part, got, c.want)
			}
			if c.inside != "" {
				i := indexOf(c.lines, c.insideLine)
				if got := styleOf(t, spans[i], c.insideLine, c.inside); got != c.insideStyle {
					t.Errorf("line %q: %q is %v, want %v", c.insideLine, c.inside, got, c.insideStyle)
				}
			}
			if end != 0 {
				t.Errorf("state %d left open after the construct closed", end)
			}
		})
	}
}

func indexOf(lines []string, want string) int {
	for i, l := range lines {
		if l == want {
			return i
		}
	}
	return -1
}

// A string does not carry: an unclosed quote ends at the line, so a typed
// quote never recolours the rest of the file.
func TestAnOpenStringEndsAtTheLine(t *testing.T) {
	h := sql.Highlighter(sql.PostgreSQL)
	spans, state := h.HighlightBlock("SELECT 'unfinished", 0)
	if state != 0 {
		t.Errorf("an open string carried state %d", state)
	}
	if got := styleOf(t, spans, "SELECT 'unfinished", "unfinished"); got != highlight.String {
		t.Errorf("the open string is %v", got)
	}
}

// Two editors sharing one highlighter keep their own dollar tags apart.
func TestDollarTagsAreKeptApart(t *testing.T) {
	h := sql.Highlighter(sql.PostgreSQL)
	_, a := h.HighlightBlock("AS $a$", 0)
	_, b := h.HighlightBlock("AS $b$", 0)
	if a == 0 || b == 0 || a == b {
		t.Fatalf("states a=%d b=%d, want two distinct open states", a, b)
	}
	spans, st := h.HighlightBlock("x $b$ SELECT", a)
	if st != a {
		t.Errorf("$b$ closed the body opened by $a$ (state %d)", st)
	}
	if got := styleOf(t, spans, "x $b$ SELECT", "SELECT"); got != highlight.String {
		t.Errorf("after the wrong tag, SELECT is %v, want still String", got)
	}
}

// No input panics, and every span lies within its line, in order, without
// overlap: what an editor painting them relies on.
func TestSpansAreWellFormedForAnyInput(t *testing.T) {
	inputs := []string{"", "$", "$$", "$a", "'", `E'\`, "/*", "*/", "--", "-", "#", "`", "[", `"`,
		"0x", "1e", "1e+", ".5", "?", ":", "::", "@", "x'", "SELECT $1$ 2", "é'ü", "\t\t", "ab\x00cd",
		"/* /* */", "$tag$ $tag", "CREATE FUNCTION f() AS $$ BEGIN RETURN 1; END $$"}
	for _, d := range []sql.Dialect{sql.PostgreSQL, sql.SQLite, sql.MySQL} {
		h := sql.Highlighter(d)
		for _, in := range inputs {
			for _, prev := range []highlight.State{0, 1, 2, 1 << 16, 1<<16 + 5} {
				spans, _ := h.HighlightBlock(in, prev)
				end := 0
				for _, s := range spans {
					if s.Start < end || s.End <= s.Start || s.End > len(in) {
						t.Errorf("dialect %d, %q from state %d: bad span %+v after %d in %v", d, in, prev, s, end, spans)
					}
					end = s.End
				}
			}
		}
	}
}

func TestTheDefinitionsNameTheDialects(t *testing.T) {
	defs := sql.Definitions()
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
		if d.Highlighter == nil {
			t.Errorf("%s has no highlighter", d.Name)
		}
	}
	for _, want := range []string{"SQL (PostgreSQL)", "SQL (SQLite)", "SQL (MySQL)"} {
		if !names[want] {
			t.Errorf("no definition named %q in %v", want, names)
		}
	}
}

// Each dialect's file name reaches its own definition through a repository,
// which is how a file dialog's preview picks one: the SQLite and MySQL names
// are narrower than *.sql and must not fall to PostgreSQL.
func TestAFileNameReachesItsDialect(t *testing.T) {
	r := highlight.NewRepository(sql.Definitions()...)
	for file, want := range map[string]string{
		"schema.sql":       "SQL (PostgreSQL)",
		"fn.pgsql":         "SQL (PostgreSQL)",
		"dir/a.sqlite.sql": "SQL (SQLite)",
		"000001.mysql.sql": "SQL (MySQL)",
		"notes.sqlite":     "",
	} {
		d, ok := r.DefinitionForFileName(file)
		if d.Name != want || ok != (want != "") {
			t.Errorf("%s: %q %v, want %q", file, d.Name, ok, want)
		}
	}
}
