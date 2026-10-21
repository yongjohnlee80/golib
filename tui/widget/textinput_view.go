package widget

import "github.com/yongjohnlee80/golib/tui"

// THE TEXTINPUT'S VIEW: the one-row viewport, its horizontal scroll, the caret cell and the paint,
// a mask's rune standing in for every cluster.

// renderCluster returns the painted form of cluster cl (mask-aware).
func (t *TextInput) renderCluster(cl string) string {
	if t.mask != 0 {
		return string(t.mask)
	}
	return cl
}

// cellAt is the display offset (cells) of cluster index i in painted form.
func (t *TextInput) cellAt(i int) int {
	return cellsBefore(t.clusters(), i, func(cl string) int { return t.measure(t.renderCluster(cl)) })
}

// ensureVisible adjusts the horizontal scroll so the cursor cell is inside
// the viewport (using the last layout width).
func (t *TextInput) ensureVisible() {
	if t.width <= 0 {
		return
	}
	cx := t.cellAt(t.cursor())
	if cx < t.scroll {
		t.scroll = cx
	}
	if cx >= t.scroll+t.width {
		t.scroll = cx - t.width + 1
	}
	total := t.cellAt(t.length()) + 1 // +1: the cursor cell past the end
	t.scroll = max(0, min(t.scroll, max(total-t.width, 0)))
}

// Layout: height 1, width greedy; the viewport width feeds cursor-visible
// scrolling.
func (t *TextInput) Layout(c tui.Constraints) tui.Size {
	t.pad = 0
	if ctx := t.Context(); ctx != nil && ctx.Capabilities().NativeViews {
		t.pad = 1 // the native field's rounded frame: text starts a cell in from each end
	}
	w := boundedMax(c.MaxW, max(c.MinW, t.cellAt(t.length())+1+2*t.pad))
	if w <= 2*t.pad {
		t.pad = 0
	}
	t.width = w - 2*t.pad
	t.ensureVisible()
	return c.Constrain(tui.Size{W: w, H: 1})
}

// Cursor implements tui.CursorReporter: the insertion point in local
// coordinates (the runtime consults it only while this widget is focused).
func (t *TextInput) Cursor() (int, int, bool) {
	x := t.cellAt(t.cursor()) - t.scroll
	if t.width > 0 {
		x = min(x, t.width-1)
	}
	return max(x, 0) + t.pad, 0, true
}

// Render paints value (or placeholder), selection fill, and mask runes.
func (t *TextInput) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	// The field paints its whole box, as a Qt field does, not only the cells
	// its text covers: an empty field on a card is still a field.
	s.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", t.styles.Text)
	pad := t.pad
	if sz.W <= 2*pad {
		pad = 0
	}
	inner := sz.W - 2*pad
	cs := t.clusters()
	if len(cs) == 0 {
		if ph := t.placeholder.inLanguage(&t.Base); ph != "" {
			drawText(s, pad, 0, truncate(ph, inner, s.StringWidth), t.styles.Placeholder)
		}
		return
	}
	base := t.styles.Text
	if t.verr != nil {
		base = t.styles.Error.Inherit(t.styles.Text)
	}
	selLo, selHi, hasSel := t.selection()
	x := t.eachVisible(inner, s.StringWidth, func(i int, cl string, x int) {
		st := base
		if hasSel && t.focused() && i >= selLo && i < selHi {
			st = t.styles.Selection.Inherit(base)
		}
		s.SetCell(pad+x, 0, cl, st)
	})
	if h := t.hint(); h != "" && x >= 0 && x < inner {
		drawText(s, pad+x, 0, truncate(h, inner-x, s.StringWidth), t.styles.Placeholder)
	}
}

// eachVisible walks the clusters the field shows within inner cells: fn has each one's index, its
// painted form and its cell offset from the text's left edge. A cluster cut by either edge is not
// shown. It returns the offset where the walk stopped, past the last cluster shown, where Render
// draws the completion hint. Render and SelectionCells both walk by it, so the cells a selection
// is reported at are the cells it is drawn at.
func (t *TextInput) eachVisible(inner int, measure func(string) int, fn func(i int, cl string, x int)) int {
	x := -t.scroll
	for i, c := range t.clusters() {
		cl := t.renderCluster(c)
		cw := measure(cl)
		if x+cw > inner {
			break
		}
		if x >= 0 {
			fn(i, cl, x)
		}
		x += cw
	}
	return x
}

var _ tui.SelectionCells = (*TextInput)(nil)

// SelectionCells implements tui.SelectionCells: the cells Render draws the selection over, on the
// field's one row, after its padding and scroll, clipped as Render clips. None while the field is
// unfocused, as Render then draws none.
func (t *TextInput) SelectionCells() ([]tui.Rect, bool) {
	lo, hi, ok := t.selection()
	if !ok || !t.focused() || t.width <= 0 {
		return nil, false
	}
	x0, x1 := -1, -1
	t.eachVisible(t.width, t.measure, func(i int, cl string, x int) {
		if i >= lo && i < hi {
			if x0 < 0 {
				x0 = x
			}
			x1 = x + t.measure(cl)
		}
	})
	if x0 < 0 {
		return nil, false
	}
	return []tui.Rect{{X: t.pad + x0, W: x1 - x0, H: 1}}, true
}
