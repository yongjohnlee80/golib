package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// readyDiagrams is a Rendered editor over text whose diagrams are drawn at once.
func readyDiagrams(t *testing.T, text string, opts ...MarkdownOption) *edHarness {
	t.Helper()
	f := &fakeDiagrams{done: map[string]bool{"A-->B": true}}
	h := startEditor(t, 50, 14, WithRenderer(NewMarkdownRenderer(append([]MarkdownOption{WithDiagrams(f)}, opts...)...)),
		WithMode(Rendered), WithCore(tuiwidget.CoreInitialText(text)))
	h.paint()
	return h
}

// foldEdge is the top edge of line's fold's outline, in the view, as Paint draws it.
func (h *edHarness) foldEdge(line int) (r gui.Rect, ok bool) {
	h.onLoop(func() {
		l := h.e.layout
		var fr gui.Rect
		if fr, _, ok = l.foldRect(line); ok {
			r = gui.Rect{X: fr.X + padX + l.gutter, Y: fr.Y + padY - l.scroll, W: fr.W, H: 2}
		}
	})
	return r, ok
}

// drawn reports a FillRect at r in rc.
func drawn(rc *gui.RecordingCanvas, r gui.Rect) bool {
	for _, c := range rc.Calls {
		if c.Op == "FillRect" && c.Rect == r {
			return true
		}
	}
	return false
}

// shown is the cursor as the last paint drew it: the caret, or the fold's outline (or bar) at line.
func (h *edHarness) shown(line int) (caret, fold bool) {
	h.t.Helper()
	rc := h.paint()
	h.onLoop(func() { caret = h.e.body.caretOn })
	if r, ok := h.foldEdge(line); ok {
		fold = drawn(rc, r)
	}
	return caret, fold
}

func (h *edHarness) at(what string, ln, col int) {
	h.t.Helper()
	if l, c := h.line(); l != ln || c != col {
		h.t.Errorf("%s: cursor (%d,%d), want (%d,%d)", what, l, c, ln, col)
	}
}

// In Normal mode the cursor on a diagram is drawn as the diagram's outline: j lands on the
// fence's first line, the caret gives way to the outline, and j again leaves it in one row.
func TestTheCursorIsDrawnOnADiagram(t *testing.T) {
	h := readyDiagrams(t, diagramDoc) // top, the fence (1-3) drawn as a picture, end
	h.keys(key('j'))
	h.barrier()
	h.at("j onto the diagram", 1, 0)
	if caret, fold := h.shown(1); caret || !fold {
		t.Errorf("on the diagram: caret %v, outline %v; want the outline alone", caret, fold)
	}
	h.keys(key('j'))
	h.barrier()
	h.at("j past it", 4, 0)
	if caret, _ := h.shown(4); !caret {
		t.Error("past the diagram the caret is not drawn")
	}
}

// A diagram that arrives under the cursor folds there: the cursor stays where it was, its outline
// is drawn, and one j leaves the block.
func TestADiagramReadyUnderTheCursor(t *testing.T) {
	h, f := startDiagrams(t)
	h.onLoop(func() { h.e.core.SetLine(2, 0) })
	if caret, _ := h.shown(2); !caret {
		t.Fatal("on the pending diagram's code the caret is not drawn")
	}
	f.finish("A-->B")
	h.until("the ready picture", func() bool { h.paint(); return h.picture(1) != nil })
	h.at("not moved", 2, 0)
	if caret, fold := h.shown(2); caret || !fold {
		t.Errorf("under the picture: caret %v, outline %v; want the outline alone", caret, fold)
	}
	h.keys(key('j'))
	h.barrier()
	h.at("one j leaves it", 4, 0)
}

// A move onto a diagram below the view scrolls it in: Reveal goes to the fold's rectangle. The
// blocks above are laid out first: a block not yet laid is at an estimated height, for the caret's
// Reveal as for a fold's.
func TestRevealScrollsToADiagram(t *testing.T) {
	h := readyDiagrams(t, strings.Repeat("line\n\n", 20)+diagramDoc) // the fence at lines 41-43
	h.onLoop(func() {
		for i := range h.e.layout.blocks {
			h.e.layout.lay(i, nil)
		}
	})
	h.keys(key('4'), key('2'), key('G'))
	h.barrier()
	h.at("42G", 41, 0)
	h.paint()
	var in bool
	h.onLoop(func() {
		l := h.e.layout
		r, _, ok := l.foldRect(41)
		in = ok && r.Y >= l.scroll && r.Y < l.scroll+l.height-2*padY
	})
	if !in {
		t.Error("the diagram the cursor is on is not in view")
	}
}

