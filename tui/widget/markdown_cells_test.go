package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui/style"
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

// A tab in a code block is blank cells, as many as the editor gives a tab: never a tab character
// on the screen.
func TestMarkdownTabIsBlankCells(t *testing.T) {
	h, _, _ := renderedEditor(t, "```\na\tb\n```", mdRenderer())
	wantRows(t, h, "a    b")
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

// Prose wraps at the width before the word that would cross it: no word is cut. Six-cell words do
// not divide the 40-cell view, so a wrap by cells alone would cut one.
func TestMarkdownProseWrapsAtWords(t *testing.T) {
	h, _, _ := renderedEditor(t, strings.Repeat("words ", 10), mdRenderer())
	got := rows(h, 2)
	for _, r := range got {
		if len(r) > 40 {
			t.Errorf("row %q: wider than the view", r)
		}
		for _, w := range strings.Fields(r) {
			if w != "words" {
				t.Errorf("row %q: a word cut (%q)", r, w)
			}
		}
	}
	if got[1] == "" {
		t.Errorf("60 cells of prose in a 40-cell view drew one row: %q", got)
	}
}

// mdEditorAt is renderedEditor at w×h.
func mdEditorAt(t *testing.T, text string, w, h int) (*harness, *shell, *widget.Editor) {
	t.Helper()
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText(text)))
	sh := newShell(ed)
	hs := startApp(t, sh, w, h)
	hs.onLoop(func() {
		ed.SetRenderer(mdRenderer())
		ed.SetView(widget.ViewRendered)
		hs.app.FocusInto(ed)
	})
	hs.barrier(sh)
	return hs, sh, ed
}

// caretAt puts the cursor on (ln, col) and asks where the Editor draws it.
func caretAt(h *harness, sh *shell, ed *widget.Editor, ln, col int) (x, y int, ok bool) {
	h.onLoop(func() { ed.Core().SetLine(ln, col) })
	h.barrier(sh)
	h.onLoop(func() { x, y, ok = ed.Cursor() })
	return
}

// A bare <br> ends its row, and two in a row leave an empty one, as the window's view breaks
// them; the caret has a place on each row: after a <br>, at the text it breaks to; on the empty
// row, at the <br> that ends it.
func TestMarkdownBareBreaksEndRows(t *testing.T) {
	const line = "before<br>after<br><br>end" // after at 10, the second <br> at 15, the third at 19, end at 23
	h, sh, ed := renderedEditor(t, line, mdRenderer())
	wantRows(t, h, "before", "after", "", "end")
	for _, c := range []struct{ col, x, y int }{{0, 0, 0}, {10, 0, 1}, {19, 0, 2}, {23, 0, 3}} {
		if x, y, ok := caretAt(h, sh, ed, 0, c.col); !ok || x != c.x || y != c.y {
			t.Errorf("caret on column %d at (%d, %d) %v, want (%d, %d)", c.col, x, y, ok, c.x, c.y)
		}
	}
}

// A line's prefix keeps its source: each quote bar is its own '>', a tab in a list item's indent is
// blank cells (never a tab on the screen), a wrapped item hangs under its text, and a prefix wider
// than the view wraps like text, so the item's text still shows.
func TestMarkdownPrefixesKeepTheirSource(t *testing.T) {
	h, sh, ed := renderedEditor(t, "  > > text\n- parent\n\t- child\n\n- "+strings.Repeat("words ", 8), mdRenderer())
	got := rows(h, 6)
	if got[0] != "│ │ text" {
		t.Errorf("the quote's row %q, want \"│ │ text\"", got[0])
	}
	for _, c := range []struct{ col, x int }{{2, 0}, {4, 2}} {
		if x, y, ok := caretAt(h, sh, ed, 0, c.col); !ok || x != c.x || y != 0 {
			t.Errorf("caret on the '>' at column %d: (%d, %d) %v, want x %d on row 0", c.col, x, y, ok, c.x)
		}
	}
	if !strings.HasPrefix(got[2], "    ") || strings.Contains(h.row(2), "\t") || !strings.HasSuffix(got[2], " child") {
		t.Errorf("the nested item's row %q, want its tab as four blank cells, then its bullet and text", h.row(2))
	}
	if x, y, ok := caretAt(h, sh, ed, 2, 1); !ok || x != 4 || y != 2 {
		t.Errorf("caret on the nested item's marker: (%d, %d) %v, want (4, 2)", x, y, ok)
	}
	if !strings.HasPrefix(got[5], "  words") {
		t.Errorf("the wrapped item's second row %q, want it hung under its text", got[5])
	}
	for _, r := range got[4:6] {
		for _, w := range strings.Fields(strings.TrimPrefix(r, "•")) {
			if w != "words" {
				t.Errorf("row %q: a word cut (%q)", r, w)
			}
		}
	}

	narrow, nsh, ned := mdEditorAt(t, "   - x", 3, 6)
	if !strings.Contains(narrow.grid(), "x") {
		t.Errorf("an item whose prefix is wider than the view hid its text:\n%s", narrow.grid())
	}
	// its indent fills the first row, so its marker (column 3) starts the next, inside the room
	if x, y, ok := caretAt(narrow, nsh, ned, 0, 3); !ok || x != 0 || y != 1 {
		t.Errorf("caret on the marker of an item whose prefix is wider than the view: (%d, %d) %v, want (0, 1)\n%s", x, y, ok, narrow.grid())
	}
}

