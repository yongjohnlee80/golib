package sql_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/sql"
)

// A statement owns its source from its first character up to the next statement's: its semicolon,
// the space and a comment after it are its own; before the first statement is the first's, past
// the last is the last's.
func TestStatementAt_OwnsUpToTheNextStatement(t *testing.T) {
	t.Parallel()
	src := "-- lead\nSELECT 1; -- c; x\n  SELECT 2;  \n"
	second := strings.Index(src, "SELECT 2")
	for _, tc := range []struct {
		at   int
		want string
	}{
		{0, "SELECT 1"}, // the leading comment
		{strings.Index(src, "SELECT 1"), "SELECT 1"},     // its first character
		{strings.Index(src, ";"), "SELECT 1"},            // its semicolon
		{strings.Index(src, "c; x"), "SELECT 1"},         // the comment after it
		{second - 1, "SELECT 1"},                         // the space before the next
		{second, "SELECT 2"},                             // the next's first character
		{len(src), "SELECT 2"},                           // past the end
		{len(src) + 10, "SELECT 2"},                      // well past it
		{strings.Index(src, "2;") + 1, "SELECT 2"},       // the last semicolon
		{strings.LastIndex(src, "\n"), "SELECT 2"},       // the trailing space
		{strings.Index(src, "SELECT 1") + 3, "SELECT 1"}, // inside a word
	} {
		got, ok, err := sql.PostgreSQLSplitter().StatementAt([]byte(src), tc.at)
		if err != nil || !ok || got.Text != tc.want {
			t.Errorf("at %d: %q ok=%v err=%v, want %q", tc.at, got.Text, ok, err, tc.want)
		}
	}
}

// The cursor on the commented-out DROP is in the statement whose comment holds it, never a
// statement of its own.
func TestStatementAt_ACommentedStatementIsNotOne(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"SELECT 1 # ; DROP TABLE t;",
		"SELECT 1 /*+ ; DROP TABLE t; */;",
	} {
		got, ok, err := sql.MySQLSplitter().StatementAt([]byte(src), strings.Index(src, "DROP"))
		if err != nil || !ok || !strings.HasPrefix(got.Text, "SELECT 1") {
			t.Errorf("%q: %q ok=%v err=%v, want the statement beginning SELECT 1", src, got.Text, ok, err)
		}
	}
}

// Nothing but space and comments holds no statement.
func TestStatementAt_NoStatement(t *testing.T) {
	t.Parallel()
	for _, src := range []string{"", "   \n", "-- only\n", "/* only */", ";;", "/*+ DROP TABLE t */"} {
		got, ok, err := sql.MySQLSplitter().StatementAt([]byte(src), 0)
		if err != nil || ok {
			t.Errorf("%q: %+v ok=%v err=%v, want no statement", src, got, ok, err)
		}
	}
}

// An unclosed string is the error Parse reports, from StatementAt too.
func TestStatementAt_ReportsAnUnclosedString(t *testing.T) {
	t.Parallel()
	_, ok, err := sql.SQL{}.StatementAt([]byte("SELECT 'open"), 0)
	if err == nil || ok {
		t.Fatalf("ok=%v err=%v, want the unclosed string's error", ok, err)
	}
}

// End is just past the last character, before the semicolon, so Text is the source between Pos and
// End; its line and column count runes, as Pos's do.
func TestParse_EndClosesTheText(t *testing.T) {
	t.Parallel()
	src := "SELECT 'é';\n  SELECT\n    2 ;  \nSELECT 3"
	stmts, err := sql.SQL{}.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []parse.Position{
		{Offset: 11, Line: 1, Column: 11},
		{Offset: 27, Line: 3, Column: 6},
		{Offset: 40, Line: 4, Column: 9},
	}
	if len(stmts) != len(want) {
		t.Fatalf("%d statements, want %d", len(stmts), len(want))
	}
	for i, s := range stmts {
		if s.Text != src[s.Pos.Offset:s.End.Offset] {
			t.Errorf("statement %d: Text %q, the source from Pos to End %q", i, s.Text, src[s.Pos.Offset:s.End.Offset])
		}
		if s.End != want[i] {
			t.Errorf("statement %d: End %+v, want %+v", i, s.End, want[i])
		}
	}
}
