package widget

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// countingRenderer is a Markdown cell renderer that counts the blocks it lays.
type countingRenderer struct {
	md   *MarkdownCellRenderer
	laid []Block
}

func (c *countingRenderer) Blocks(lines []string, from, to int) []Block {
	return c.md.Blocks(lines, from, to)
}

func (c *countingRenderer) LayOut(b Block, lines []string, width int, measure func(string) int, typing bool, th CellTheme) CellBlock {
	c.laid = append(c.laid, b)
	return c.md.LayOut(b, lines, width, measure, typing, th)
}

func (c *countingRenderer) BlockContext(b Block, lines []string) uint64 {
	return c.md.BlockContext(b, lines)
}

func (c *countingRenderer) InCode(lines []string, ln int) bool  { return c.md.InCode(lines, ln) }
func (c *countingRenderer) InTable(lines []string, ln int) bool { return c.md.InTable(lines, ln) }

const cacheWidth = 40

// cacheEditor is a Rendered Markdown Editor of text, unmounted, through a counting renderer.
func cacheEditor(t *testing.T, text string) (*Editor, *countingRenderer) {
	t.Helper()
	e := NewEditor(WithCore(CoreInitialText(text)))
	c := &countingRenderer{md: &MarkdownCellRenderer{MarkdownBlocks: MarkdownBlocks{HideMetadata: true}}}
	e.SetRenderer(c)
	e.SetView(ViewRendered)
	e.renderedRows(cacheWidth)
	c.laid = nil
	return e, c
}

// relaid lays e again and is the blocks laid for it.
func relaid(e *Editor, c *countingRenderer) []Block {
	c.laid = nil
	e.renderedRows(cacheWidth)
	return c.laid
}

// sameAsFresh fails unless e's layout is the one a new Editor lays for its text: the same rows, cell
// by cell, and the caret and the hit agreeing on every line and row.
func sameAsFresh(t *testing.T, e *Editor, what string) {
	t.Helper()
	got := e.renderedRows(cacheWidth)
	f, _ := cacheEditor(t, strings.Join(e.core.buf.textLines, "\n"))
	if e.cells.typing() {
		f.core.setMode(ModeInsert)
		f.core.SetLine(e.core.buf.ln, 0)
	}
	want := f.renderedRows(cacheWidth)
	if !reflect.DeepEqual(got.rows, want.rows) {
		for i := range max(len(got.rows), len(want.rows)) {
			var g, w CellRow
			if i < len(got.rows) {
				g = got.rows[i]
			}
			if i < len(want.rows) {
				w = want.rows[i]
			}
			if !reflect.DeepEqual(g, w) {
				t.Fatalf("%s: row %d of %d is %s, a fresh layout's %s", what, i, len(got.rows), cacheRowText(g), cacheRowText(w))
			}
		}
	}
	for ln := range e.core.buf.textLines {
		gr, gx := got.caret(ln, 0)
		if wr, wx := want.caret(ln, 0); gr != wr || gx != wx {
			t.Fatalf("%s: caret of line %d (%d, %d), a fresh layout's (%d, %d)", what, ln, gr, gx, wr, wx)
		}
	}
	for ri := range got.rows {
		for x := range 4 {
			gl, gc := got.hit(ri, x)
			if wl, wc := want.hit(ri, x); gl != wl || gc != wc {
				t.Fatalf("%s: hit (%d, %d) is (%d, %d), a fresh layout's (%d, %d)", what, ri, x, gl, gc, wl, wc)
			}
		}
	}
}

// cacheRowText is a row as text, each cell's source after it: "a@3:0".
func cacheRowText(r CellRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[anchor %d]", r.Anchor)
	for _, c := range r.Cells {
		fmt.Fprintf(&b, " %q@%d:%d", c.Text, c.Line, c.Col)
	}
	return b.String()
}