// A fenced block's info string is its own text: on a "~~~ ~" fence, the "~" after the fence.
func TestMarkdownFenceInfoIsItsOwnText(t *testing.T) {
	h, sh, ed := renderedEditor(t, "~~~ ~\nx\n~~~", mdRenderer())
	wantRows(t, h, "~", "x")
	if x, y, ok := caretAt(h, sh, ed, 0, 4); !ok || x != 0 || y != 0 {
		t.Errorf("caret on the info string: (%d, %d) %v, want (0, 0)", x, y, ok)
	}
}

// A fenced block is highlighted in its info string's language from Languages, its highlighter's
// state carried from line to line (a comment over two lines), the code look where no span is.
func TestMarkdownCodeIsHighlightedInItsLanguage(t *testing.T) {
	toy := highlight.HighlighterFunc(func(line string, prev highlight.State) ([]highlight.Span, highlight.State) {
		if prev == 1 || strings.HasPrefix(line, "/*") { // a comment, until a line holding */
			next := highlight.State(1)
			if strings.Contains(line, "*/") {
				next = 0
			}
			return []highlight.Span{{Start: 0, End: len(line), Style: highlight.Comment}}, next
		}
		if i := strings.Index(line, "kw"); i >= 0 {
			return []highlight.Span{{Start: i, End: i + 2, Style: highlight.Keyword}}, 0
		}
		return nil, 0
	})
	r := &widget.MarkdownCellRenderer{Languages: highlight.NewRepository(highlight.Definition{Name: "toy", Highlighter: toy}).Snapshot()}
	red, green, blue := style.RGB(200, 0, 0), style.RGB(0, 200, 0), style.RGB(0, 0, 200)
	var th widget.CellTheme
	th.Syntax[highlight.Keyword] = style.New().Foreground(red)
	th.Syntax[highlight.Comment] = style.New().Foreground(green)
	th.Syntax[widget.MarkdownCodeStyle] = style.New().Foreground(blue)
	lines := []string{"```toy", "a kw", "/* x", "y */", "z", "```"}
	cb := r.LayOut(widget.Block{From: 0, To: len(lines)}, lines, 40, func(string) int { return 1 }, false, th)
	fg := func(row, cell int) style.Color {
		if row >= len(cb.Rows) || cell >= len(cb.Rows[row].Cells) {
			t.Fatalf("no cell %d on row %d of %d rows", cell, row, len(cb.Rows))
		}
		c, _ := cb.Rows[row].Cells[cell].Style.GetForeground()
		return c
	}
	for _, c := range []struct {
		row, cell int
		want      style.Color
		what      string
	}{
		{1, 0, blue, "plain code"}, {1, 2, red, "a keyword"}, {2, 0, green, "a comment's first line"},
		{3, 0, green, "a comment's second line (the state carried)"}, {4, 0, blue, "code after the comment"},
	} {
		if got := fg(c.row, c.cell); got != c.want {
			t.Errorf("%s (row %d, cell %d): %v, want %v", c.what, c.row, c.cell, got, c.want)
		}
	}
}

