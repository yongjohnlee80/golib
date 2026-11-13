package editor

import "github.com/yongjohnlee80/golib/tui/style"

// THE EDITOR'S TOP LINE AND LINE LOOKS, for a host that lays several views side by side: the first
// line shown, read, set and followed, and a look for each whole line.

// WithEditorOnScroll calls fn with the first line shown each time it changes: a key's move, the
// wheel, a reveal of the cursor, a new text, SetTopLine. A layout that leaves it where it was calls
// nothing.
func WithEditorOnScroll(fn func(top int)) EditorOption { return func(e *Editor) { e.onScroll = fn } }

// WithEditorLineStyle gives each line a look of its own: fn answers line's style and true, or false
// for the text's own. The style is laid over the text's look across the whole row, under the
// syntax styles and the selection; a whole-line mark, such as a diff's added and deleted lines. The
// Rendered view draws its own looks and does not ask.
func WithEditorLineStyle(fn func(line int) (style.Style, bool)) EditorOption {
	return func(e *Editor) { e.lineStyle = fn }
}

// SetLineStyle replaces the line looks (WithEditorLineStyle); nil for none.
func (e *Editor) SetLineStyle(fn func(line int) (style.Style, bool)) {
	e.lineStyle = fn
	e.MarkDirty()
}

// TopLine is the first line shown, from 0.
func (e *Editor) TopLine() int { return e.cells.top }

// SetTopLine makes line the first shown, clamped to the text: never above the first line, and,
// once the editor is laid out, never so low that the last line is above the view's bottom.
func (e *Editor) SetTopLine(line int) {
	line = max(0, min(line, max(e.core.LineCount()-1, 0)))
	l := &e.cells
	switch {
	case l.h <= 0 || e.rowMap().r != nil: // not laid out yet, or the Rendered view: by lines
		l.top, l.topIn = line, 0
		e.topMoved()
		e.MarkDirty()
	default:
		l.ScrollLines(line - l.top)
	}
}

// topMoved tells the host the first line shown, when it is another than it was last told.
func (e *Editor) topMoved() {
	if e.cells.top == e.toldTop {
		return
	}
	e.toldTop = e.cells.top
	if e.onScroll != nil {
		e.onScroll(e.cells.top)
	}
}

// lineLook is line's own look (WithEditorLineStyle), or false.
func (e *Editor) lineLook(line int) (style.Style, bool) {
	if e.lineStyle == nil {
		return style.Style{}, false
	}
	return e.lineStyle(line)
}
