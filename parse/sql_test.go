package parse_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/parse"
)

// SQL must satisfy the one required interface. This is a compile-time
// assertion: if the interface or the type drifts, the build fails here rather
// than at a call site in another package.
var _ parse.Parser[[]parse.Statement] = parse.SQL{}

// texts is a helper so the table below can compare against plain strings.
func texts(t *testing.T, s parse.SQL, src string) []string {
	t.Helper()
	stmts, err := s.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q): unexpected error: %v", src, err)
	}
	out := make([]string, len(stmts))
	for i, st := range stmts {
		out[i] = st.Text
	}
	return out
}

// A semicolon only ends a statement when it is not inside something. Each case
// here is a place where a naive strings.Split would be wrong, which is the
// entire reason this type exists.
func TestSQL_SemicolonInsideAConstructDoesNotSplit(t *testing.T) {
	cases := []struct {
		name string
		sql  parse.SQL
		src  string
		want []string
	}{
		{
			name: "plain two statements",
			src:  "SELECT 1; SELECT 2",
			want: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name: "trailing semicolon does not make an empty statement",
			src:  "SELECT 1;",
			want: []string{"SELECT 1"},
		},
		{
			name: "semicolon inside a single-quoted string",
			src:  "SELECT 'a;b'; SELECT 2",
			want: []string{"SELECT 'a;b'", "SELECT 2"},
		},
		{
			name: "doubled quote is an escape, not a close",
			src:  "SELECT 'it''s; fine'; SELECT 2",
			want: []string{"SELECT 'it''s; fine'", "SELECT 2"},
		},
		{
			name: "semicolon inside a quoted identifier",
			src:  `SELECT "odd;name"; SELECT 2`,
			want: []string{`SELECT "odd;name"`, "SELECT 2"},
		},
		{
			// The comment stays in the statement text. A splitter that stripped
			// comments would be reporting something other than what the caller
			// wrote, and the caller is the one who has to recognise it.
			name: "semicolon inside a line comment",
			src:  "SELECT 1 -- ; not a split\n; SELECT 2",
			want: []string{"SELECT 1 -- ; not a split", "SELECT 2"},
		},
		{
			name: "semicolon inside a block comment",
			src:  "SELECT /* ; */ 1; SELECT 2",
			want: []string{"SELECT /* ; */ 1", "SELECT 2"},
		},
		{
			name: "a backslash does not escape the closing quote",
			src:  `SELECT 'C:\'; SELECT 2`,
			want: []string{`SELECT 'C:\'`, "SELECT 2"},
		},
		{
			name: "backticks are ordinary text unless the dialect enables them",
			src:  "SELECT `a;b`",
			want: []string{"SELECT `a", "b`"},
		},
		{
			name: "backticks quote when enabled",
			sql:  parse.SQL{Backticks: true},
			src:  "SELECT `a;b`; SELECT 2",
			want: []string{"SELECT `a;b`", "SELECT 2"},
		},
		{
			name: "dollar-quoted body when enabled",
			sql:  parse.SQL{DollarQuotes: true},
			src:  "CREATE FUNCTION f() AS $$ BEGIN a; b; END $$; SELECT 2",
			want: []string{"CREATE FUNCTION f() AS $$ BEGIN a; b; END $$", "SELECT 2"},
		},
		{
			name: "tagged dollar quote when enabled",
			sql:  parse.SQL{DollarQuotes: true},
			src:  "SELECT $body$ a; b $body$; SELECT 2",
			want: []string{"SELECT $body$ a; b $body$", "SELECT 2"},
		},
		{
			name: "a lone dollar sign stays ordinary text",
			sql:  parse.SQL{DollarQuotes: true},
			src:  "SELECT $1; SELECT 2",
			want: []string{"SELECT $1", "SELECT 2"},
		},
		{
			// Valid PostgreSQL: the E prefix is the engine's opt-in to backslash
			// escapes, so the quote after the backslash does NOT close the run.
			name: "E-string backslash escape when the dialect enables it",
			sql:  parse.SQL{EStringEscapes: true},
			src:  `SELECT E'a\'b'; SELECT 2`,
			want: []string{`SELECT E'a\'b'`, "SELECT 2"},
		},
		{
			// The opt-in is the prefix, not the construct: an ordinary string
			// keeps the standard reading even with E-strings enabled, so a path
			// ending in a backslash still closes where it should.
			name: "an ordinary string keeps standard backslash handling",
			sql:  parse.SQL{EStringEscapes: true},
			src:  `SELECT 'C:\'; SELECT 2`,
			want: []string{`SELECT 'C:\'`, "SELECT 2"},
		},
		{
			// A trailing e on an identifier is not a string prefix.
			name: "a word ending in e does not make the next string an E-string",
			sql:  parse.SQL{EStringEscapes: true},
			src:  `SELECT value'a\'; SELECT 2`,
			want: []string{`SELECT value'a\'`, "SELECT 2"},
		},
		{
			name: "nested block comment when the dialect nests",
			sql:  parse.SQL{NestedBlockComments: true},
			src:  "SELECT /* a /* b; */ c */ 1; SELECT 2",
			want: []string{"SELECT /* a /* b; */ c */ 1", "SELECT 2"},
		},
	}
	if len(cases) < 17 {
		t.Fatalf("only %d cases; the table has shrunk and this test proves less "+
			"than it claims", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := texts(t, tc.sql, tc.src)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d statements %q, want %d %q",
					len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("statement %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// An unfinished construct and a wrong one are different conditions, and the two
// identities must not answer for each other — a caller that gives up on a
// syntax error must not thereby give up on input that was merely unfinished.
func TestSQL_UnterminatedIsNotTheSameConditionAsBadSyntax(t *testing.T) {
	cases := map[string]string{
		"unclosed string":        "SELECT 'oops",
		"unclosed identifier":    `SELECT "oops`,
		"unclosed block comment": "SELECT 1 /* oops",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parse.SQL{}.Parse([]byte(src))
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			if !errors.Is(err, parse.ErrUnterminated) {
				t.Errorf("must satisfy ErrUnterminated, got %v", err)
			}
			if errors.Is(err, parse.ErrSyntax) {
				t.Error("must NOT satisfy ErrSyntax: an unfinished construct is a " +
					"sibling condition, and answering both would make a caller that " +
					"gives up on ErrSyntax give up on resumable input")
			}
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Error("both siblings must still answer the shared general question")
			}
		})
	}
}

// The reverse direction, driven from the core type rather than from SQL,
// because SQL's splitter currently produces only the unfinished kind. Without
// this the sibling isolation is proven in one direction only.
func TestSyntaxError_TheOtherDirectionOfSiblingIsolation(t *testing.T) {
	err := error(parse.SyntaxError{Format: "sql", Want: "FROM", Got: `"WERE"`})
	if !errors.Is(err, parse.ErrSyntax) {
		t.Error("a non-Incomplete SyntaxError must satisfy ErrSyntax")
	}
	if errors.Is(err, parse.ErrUnterminated) {
		t.Error("a non-Incomplete SyntaxError must NOT satisfy ErrUnterminated")
	}
}

// The fields must survive wrapping, which is the whole reason SyntaxError is a
// type rather than a sentinel — and the target must be spelled as a VALUE.
func TestSyntaxError_AsRecoversPositionThroughWrapping(t *testing.T) {
	_, err := parse.SQL{}.Parse([]byte("SELECT 1;\nSELECT 'oops"))
	if err == nil {
		t.Fatal("want an error")
	}
	wrapped := errors.Join(errors.New("outer"), err)

	var se parse.SyntaxError
	if !errors.As(wrapped, &se) {
		t.Fatal("errors.As with a VALUE target must recover the SyntaxError")
	}
	if se.Pos.Line != 2 {
		t.Errorf("Pos.Line = %d, want 2 — the position must point at the line "+
			"the quote was opened on, not the start of the source", se.Pos.Line)
	}
	if !se.Incomplete {
		t.Error("an unclosed quote must be marked Incomplete")
	}

	// The pointer spelling silently fails to match. Pinned so that a change to
	// value semantics is caught here rather than in a consumer.
	var ptr *parse.SyntaxError
	if errors.As(wrapped, &ptr) {
		t.Error("a POINTER target now matches; the doc comment on SyntaxError " +
			"says it cannot and must be corrected")
	}
}

// Error() must read as a detailed, human-usable sentence. Requiring identity
// comparison is not a licence to make the text useless.
func TestSyntaxError_MessageIsUsableProse(t *testing.T) {
	e := parse.SyntaxError{
		Format: "sql",
		Pos:    parse.Position{Line: 3, Column: 17},
		Want:   "FROM",
		Got:    `"WERE"`,
	}
	got := e.Error()
	for _, want := range []string{"sql:3:17", "want FROM", `got "WERE"`, "(invalid argument: syntax)"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
	// The zero value must be prose, not a crash and not an empty string.
	if z := (parse.SyntaxError{}).Error(); z == "" || !strings.Contains(z, "parse:") {
		t.Errorf("the zero SyntaxError must still render usefully, got %q", z)
	}
}

// The verb is a routing hint taken only from the first token, so a keyword
// inside a string or an identifier must never become one.
func TestSQL_VerbComesOnlyFromTheFirstToken(t *testing.T) {
	stmts, err := parse.SQL{}.Parse([]byte(
		"SELECT 'DELETE FROM t';\n" +
			"  -- a comment first\n  INSERT INTO t VALUES (1);\n" +
			"/* block */ WITH x AS (SELECT 1) SELECT * FROM x;\n" +
			"(SELECT 1)"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"SELECT", "INSERT", "WITH", ""}
	if len(stmts) != len(want) {
		t.Fatalf("got %d statements, want %d", len(stmts), len(want))
	}
	for i, w := range want {
		if stmts[i].Verb != w {
			t.Errorf("statement %d (%q) verb = %q, want %q",
				i, stmts[i].Text, stmts[i].Verb, w)
		}
	}
}

// Positions must point into the ORIGINAL source, so a caller can report a line
// number that matches the file the user is looking at.
func TestSQL_PositionsPointAtTheOriginalSource(t *testing.T) {
	stmts, err := parse.SQL{}.Parse([]byte("SELECT 1;\n\n   SELECT 2;\nSELECT 3"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []parse.Position{
		{Offset: 0, Line: 1, Column: 1},
		{Offset: 14, Line: 3, Column: 4},
		{Offset: 24, Line: 4, Column: 1},
	}
	if len(stmts) != len(want) {
		t.Fatalf("got %d statements, want %d", len(stmts), len(want))
	}
	for i, w := range want {
		if stmts[i].Pos != w {
			t.Errorf("statement %d (%q) pos = %+v, want %+v",
				i, stmts[i].Text, stmts[i].Pos, w)
		}
	}
}

// A capability is discovered by asking, and the answer must come from the
// implementation rather than from anything the type declares about itself.
func TestCapabilityDiscovery(t *testing.T) {
	var p any = parse.SQL{}

	if v, ok := parse.AsValidator(p); !ok {
		t.Error("SQL implements Validate and must be discoverable as a Validator")
	} else if err := v.Validate([]byte("SELECT 'oops")); err == nil {
		t.Error("the discovered capability must be the real one and must report " +
			"the unclosed quote")
	}
	if _, ok := parse.AsSplitter(p); !ok {
		t.Error("SQL implements Split and must be discoverable as a Splitter")
	}
	if _, ok := parse.AsStreamParser[[]parse.Statement](p); !ok {
		t.Error("SQL implements ParseStream and must be discoverable")
	}
	if name := parse.FormatNameOf(p); name != "sql" {
		t.Errorf("FormatNameOf = %q, want %q", name, "sql")
	}

	// A type that implements only the required method must be discoverable as
	// NONE of them. Without this the assertions above would pass for a
	// discovery helper that simply always said yes.
	var bare any = minimalParser{}
	if _, ok := parse.AsValidator(bare); ok {
		t.Error("a parser without Validate must NOT be discoverable as a Validator")
	}
	if _, ok := parse.AsSplitter(bare); ok {
		t.Error("a parser without Split must NOT be discoverable as a Splitter")
	}
	if _, ok := parse.AsStreamParser[int](bare); ok {
		t.Error("a parser without ParseStream must NOT be discoverable")
	}
	if name := parse.FormatNameOf(bare); name != "parse" {
		t.Errorf("an unnamed parser must fall back to %q, got %q", "parse", name)
	}
}

// minimalParser implements the required interface and nothing else.
type minimalParser struct{}

func (minimalParser) Parse(src []byte) (int, error) { return len(src), nil }
