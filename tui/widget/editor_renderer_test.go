package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// starRenderer is a small renderer for the Editor's own Rendered view: a block a line; `**` hidden;
// a heading's `# ` hidden; an empty line a row of its own (its Anchor); a `<!--` line no row at all
// (hidden metadata, so a fold). While text is typed in a line it is laid as its source.
type starRenderer struct{}

func (starRenderer) Blocks(lines []string, from, to int) []widget.Block {
	var out []widget.Block
	for i := from; i < to; i++ {
		out = append(out, widget.Block{From: i, To: i + 1})
	}
	return out
}

func (starRenderer) LayOut(b widget.Block, lines []string, _ int, measure func(string) int, typing bool, th widget.CellTheme) widget.CellBlock {
	ln := b.From
	line := lines[ln]
	if !typing && strings.HasPrefix(line, "<!--") {
		return widget.CellBlock{}
	}
	row := widget.CellRow{Anchor: -1}
	col := 0
	for c := range tui.Graphemes(line) {
		hidden := !typing && (c == "*" || (strings.HasPrefix(line, "# ") && col < 2))
		if !hidden {
			row.Cells = append(row.Cells, widget.RowCell{Text: c, W: measure(c), Style: th.Text, Line: ln, Col: col})
		}
		col++
	}
	if len(row.Cells) == 0 {
		row.Anchor = ln
	}
	return widget.CellBlock{Rows: []widget.CellRow{row}}
}

// structuredStar is starRenderer that knows its document's code and tables (none): a Markdown
// renderer's shape, so the Editor turns list editing on in its Rendered view.
type structuredStar struct{ starRenderer }

func (structuredStar) InCode([]string, int) bool  { return false }
func (structuredStar) InTable([]string, int) bool { return false }

const starText = "# Title\n**bold** word\n\n<!-- note -->\nlast"

// renderedEditor is a terminal Editor holding text, drawn Rendered through r, focused, at 40×10.
func renderedEditor(t *testing.T, text string, r widget.CellRenderer) (*harness, *shell, *widget.Editor) {
	t.Helper()
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText(text)))
	sh := newShell(ed)
	h := startApp(t, sh, 40, 10)
	h.onLoop(func() {
		ed.SetRenderer(r)
		ed.SetView(widget.ViewRendered)
		h.app.FocusInto(ed)
	})
	h.barrier(sh)
	return h, sh, ed
}

// The Editor draws its Rendered view through its own renderer, with no document view: the heading
// without its mark, the bold without its stars, an empty line as its own row, a hidden line as no
// row at all. Raw again draws the text as it is.
func TestTheEditorDrawsThroughItsRenderer(t *testing.T) {
	h, sh, ed := renderedEditor(t, starText, starRenderer{})
	var shown widget.EditorView
	h.onLoop(func() { shown = ed.ShownView() })
	if shown != widget.ViewRendered {
		t.Fatalf("drawn %v, want Rendered through the renderer", shown)
	}
	for y, want := range []string{"Title", "bold word", "", "last"} {
		if got := strings.TrimRight(h.row(y), " "); got != want {
			t.Errorf("row %d = %q, want %q", y, got, want)
		}
	}
	h.onLoop(func() { ed.SetView(widget.ViewRaw) })
	h.barrier(sh)
	if got := strings.TrimRight(h.row(1), " "); got != "**bold** word" {
		t.Errorf("Raw row 1 = %q, want the text", got)
	}
}

// The caret sits where the source is drawn: on a hidden star, on the next shown cell; j from the
// empty line steps over the hidden line (a fold) to the last.
func TestTheRenderedCaretFollowsTheSource(t *testing.T) {
	h, sh, ed := renderedEditor(t, starText, starRenderer{})
	cursor := func() (x, y int, ok bool) {
		h.onLoop(func() { x, y, ok = ed.Cursor() })
		return
	}
	h.onLoop(func() { ed.Core().SetLine(1, 0) }) // on the first '*'
	h.barrier(sh)
	if x, y, ok := cursor(); !ok || x != 0 || y != 1 {
		t.Errorf("caret on a hidden star at (%d, %d) %v, want the next shown cell, (0, 1)", x, y, ok)
	}
	h.onLoop(func() { ed.Core().SetLine(1, 5) }) // 'd' (col 5): the cells b o l d at 0..3
	h.barrier(sh)
	if x, _, _ := cursor(); x != 3 {
		t.Errorf("caret on 'd' at x %d, want 3", x)
	}
	h.onLoop(func() { ed.Core().SetLine(1, 7) }) // the closing '*' (col 7), hidden: the space after it, x 4
	h.barrier(sh)
	if x, _, _ := cursor(); x != 4 {
		t.Errorf("caret on a closing hidden star at x %d, want the space after it, 4", x)
	}
	h.onLoop(func() { ed.Core().SetLine(2, 0) })
	h.inject(keyText("j"))
	h.barrier(sh)
	if ln, _ := ed.Core().Line(); ln != 4 {
		t.Errorf("j from the empty line landed on line %d, want 4 (over the hidden line)", ln)
	}
}