// bigNote is a 5,000-line note: paragraphs, headings, list items and code, in blocks of four lines.
func bigNote() string {
	var b strings.Builder
	for i := range 1250 {
		switch i % 4 {
		case 0:
			fmt.Fprintf(&b, "# Part %d\n\n", i)
		case 1:
			fmt.Fprintf(&b, "- item %d\n\n", i)
		case 2:
			fmt.Fprintf(&b, "    code %d\n\n", i)
		default:
			fmt.Fprintf(&b, "Some *words* of paragraph %d.\n\n", i)
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// On a 5,000-line note, an edit that changes no block's kind lays only its own block again: a word
// typed into a paragraph in the middle, and a line added to it (the blocks after it moved a line
// down), each lay one block; the layout is the one a fresh Editor lays, caret and hit agreeing.
func TestAnEditLaysOnlyItsOwnBlockAgain(t *testing.T) {
	e, c := cacheEditor(t, bigNote())
	if n := len(e.core.buf.textLines); n < 5000 {
		t.Fatalf("the note has %d lines, want 5,000", n)
	}
	ln := 2*1250/2 + 1 // a paragraph's line ("Some *words* of paragraph 627.")
	for !strings.HasPrefix(e.core.buf.textLines[ln], "Some") {
		ln++
	}
	if _, err := e.core.Insert(TextPos{Line: ln, Col: 5}, "more "); err != nil {
		t.Fatal(err)
	}
	if laid := relaid(e, c); len(laid) != 1 || laid[0].From != ln {
		t.Fatalf("a word typed at line %d laid %d blocks again (%v), want its own one", ln, len(laid), laid[:min(len(laid), 4)])
	}
	sameAsFresh(t, e, "a word typed")
	if _, err := e.core.Insert(TextPos{Line: ln, Col: len(e.core.buf.textLines[ln])}, "\nand a second line."); err != nil {
		t.Fatal(err)
	}
	if laid := relaid(e, c); len(laid) != 1 || laid[0] != (Block{From: ln, To: ln + 2}) {
		t.Fatalf("a line added to the paragraph at %d laid %d blocks again (%v), want its own one", ln, len(laid), laid[:min(len(laid), 4)])
	}
	sameAsFresh(t, e, "a line added")
	if laid := relaid(e, c); len(laid) != 0 { // nothing changed: the whole layout kept
		t.Fatalf("no change laid %d blocks", len(laid))
	}
}

// An indented line is code, or not, by what is above it, past the blank line between: "- x" above
// makes it part of the item, "plain" makes it code. Changing only that line lays the indented one
// again, either way.
func TestAnIndentedLinesKindFollowsTheLinesAboveIt(t *testing.T) {
	e, _ := cacheEditor(t, "- x\n\n    code\n\nafter")
	sameAsFresh(t, e, "the item")
	if _, err := e.core.Replace(TextRange{From: TextPos{0, 0}, To: TextPos{0, 3}}, "plain"); err != nil {
		t.Fatal(err)
	}
	sameAsFresh(t, e, "- x made plain: the indented line is code")
	if _, err := e.core.Replace(TextRange{From: TextPos{0, 0}, To: TextPos{0, 5}}, "- x"); err != nil {
		t.Fatal(err)
	}
	sameAsFresh(t, e, "plain made a list item again: the indented line is not code")
}

// A comment closed later in the note hides the text before it: the blocks above the edit are laid
// again; and two edits before one layout, list editing's InCode and InTable asked between them, are
// laid as the text they leave.
func TestACommentClosedLaterLaysTheBlocksAboveAgain(t *testing.T) {
	e, c := cacheEditor(t, "<!-- a note\n\nhidden\n\nstill\n\nend\n\nafter")
	sameAsFresh(t, e, "the comment open")
	if _, err := e.core.Insert(TextPos{Line: 6, Col: 3}, " -->"); err != nil {
		t.Fatal(err)
	}
	sameAsFresh(t, e, "the comment closed")

	if _, err := e.core.Insert(TextPos{Line: 6, Col: 3}, "x"); err != nil { // "endx -->": the comment still closed
		t.Fatal(err)
	}
	c.InCode(e.core.buf.textLines, 8)
	if _, err := e.core.Replace(TextRange{From: TextPos{6, 4}, To: TextPos{6, 8}}, ""); err != nil { // the close removed
		t.Fatal(err)
	}
	c.InTable(e.core.buf.textLines, 8)
	sameAsFresh(t, e, "two edits, the close typed on and removed")
}

// The block typed in is laid as its source, and moving to another block while typing lays both
// again: the one left rendered, the one entered as source.
func TestTypingLaysTheBlockLeftAndTheBlockEntered(t *testing.T) {
	e, c := cacheEditor(t, "# one\n\n**two**\n\nthree")
	e.core.setMode(ModeInsert)
	if laid := relaid(e, c); len(laid) != 1 || laid[0].From != 0 {
		t.Fatalf("Insert on line 0 laid %v, want its block", laid)
	}
	sameAsFresh(t, e, "typing in the heading")
	e.core.SetLine(2, 0)
	if laid := relaid(e, c); len(laid) != 2 || laid[0].From != 0 || laid[1].From != 2 {
		t.Fatalf("moving to line 2 laid %v, want the heading and line 2's block", laid)
	}
	sameAsFresh(t, e, "typing in the second block")
}

// A layout laid before the Editor is mounted measures by the default width policy; mounted in an
// App that draws East Asian ambiguous characters wide, it is laid again by that policy: "±" two
// cells, not the one it was.
func TestAMountedEditorLaysByItsAppsWidthPolicy(t *testing.T) {
	e, _ := cacheEditor(t, "a ± b")
	if w := cellWidthOf(e.renderedRows(cacheWidth), "±"); w != 1 {
		t.Fatalf("unmounted, ± is %d cells, want 1", w)
	}
	tb := tui.NewTestBackend(40, 5)
	app := tui.NewApp(e, tui.WithBackend(tb), tui.WithMinFrameInterval(0), tui.WithWidthPolicy(tui.WidthPolicyAmbiguousWide))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = app.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	got := make(chan int, 1)
	app.Update(func() { got <- cellWidthOf(e.renderedRows(cacheWidth), "±") })
	select {
	case w := <-got:
		if w != 2 {
			t.Fatalf("mounted under the wide policy, ± is %d cells, want 2", w)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not run")
	}
}

// cellWidthOf is the width of the first cell of text in r, -1 for none.
func cellWidthOf(r *renderedLayout, text string) int {
	for _, row := range r.rows {
		for _, c := range row.Cells {
			if c.Text == text {
				return c.W
			}
		}
	}
	return -1
}
