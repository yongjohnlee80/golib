package widget

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
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
	var st SyntaxStyles // code, marks and emphasis each look their own, so a block's kind shows in its cells
	st[MarkdownCodeStyle] = style.New().Bold(true)
	st[MarkdownMarkerStyle] = style.New().Underline(true)
	st[MarkdownEmphStyle] = style.New().Italic(true)
	e.core.SetSyntaxStyles(st)
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

// bigNote is a note of over 5,000 lines: headings, list items, code, paragraphs and tables, each
// with a blank line after it.
func bigNote() string {
	var b strings.Builder
	for i := range 2500 {
		if i%10 == 9 {
			fmt.Fprintf(&b, "| a | b |\n| - | - |\n| %d | x |\n\n", i)
			continue
		}
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
// typed into a paragraph's line in the middle lays that line's block, and a line added after it (a
// prose line is a block of its own) lays only the new line's, the blocks after it (tables among
// them) moved a line down; the layout is the one a fresh Editor lays, caret and hit agreeing.
func TestAnEditLaysOnlyItsOwnBlockAgain(t *testing.T) {
	e, c := cacheEditor(t, bigNote())
	if n := len(e.core.buf.textLines); n < 5000 {
		t.Fatalf("the note has %d lines, want 5,000", n)
	}
	ln := len(e.core.buf.textLines) / 2 // about the middle: the next paragraph's line
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
	if laid := relaid(e, c); len(laid) != 1 || laid[0] != (Block{From: ln + 1, To: ln + 2}) {
		t.Fatalf("a line added after line %d laid %d blocks again (%v), want only the new line's", ln, len(laid), laid[:min(len(laid), 4)])
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

// A comment begun inside a line and closed lines later hides the text after its start: the line it
// begins on keeps its block, laid again under its new mask (its bounds the same, its text too).
func TestACommentClosedLaterLaysTheLineItBeganOnAgain(t *testing.T) {
	e, c := cacheEditor(t, "a <!-- b\nc\nd\n\nafter")
	sameAsFresh(t, e, "the comment open")
	before := e.renderedRows(cacheWidth).blocks
	if _, err := e.core.Insert(TextPos{Line: 2, Col: 1}, " -->"); err != nil {
		t.Fatal(err)
	}
	laid := relaid(e, c)
	sameAsFresh(t, e, "the comment closed")
	after := e.renderedRows(cacheWidth).blocks
	if before[0] != (Block{From: 0, To: 1}) || after[0] != before[0] || !slices.Contains(laid, before[0]) {
		t.Fatalf("the line the comment begins on: blocks %v then %v, laid %v; want [0, 1) both times, laid again", before[:1], after[:1], laid)
	}
}

// Two edits before one layout, list editing's InCode and InTable asked after each (a Batch: no
// layout until it ends): a comment's close typed, which hides text on the line it began on, then a
// word on a later line, whose Prepare says nothing earlier changed. The line the comment began on is
// laid again under its new mask, as a fresh layout lays it; and the inverse, the close taken off.
func TestTwoEditsBeforeOneLayoutAreLaidAsTheTextTheyLeave(t *testing.T) {
	e, c := cacheEditor(t, "a <!-- b\nc\nd\n\nafter")
	for _, step := range []struct {
		what  string
		close func() error
	}{
		{"the close typed", func() error { _, err := e.core.Insert(TextPos{Line: 2, Col: 1}, " -->"); return err }},
		{"the close taken off", func() error {
			return e.core.Delete(TextRange{From: TextPos{Line: 2, Col: 1}, To: TextPos{Line: 2, Col: 5}})
		}},
	} {
		e.renderedRows(cacheWidth) // the layout the batch's is laid from
		c.laid = nil
		err := e.core.Batch(func() error {
			if err := step.close(); err != nil {
				return err
			}
			c.InCode(e.core.buf.textLines, 4)
			if _, err := e.core.Insert(TextPos{Line: 4, Col: 5}, "!"); err != nil { // "after!": no comment's
				return err
			}
			c.InTable(e.core.buf.textLines, 4)
			if len(c.laid) != 0 {
				t.Errorf("%s: laid %v between the edits, want no layout until the batch ends", step.what, c.laid)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if laid := relaid(e, c); !slices.Contains(laid, Block{From: 0, To: 1}) {
			t.Fatalf("%s, then a later word: laid %v, want the line the comment begins on, [0, 1), again", step.what, laid)
		}
		sameAsFresh(t, e, step.what+", then a later word")
	}
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
