package sql_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/sql"
)

// goldenDir holds the corpus and the golden: the colours highlight/sql gave each line before its
// highlighter moved here, captured once from the code as it was. Nothing regenerates it.
const goldenDir = "testdata/highlight"

type goldenLine struct {
	Spans []highlight.Span `json:"spans"`
	State highlight.State  `json:"state"`
}

var goldenCorpus = []struct {
	file    string
	dialect sql.Dialect
}{
	{"postgresql.sql", sql.PostgreSQLDialect},
	{"mysql.sql", sql.MySQLDialect},
	{"sqlite.sql", sql.SQLiteDialect},
}

// highlightCorpus colours each corpus file line by line with a fresh highlighter, carrying the
// state from line to line as an editor does.
func highlightCorpus(t *testing.T) map[string][]goldenLine {
	t.Helper()
	out := map[string][]goldenLine{}
	for _, c := range goldenCorpus {
		src, err := os.ReadFile(filepath.Join(goldenDir, c.file))
		if err != nil {
			t.Fatal(err)
		}
		h := sql.Highlighter(c.dialect)
		var state highlight.State
		for _, ln := range strings.Split(strings.TrimSuffix(string(src), "\n"), "\n") {
			spans, next := h.HighlightBlock(ln, state)
			out[c.file] = append(out[c.file], goldenLine{Spans: spans, State: next})
			state = next
		}
	}
	return out
}

func TestTheMovedHighlightingMatchesTheGolden(t *testing.T) {
	got := highlightCorpus(t)
	path := filepath.Join(goldenDir, "golden.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string][]goldenLine
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	for file, lines := range want {
		if !reflect.DeepEqual(got[file], lines) {
			for i := range lines {
				if i >= len(got[file]) || !reflect.DeepEqual(got[file][i], lines[i]) {
					t.Errorf("%s line %d: colours differ from the golden", file, i+1)
				}
			}
		}
	}
	if len(got) != len(want) {
		t.Errorf("%d corpus files, golden has %d", len(got), len(want))
	}
}
