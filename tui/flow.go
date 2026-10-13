package tui

// Flow is Qt's Flow: children laid left to right, each at its own size, wrapping to a new line
// when the next one would pass the right edge. A row of buttons in a narrow panel stays whole
// instead of being clipped: one line when there is room, as many as it needs when there is not.
//
//	wide:    [Preview] [Add] [◀ Status] [Status ▶]
//	narrow:  [Preview] [Add]
//	         [◀ Status]
//	         [Status ▶]
//
// Spacing is the gap between two children on a line, and between two lines unless RowSpacing
// says otherwise (Qt's GridLayout names): a terminal row of buttons wants a column between them
// and none between its lines. A child wider than the Flow is given the whole width, on a line of
// its own. A hidden child takes no room. With no bound on its width a Flow is one line. It sizes
// to its lines, never stretching a child; a parent that fixes its width (a column stretching it)
// gets that width, and the height the lines need.
type Flow struct {
	MultiChild
	spacing    int
	rowSpacing int // -1: as spacing
}

var _ Container = (*Flow)(nil)

// NewFlow builds an empty Flow, its children touching (spacing 0, as Qt's).
func NewFlow() *Flow {
	f := &Flow{rowSpacing: -1}
	f.Label("Flow")
	return f
}

// Spacing is the gap, in cells, between two children on a line.
func (f *Flow) Spacing() int { return f.spacing }

// SetSpacing sets the gap between two children on a line, and between lines while RowSpacing
// follows it. A negative gap is none.
func (f *Flow) SetSpacing(n int) {
	n = max(n, 0)
	if f.spacing != n {
		f.spacing = n
		f.relayout()
	}
}

// RowSpacing is the gap, in rows, between two lines: Spacing unless SetRowSpacing set its own.
func (f *Flow) RowSpacing() int {
	if f.rowSpacing < 0 {
		return f.spacing
	}
	return f.rowSpacing
}

// SetRowSpacing sets the gap between lines; a negative one makes it follow Spacing again.
func (f *Flow) SetRowSpacing(n int) {
	n = max(n, -1)
	if f.rowSpacing != n {
		f.rowSpacing = n
		f.relayout()
	}
}

func (f *Flow) relayout() {
	if f.ctx != nil {
		f.ctx.RequestLayout()
	}
}

// Layout measures each child up to the Flow's width and places it after the one before, or at the
// start of a new line when it would pass the right edge.
func (f *Flow) Layout(c Constraints) Size {
	width := c.MaxW
	rowGap := f.RowSpacing()
	x, y, lineH, widest := 0, 0, 0, 0
	lines := 0
	for _, it := range f.All() {
		if hidden(it) {
			f.Ctx().PlaceChild(it, Rect{})
			continue
		}
		sz := f.Ctx().LayoutChild(it, Constraints{MaxW: width, MaxH: c.MaxH})
		at := 0
		if lines == 0 {
			lines = 1
		} else {
			at = x + f.spacing
			if width != Unbounded && at+sz.W > width {
				y += lineH + rowGap
				at, lineH = 0, 0
				lines++
			}
		}
		f.Ctx().PlaceChild(it, Rect{X: at, Y: y, W: sz.W, H: sz.H})
		x = at + sz.W
		lineH = max(lineH, sz.H)
		widest = max(widest, x)
	}
	return c.Constrain(Size{W: widest, H: y + lineH})
}

// Render paints nothing: a Flow is pure geometry.
func (f *Flow) Render(Surface) {}

// HandleEvent consumes nothing; events bubble through.
func (f *Flow) HandleEvent(Event) bool { return false }