// Typing reveals the source of the line typed in, and only it; Esc renders it again.
func TestTypingRevealsTheLinesSource(t *testing.T) {
	h, sh, ed := renderedEditor(t, starText, starRenderer{})
	h.onLoop(func() { ed.Core().SetLine(1, 2) })
	h.inject(keyText("i"))
	h.barrier(sh)
	if got := strings.TrimRight(h.row(1), " "); got != "**bold** word" {
		t.Errorf("typing: row 1 = %q, want its source", got)
	}
	if got := strings.TrimRight(h.row(0), " "); got != "Title" {
		t.Errorf("typing: row 0 = %q, want still rendered", got)
	}
	h.inject(dtEsc)
	h.barrier(sh)
	if got := strings.TrimRight(h.row(1), " "); got != "bold word" {
		t.Errorf("after Esc: row 1 = %q, want rendered again", got)
	}
}

// A structured renderer turns list editing on in the Rendered view: Enter on an item continues it.
// Replacing it with a generic renderer turns list editing off at once.
func TestAStructuredRendererEditsLists(t *testing.T) {
	h, sh, ed := renderedEditor(t, "- a", structuredStar{})
	if on := ed.Core().ListEditing(); !on {
		t.Fatal("list editing is off under a structured renderer")
	}
	h.onLoop(func() { ed.Core().SetLine(0, 3) })
	h.inject(keyText("A"), dtEnter, keyText("b"))
	h.barrier(sh)
	if v := ed.Core().Value(); v != "- a\n- b" {
		t.Errorf("Enter on an item: %q, want it continued", v)
	}
	h.onLoop(func() { ed.SetRenderer(starRenderer{}) })
	h.barrier(sh)
	if ed.Core().ListEditing() {
		t.Error("list editing stayed on with a generic renderer")
	}
}

// With a renderer and no document view, Ctrl+T (the core's Rendered toggle) flips between the text
// and the renderer's view, as it does for a document view.
func TestCtrlTFlipsToTheRenderersView(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText(starText)))
	sh := newShell(ed)
	h := startApp(t, sh, 40, 10)
	h.onLoop(func() {
		ed.SetRenderer(starRenderer{})
		h.app.FocusInto(ed)
	})
	h.barrier(sh)
	shown := func() (v widget.EditorView) { h.onLoop(func() { v = ed.ShownView() }); return }
	if v := shown(); v != widget.ViewRaw {
		t.Fatalf("before Ctrl+T: %v, want Raw (the choice starts Raw)", v)
	}
	h.inject(ctrlEv('t'))
	h.barrier(sh)
	if v := shown(); v != widget.ViewRendered {
		t.Fatalf("Ctrl+T: %v, want Rendered through the renderer", v)
	}
	h.inject(ctrlEv('t'))
	h.barrier(sh)
	if v := shown(); v != widget.ViewRaw {
		t.Errorf("Ctrl+T again: %v, want Raw", v)
	}
}

// tagRenderer is starRenderer with a slice in it: a value no == compares. The Editor names its
// renderer by its own count, never by comparing the value, so drawing it again doesn't panic.
type tagRenderer struct {
	starRenderer
	tags []string
}

func TestAnUncomparableRendererDrawsAgain(t *testing.T) {
	h, sh, ed := renderedEditor(t, starText, tagRenderer{tags: []string{"x"}})
	for range 3 {
		h.onLoop(func() { ed.Core().SetLine(4, 0); ed.MarkDirty() })
		h.barrier(sh)
	}
	if got := strings.TrimRight(h.row(0), " "); got != "Title" {
		t.Errorf("row 0 = %q, want the rendered heading", got)
	}
}

// tallRenderer lays each cluster of a line on a row of its own: one line taller than the view.
type tallRenderer struct{ starRenderer }

func (tallRenderer) LayOut(b widget.Block, lines []string, _ int, measure func(string) int, _ bool, th widget.CellTheme) widget.CellBlock {
	var cb widget.CellBlock
	col := 0
	for c := range tui.Graphemes(lines[b.From]) {
		cb.Rows = append(cb.Rows, widget.CellRow{Anchor: -1, Cells: []widget.RowCell{{Text: c, W: measure(c), Style: th.Text, Line: b.From, Col: col}}})
		col++
	}
	if len(cb.Rows) == 0 {
		cb.Rows = []widget.CellRow{{Anchor: b.From}}
	}
	return cb
}

// A line laid taller than the view scrolls by rows: the caret on its last cluster is revealed on
// the view's last row, and back on its first, on the first.
func TestATallLinesLaterRowsAreRevealed(t *testing.T) {
	h, sh, ed := renderedEditor(t, "abcdefghijklmnop", tallRenderer{})
	cursor := func() (x, y int, ok bool) { h.onLoop(func() { x, y, ok = ed.Cursor() }); return }
	h.onLoop(func() { ed.Core().SetLine(0, 15) })
	h.barrier(sh)
	if _, y, ok := cursor(); !ok || y != 9 {
		t.Fatalf("caret on the 16th row: y %d, shown %v; want the view's last row, 9", y, ok)
	}
	if got := strings.TrimRight(h.row(9), " "); got != "p" {
		t.Errorf("the view's last row %q, want p", got)
	}
	h.onLoop(func() { ed.Core().SetLine(0, 0) })
	h.barrier(sh)
	if _, y, ok := cursor(); !ok || y != 0 {
		t.Errorf("caret back on the first row: y %d, shown %v; want 0", y, ok)
	}
	if got := strings.TrimRight(h.row(0), " "); got != "a" {
		t.Errorf("the view's first row %q, want a", got)
	}
}