// Hidden metadata is no row: k under it stays; gg reaches it and draws a bar at its place; j
// leaves it.
func TestHiddenMetadataIsABar(t *testing.T) {
	h := startEditor(t, 50, 14, WithRenderer(NewMarkdownRenderer(HideMetadata())), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText("---\ntitle: x\n---\nbody\nmore")))
	h.onLoop(func() { h.e.core.SetLine(3, 0) })
	h.keys(key('k'))
	h.barrier()
	h.at("k under hidden metadata", 3, 0)
	h.keys(key('g'), key('g'))
	h.barrier()
	h.at("gg", 0, 0)
	if caret, bar := h.shown(0); caret || !bar {
		t.Errorf("in hidden metadata: caret %v, bar %v; want the bar alone", caret, bar)
	}
	h.keys(key('j'))
	h.barrier()
	h.at("j leaves it", 3, 0)
}

// Insert opens the fold: the block is laid raw, the caret drawn at its column; Esc folds it again.
// Moving into a diagram while typing goes into its source at the sticky column, as into any line.
func TestInsertOpensADiagram(t *testing.T) {
	h := readyDiagrams(t, diagramDoc)
	h.keys(key('l'), key('l'), key('i'), tui.KeyEvent{Code: tui.KeyDown})
	h.barrier()
	h.at("Insert, down into the diagram", 1, 2)
	h.keys(tui.KeyEvent{Code: tui.KeyEscape})
	h.barrier()
	h.onLoop(func() { h.e.core.SetLine(0, 0) })
	h.keys(key('j'), key('i'))
	h.barrier()
	if caret, fold := h.shown(1); !caret || fold {
		t.Errorf("in Insert: caret %v, outline %v; want the caret alone", caret, fold)
	}
	h.keys(tui.KeyEvent{Code: tui.KeyEscape})
	h.barrier()
	if caret, fold := h.shown(1); caret || !fold {
		t.Errorf("Normal again: caret %v, outline %v; want the outline alone", caret, fold)
	}
}

// A Visual selection over a diagram tints it, and the gutter numbers it with its first line.
func TestASelectedDiagramIsTinted(t *testing.T) {
	h := readyDiagrams(t, diagramDoc)
	h.keys(key('j'), key('V'))
	h.barrier()
	rc := h.paint()
	var want gui.Rect
	var tint gui.Brush
	h.onLoop(func() {
		l := h.e.layout
		i := l.blockOf(1)
		want = gui.Rect{X: padX + l.gutter - padX/2, Y: padY + l.topOf(i) - l.scroll, W: l.width + padX, H: l.heightOf(i)}
		c := selColor(l.th)
		c.A = 96
		tint = gui.Solid(c)
	})
	found := false
	for _, c := range rc.Calls {
		if c.Op == "FillRect" && c.Rect == want && c.Brush == tint {
			found = true
		}
	}
	if !found {
		t.Error("the selected diagram is not tinted")
	}
}

// What is drawn never folds: a blank line between paragraphs, a code block's lines, a table's.
func TestDrawnLinesDoNotFold(t *testing.T) {
	h := readyDiagrams(t, "one\n\ntwo\n\n```go\nx := 1\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |")
	h.paint()
	h.onLoop(func() {
		for ln := range h.e.core.LineCount() {
			if from, to, _, ok := h.e.layout.FoldOf(ln); ok {
				t.Errorf("line %d (%q) folds, [%d, %d)", ln, h.e.core.LineAt(ln), from, to)
			}
		}
	})
}

// Before the first paint there is no shaper: nothing folds, and the moves that ask (j, k, gj, gk, x)
// run without laying anything out, as the caret is estimated then. Keys reach a mounted editor's
// core before its first paint.
func TestFoldsBeforeTheFirstPaint(t *testing.T) {
	f := &fakeDiagrams{done: map[string]bool{"A-->B": true}}
	e := NewEditor(WithRenderer(NewMarkdownRenderer(WithDiagrams(f))), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText(diagramDoc)))
	e.core.Bind(nil, e.layout) // as the body's Init binds it, with no paint yet
	if _, _, _, ok := e.layout.FoldOf(1); ok {
		t.Error("a line folds before the first paint")
	}
	for _, k := range []string{"j", "k", "gj", "gk", "x"} {
		for _, r := range k {
			e.core.HandleKey(key(r))
		}
	}
	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	if calls != 0 {
		t.Errorf("asked for %d diagrams before the first paint", calls)
	}
}
