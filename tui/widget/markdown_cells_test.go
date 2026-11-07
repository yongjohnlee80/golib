package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

func mdRenderer() *widget.MarkdownCellRenderer {
	return &widget.MarkdownCellRenderer{MarkdownBlocks: widget.MarkdownBlocks{HideMetadata: true}}
}

// rows are the first n rows drawn, right-trimmed.
func rows(h *harness, n int) []string {
	out := make([]string, n)
	for y := range n {
		out[y] = strings.TrimRight(h.row(y), " ")
	}
	return out
}

func wantRows(t *testing.T, h *harness, want ...string) {
	t.Helper()
	got := rows(h, len(want))
	for y := range want {
		if got[y] != want[y] {
			t.Errorf("row %d = %q, want %q (rows %q)", y, got[y], want[y], got)
		}
	}
}

// The terminal editor's Markdown, Rendered: a heading without its #, strong, emphasis, a code span
// and a link without their marks, a list item's marker as its bullet, a quote's mark as a bar.
func TestMarkdownIsDrawnInCells(t *testing.T) {
	h, _, _ := renderedEditor(t, "# Title\n**bold** and *em* `code` [link](u)\n- item\n> quote", mdRenderer())
	wantRows(t, h, "Title", "bold and em code link", "• item", "│ quote")
}

// What a reading view hides takes no row: closed frontmatter, and a comment's lines; a fenced code
// block is its info string and its lines, its fences no rows.
func TestMarkdownHidesWhatTheReaderSkips(t *testing.T) {
	h, _, _ := renderedEditor(t, "---\ntitle: x\n---\nbody\n<!-- a comment -->\nafter\n```go\nfmt.Println()\n```\nend", mdRenderer())
	wantRows(t, h, "body", "after", "go", "fmt.Println()", "end")
}

// The caret on a hidden mark sits on the next shown cell; typing reveals the line's source, and Esc
// renders it again; Enter on a list item continues the list.
func TestMarkdownCellsAreEditable(t *testing.T) {
	h, sh, ed := renderedEditor(t, "**bold** word\n- a", mdRenderer())
	h.onLoop(func() { ed.Core().SetLine(0, 0) })
	h.barrier(sh)
	var x, y int
	var ok bool
	h.onLoop(func() { x, y, ok = ed.Cursor() })
	if !ok || x != 0 || y != 0 {
		t.Errorf("caret on the hidden ** at (%d, %d) %v, want the 'b' at (0, 0)", x, y, ok)
	}
	h.inject(keyText("i"))
	h.barrier(sh)
	wantRows(t, h, "**bold** word")
	h.inject(dtEsc)
	h.barrier(sh)
	wantRows(t, h, "bold word")
	h.onLoop(func() { ed.Core().SetLine(1, 2) })
	h.inject(keyText("A"), dtEnter, keyText("b"), dtEsc)
	h.barrier(sh)
	if v := ed.Core().Value(); v != "**bold** word\n- a\n- b" {
		t.Errorf("Enter on an item: %q, want the list continued", v)
	}
}

// Prose wraps at the width before the word that would cross it: no word is cut.
func TestMarkdownProseWrapsAtWords(t *testing.T) {
	h, _, _ := renderedEditor(t, strings.Repeat("word ", 12), mdRenderer())
	for _, r := range rows(h, 2) {
		if strings.HasPrefix(r, "ord") || strings.HasSuffix(r, " w") || len(r) > 40 {
			t.Errorf("row %q: cut inside a word, or wider than the view", r)
		}
	}
	if got := rows(h, 2); got[1] == "" {
		t.Errorf("60 cells of prose in a 40-cell view drew one row: %q", got)
	}
}
