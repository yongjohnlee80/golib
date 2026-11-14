package widget

import (
	"image/color"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// THE EDITOR'S TOP LINE AND LINE LOOKS, as the terminal Editor's (tui/widget/editor), for a host
// that lays several views side by side: the first line shown, read, set and followed, and a look
// for each whole line.

// WithEditorOnScroll calls fn with the first line shown each time it changes, however it moved: a
// key, the wheel, a reveal of the cursor, a new text, SetTopLine. The change is seen as the view is
// drawn and told on the loop's next turn, never while the view paints; a view that is drawn with the
// top where it was tells nothing.
func WithEditorOnScroll(fn func(top int)) EditorOption {
	return func(c *editorConfig) { c.onScroll = fn }
}

// WithEditorLineStyle gives each line a look of its own: fn answers line's style and true, or false
// for the text's own. Its background is laid across the line's whole row, under the line's marks,
// its text, the selection and the caret: a diff's added and changed lines.
func WithEditorLineStyle(fn func(line int) (style.Style, bool)) EditorOption {
	return func(c *editorConfig) { c.lineStyle = fn }
}

// SetLineStyle replaces the line looks (WithEditorLineStyle); nil for none.
func (e *Editor) SetLineStyle(fn func(line int) (style.Style, bool)) {
	e.lineStyle = fn
	e.body.MarkDirty()
}

// TopLine is the first line shown, from 0: in pixels, the line whose top the view's top is at or
// past; in cells, the first row's.
func (e *Editor) TopLine() int {
	if !e.body.native() {
		return e.body.cellTop
	}
	if e.pendingTop >= 0 {
		return e.pendingTop
	}
	if e.layout.height <= 0 {
		return 0
	}
	first, _ := e.layout.VisibleLines()
	return first
}

// SetTopLine makes line the first shown, clamped to the text: never above the first line, and never
// so low that the text's end is above the view's bottom, in pixels or in the cell fallback's rows.
// Before the view is first drawn, it is held and applied then.
func (e *Editor) SetTopLine(line int) {
	line = max(0, min(line, max(e.core.LineCount()-1, 0)))
	// the cell fallback's rows: never so low that the last line is above the bottom row
	e.body.cellTop = line
	if h := e.body.h; h > 0 {
		e.body.cellTop = min(line, max(e.core.LineCount()-h, 0))
	}
	if e.layout.height <= 0 || e.layout.sh == nil {
		e.pendingTop = line
	} else {
		e.layout.scrollTo(line)
	}
	e.body.MarkDirty()
}

// scrollTo puts line at the view's top, within the text.
func (l *pixelLayout) scrollTo(line int) {
	y := l.topOf(l.blockOf(line))
	if r, ok := l.caretAt(line, 0); ok {
		y = r.Y
	}
	l.scroll = max(min(y, l.total()-(l.height-2*padY)), 0)
}

// toldScroll tells the host the first line shown, after a draw, when it is another than it was
// last told: on the loop's next turn, so a host that moves other views does it outside this paint.
func (e *Editor) toldScroll() {
	if e.onScroll == nil {
		return
	}
	top := e.TopLine()
	if top == e.toldTop {
		return
	}
	e.toldTop = top
	fn := e.onScroll
	if ctx := e.body.Context(); ctx != nil {
		ctx.Update(func() { fn(top) })
	}
}

// lineLook is line's own look (WithEditorLineStyle), or false.
func (e *Editor) lineLook(line int) (style.Style, bool) {
	if e.lineStyle == nil || line < 0 {
		return style.Style{}, false
	}
	return e.lineStyle(line)
}

// lineFill is line's own background in pixels, resolved against the page bg is drawn on, or false.
func (e *Editor) lineFill(line int, th *style.Theme, bg color.NRGBA, pal gui.Palette) (color.NRGBA, bool) {
	st, ok := e.lineLook(line)
	if !ok {
		return color.NRGBA{}, false
	}
	c, ok := st.GetBackground()
	if !ok {
		return color.NRGBA{}, false
	}
	return colorOf(c, th, isDark(bg), pal)
}
