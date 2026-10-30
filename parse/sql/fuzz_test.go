package sql_test

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/sql"
)

// FuzzParse: any source splits into statements or is refused, never panics, and each statement
// is what its doc says: Text is the source from Pos to End, the statements in order and apart.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", ";", ";;", "SELECT 1", "SELECT 1; SELECT 2;", "SELECT ';' FROM t; -- ;\nSELECT 2",
		"SELECT \"a;b\" FROM t", "/* ; */ SELECT 1", "$$ ; $$; SELECT 1", "$tag$ ; $tag$",
		"CREATE FUNCTION f() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql;", "SELECT 'unterminated",
		"INSERT INTO t VALUES (1, 'a''b');", "WITH x AS (SELECT 1) SELECT * FROM x",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		stmts, err := sql.New().Parse(src)
		if err != nil {
			var se parse.SyntaxError
			if errors.As(err, &se) && (se.Pos.Offset < 0 || se.Pos.Offset > len(src)) {
				t.Fatalf("refused at offset %d, outside a %d-byte source", se.Pos.Offset, len(src))
			}
			return
		}
		prev := 0
		for i, s := range stmts {
			if s.Pos.Offset < prev || s.End.Offset < s.Pos.Offset || s.End.Offset > len(src) {
				t.Fatalf("statement %d spans [%d, %d) after %d in a %d-byte source", i, s.Pos.Offset, s.End.Offset, prev, len(src))
			}
			if got := string(src[s.Pos.Offset:s.End.Offset]); got != s.Text {
				t.Fatalf("statement %d: Text %q, but the source from Pos to End is %q", i, s.Text, got)
			}
			prev = s.End.Offset
		}
	})
}

// TestARefusalIsASyntaxErrorTheFuzzerReads: the fuzz target's position check reads the refusal as
// a parse.SyntaxError; this keeps that read from passing by never matching.
func TestARefusalIsASyntaxErrorTheFuzzerReads(t *testing.T) {
	var se parse.SyntaxError
	if _, err := sql.New().Parse([]byte("SELECT 'unterminated")); !errors.As(err, &se) {
		t.Fatalf("an unterminated string's refusal %v (%T) is not a parse.SyntaxError", err, err)
	}
}