// A table is boxed: its header row, the rule its delimiter row stands for, its body rows, each cell
// aligned as the delimiter says, with its Markdown drawn. Every cell keeps its source: the caret on
// a cell's text sits on it, and on the delimiter row, on its rule.
func TestMarkdownTablesAreBoxed(t *testing.T) {
	h, sh, ed := renderedEditor(t, "| a | **b** |\n|:--|--:|\n| ccc | ddd |\nee | f", mdRenderer())
	wantRows(t, h,
		"┌─────┬─────┐",
		"│ a   │   b │",
		"├─────┼─────┤",
		"│ ccc │ ddd │",
		"│ ee  │   f │",
		"└─────┴─────┘")
	// the last row has no outer pipes: its columns are its own, not the pipe GFM reads before it
	for _, c := range []struct{ ln, col, x, y int }{{0, 8, 10, 1}, {1, 0, 0, 2}, {2, 2, 2, 3}, {2, 8, 8, 3}, {3, 1, 3, 4}, {3, 5, 10, 4}} {
		if x, y, ok := caretAt(h, sh, ed, c.ln, c.col); !ok || x != c.x || y != c.y {
			t.Errorf("caret on (%d, %d) at (%d, %d) %v, want (%d, %d)", c.ln, c.col, x, y, ok, c.x, c.y)
		}
	}
}

// A table wider than the view narrows its columns to fit (FitColumns), its cells' text wrapped in
// them; a bare <br> breaks a cell's text.
func TestMarkdownTableColumnsNarrowToTheView(t *testing.T) {
	long := "alpha beta gamma delta epsilon zeta"
	h, _, _ := renderedEditor(t, "| "+long+" | "+long+" |\n|---|---|\n| x<br>y | z |", mdRenderer())
	got := rows(h, 10)
	if !strings.HasSuffix(got[0], "┐") || len([]rune(got[0])) > 40 {
		t.Errorf("the table's top %q: want it closed inside the 40-cell view", got[0])
	}
	if !strings.Contains(h.grid(), "zeta") {
		t.Errorf("a narrowed cell lost its text:\n%s", h.grid())
	}
	at := -1
	for y, r := range got {
		if strings.HasPrefix(r, "│ x ") {
			at = y
		}
	}
	if at < 0 || !strings.HasPrefix(got[at+1], "│ y ") {
		t.Errorf("x<br>y: want x and y on two rows of their cell:\n%s", h.grid())
	}
}

// A table whose columns can't narrow to fit is laid at its natural widths: the block is wider than
// the width.
func TestATableTooWideToNarrowKeepsItsWidth(t *testing.T) {
	row := "|" + strings.Repeat(" abcde |", 9)
	lines := []string{row, "|" + strings.Repeat("---|", 9), row}
	cb := (&widget.MarkdownCellRenderer{}).LayOut(widget.Block{From: 0, To: len(lines)}, lines, 40, func(string) int { return 1 }, false, widget.CellTheme{})
	if want := 4 + 3*8 + 5*9; cb.Width != want {
		t.Errorf("a nine-column table in 40 cells: Width %d, want its natural %d", cb.Width, want)
	}
}

// A table's top rule shows with its header at the view's top: from the start, and when the caret
// comes back up to the header from below the view.
func TestATablesTopRuleShowsWithItsHeader(t *testing.T) {
	h, sh, ed := renderedEditor(t, "| a | b |\n|---|---|\n| c | d |"+strings.Repeat("\nline", 20), mdRenderer())
	top := func() string { // the text area's first row: the last column is the scroll indicator's
		return strings.TrimRight(string([]rune(h.row(0))[:39]), " ")
	}
	if got := top(); got != "┌───┬───┐" {
		t.Errorf("the view's first row %q, want the table's top rule", got)
	}
	caretAt(h, sh, ed, 22, 0)
	if x, y, ok := caretAt(h, sh, ed, 0, 2); !ok || x != 2 || y != 1 {
		t.Errorf("caret back on the header at (%d, %d) %v, want (2, 1) under the top rule:\n%s", x, y, ok, h.grid())
	}
	if got := top(); got != "┌───┬───┐" {
		t.Errorf("back on the header, the view's first row %q, want the table's top rule", got)
	}

	// a heading right after a table starts the view itself: the table's bottom rule is not its
	h, sh, ed = renderedEditor(t, "| a | b |\n|---|---|\n| c | d |\n# H"+strings.Repeat("\nline", 20), mdRenderer())
	caretAt(h, sh, ed, 23, 0)
	if x, y, ok := caretAt(h, sh, ed, 3, 2); !ok || x != 0 || y != 0 {
		t.Errorf("caret back on the heading at (%d, %d) %v, want (0, 0), the view's top:\n%s", x, y, ok, h.grid())
	}
	if got := top(); got != "H" {
		t.Errorf("back on the heading, the view's first row %q, want H", got)
	}
}
