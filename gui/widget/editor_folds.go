package widget

import (
	"github.com/yongjohnlee80/golib/gui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

var _ tuiwidget.EditorFoldLayout = (*pixelLayout)(nil)

// THE EDITOR'S FOLDS — tui/widget's EditorFoldLayout, answered from the Rendered view's laid blocks:
// a fold is a run of a block's lines with nothing of theirs drawn, a diagram's
// picture or hidden metadata, where the caret has no place. While text is typed the cursor's block
// is laid raw (typing), so nothing folds; the Raw view folds nothing. The body draws the cursor's
// fold (foldRect), and Reveal scrolls to it.

// FoldOf is the fold holding line: the run of its block's lines around it with no span in the
// block's layout. hidden: the block draws nothing (zero height). Answering lays the block out, which
// may ask for its diagram: the block a move is about to reveal. Before the first paint there is no
// shaper to lay out with, and nothing has been drawn to fold: none, as caretAt estimates then.
func (l *pixelLayout) FoldOf(line int) (from, to int, hidden, ok bool) {
	if l.sh == nil || l.typing() || l.e.ShownView() != tuiwidget.ViewRendered || l.e.render == nil {
		return 0, 0, false, false
	}
	i := l.blockOf(line)
	if i >= len(l.blocks) {
		return 0, 0, false, false
	}
	lb := l.lay(i, nil)
	drawn := func(ln int) bool {
		_, _, ok := posIn(lb, ln, 0)
		return ok
	}
	if drawn(line) {
		return 0, 0, false, false
	}
	b := l.blocks[i]
	from, to = line, line+1
	for from > b.From && !drawn(from-1) {
		from--
	}
	for to < b.To && !drawn(to) {
		to++
	}
	return from, to, lb.bl.Height == 0, true
}

// foldBar is the height a hidden fold's cursor is drawn at: it has none of its own.
const foldBar = 2

// foldRect is where line's fold is drawn, in the text's coordinates: its block's rectangle, as its
// tint is painted, or a bar at its place when it is hidden. ok is false when line isn't folded.
func (l *pixelLayout) foldRect(line int) (r gui.Rect, hidden, ok bool) {
	if _, _, hidden, ok = l.FoldOf(line); !ok {
		return gui.Rect{}, false, false
	}
	i := l.blockOf(line)
	h := l.heightOf(i)
	if hidden {
		h = foldBar
	}
	return gui.Rect{X: -padX / 2, Y: l.topOf(i), W: l.width + padX, H: h}, hidden, true
}

// paintFold draws the cursor on a fold at r: its outline in the caret's colour, or a bar when the
// fold is hidden.
func paintFold(c gui.Canvas, r gui.Rect, hidden bool, th Theme) {
	col := th.Text
	if th.Caret.A > 0 {
		col = th.Caret
	}
	if hidden {
		c.FillRect(r, gui.Solid(col))
		return
	}
	const w = 2
	c.FillRect(gui.Rect{X: r.X, Y: r.Y, W: r.W, H: w}, gui.Solid(col))
	c.FillRect(gui.Rect{X: r.X, Y: r.Y + r.H - w, W: r.W, H: w}, gui.Solid(col))
	c.FillRect(gui.Rect{X: r.X, Y: r.Y, W: w, H: r.H}, gui.Solid(col))
	c.FillRect(gui.Rect{X: r.X + r.W - w, Y: r.Y, W: w, H: r.H}, gui.Solid(col))
}
