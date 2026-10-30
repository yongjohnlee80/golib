package exporter

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTheFormulaGuardQuotesWhatASpreadsheetWouldRun: with WithFormulaGuard a cell starting with
// =, +, -, @, a tab or a carriage return is written behind a quote; other cells, and every cell
// without the option, are as they were.
func TestTheFormulaGuardQuotesWhatASpreadsheetWouldRun(t *testing.T) {
	for _, guard := range []bool{true, false} {
		sink := newMemSink()
		opts := []Option{WithBatchSize(100), WithOpener(sink.open)}
		if guard {
			opts = append(opts, WithFormulaGuard())
		}
		c := NewCSV[csvRow]("g", opts...)
		if err := c.Commit(t.Context(), csvRow{"=HYPERLINK(\"x\")", 1, ""}, csvRow{"@SUM(A1)", 2, ""}, csvRow{"plain", 3, ""}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Flush(t.Context()); err != nil {
			t.Fatal(err)
		}
		got := sink.content(sink.names()[0])
		want := "Name,Count\n\"'=HYPERLINK(\"\"x\"\")\",1\n'@SUM(A1),2\nplain,3\n"
		if !guard {
			want = "Name,Count\n\"=HYPERLINK(\"\"x\"\")\",1\n@SUM(A1),2\nplain,3\n"
		}
		if got != want {
			t.Errorf("guard %v:\n got %q\nwant %q", guard, got, want)
		}
	}
}

// TestABatchFileIsNeverWrittenThrough: a batch file is created new; a name already there (a file,
// or a symlink to another file) is refused, and the file it names is not touched.
func TestABatchFileIsNeverWrittenThrough(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, "planted.csv")); err != nil {
		t.Fatal(err)
	}
	c := newConfig([]Option{WithDir(dir)})
	if w, err := c.open("planted.csv"); err == nil {
		_ = w.Close()
		t.Error("a planted symlink was opened")
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Errorf("the file the symlink named holds %q", b)
	}
	w, err := c.open("new.csv")
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
}
