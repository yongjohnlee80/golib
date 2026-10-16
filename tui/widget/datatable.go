package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// DATA TABLE — the terminal view of a TableCore: a header row, then the rows with a cell cursor,
// scrolled both ways to keep the cursor in sight. The core owns the rows, the cursor, the
// selection, the sort and the paging; DataTable paints them and turns presses into cursor moves.
//
// Its layout is the backend's. In a terminal it lays the rows out in cells. A native backend binds
// a pixel layout with BindLayout before the first paint, which then scrolls, measures and paints
// the same core; the keys, the cursor and the sort are the same code on both.

// DataTableStyles are DataTable's looks. Zero fields keep the defaults.
type DataTableStyles struct {
	Header   style.Style // default: muted, bold, underlined
	Cell     style.Style // default: the theme's
	Cursor   style.Style // default: reversed
	Selected style.Style // default: bold
	Empty    style.Style // default: muted
}

// DataTable is the terminal view of a TableCore.
type DataTable struct {
	Base
	core      *TableCore
	layout    TableLayout // the table itself (the cell layout) until a backend binds another
	styles    DataTableStyles
	emptyText string
	theme     *style.Theme // the surface's at the last paint, for a pixel layout's colours
	top, left int          // the cell layout's first row shown, first field shown
	w, h      int
}

var (
	_ tui.Focusable = (*DataTable)(nil)
	_ TableLayout   = (*DataTable)(nil)
)

// TableLayoutPointer is a bound layout that places the pointer itself: a pixel layout's rows and
// fields do not fall on cells. Its points are pixels from the view's top-left.
type TableLayoutPointer interface {
	// CellAt is the cell at (x, y); header reports the header row; ok is false off the rows and
	// the fields.
	CellAt(x, y float32) (p TablePos, header, ok bool)
	// ScrollRows scrolls the rows shown by n, leaving the cursor where it is.
	ScrollRows(n int)
}

// NewDataTable is a view of core, laid out in cells until a backend binds another layout.
func NewDataTable(core *TableCore) *DataTable {
	t := &DataTable{core: core, emptyText: "No rows"}
	t.layout = t
	t.SetStyles(DataTableStyles{})
	return t
}

// BindLayout replaces the cell layout with l, a native backend's pixel layout, which from then on
// scrolls the rows, measures the widths and paints the table; the core and its keys stay the same.
// A native style calls it once, before the first paint. nil keeps the layout the table has.
func (t *DataTable) BindLayout(l TableLayout) {
	if l == nil {
		return
	}
	t.layout = l
	t.core.Bind(t.Context(), l)
	t.core.Remeasure() // widths in the cells' units are not the new layout's
	t.RequestLayout()
	t.MarkDirty()
}

// BoundLayout is the layout the table draws with: the table itself (the cell layout), or the one
// BindLayout gave.
func (t *DataTable) BoundLayout() TableLayout { return t.layout }

// bound reports whether a backend's layout took over from the cells.
func (t *DataTable) bound() bool { return t.layout != TableLayout(t) }

// Styles are the table's looks, the defaults filled in.
func (t *DataTable) Styles() DataTableStyles { return t.styles }

// EmptyText is what an empty table shows.
func (t *DataTable) EmptyText() string { return t.emptyText }

// Theme is the tui theme of the surface the table last painted on: what a pixel layout takes its
// accent and muted colours from. nil before the first paint.
func (t *DataTable) Theme() *style.Theme { return t.theme }

// Core is the table's behaviour.
func (t *DataTable) Core() *TableCore { return t.core }

// SetStyles replaces the looks; zero fields keep the defaults.
func (t *DataTable) SetStyles(st DataTableStyles) {
	def := DataTableStyles{
		Header:   style.New().Foreground(style.TokenTextMuted).Bold(true).Underline(true),
		Cursor:   style.New().Reverse(true),
		Selected: style.New().Bold(true),
		Empty:    style.New().Foreground(style.TokenTextMuted),
	}
	pick := func(a, b style.Style) style.Style {
		if a == (style.Style{}) {
			return b
		}
		return a
	}
	t.styles = DataTableStyles{
		Header: pick(st.Header, def.Header), Cell: st.Cell, Cursor: pick(st.Cursor, def.Cursor),
		Selected: pick(st.Selected, def.Selected), Empty: pick(st.Empty, def.Empty),
	}
	t.MarkDirty()
}

// SetEmptyText is what an empty table shows.
func (t *DataTable) SetEmptyText(s string) {
	t.emptyText = s
	t.MarkDirty()
}

// Init binds the core to the table's layout.
func (t *DataTable) Init(ctx *tui.Context) {
	t.Base.Init(ctx)
	t.core.Bind(ctx, t.layout)
}

// AcceptsFocus implements tui.Focusable.
func (t *DataTable) AcceptsFocus() bool { return true }

// Reveal implements TableLayout: scroll so the cell is in sight.
func (t *DataTable) Reveal(row, col int) {
	body := max(t.h-1, 1)
	t.top = min(max(t.top, row-body+1), row)
	t.top = max(t.top, 0)
	if col < t.left {
		t.left = col
	}
	for t.left < col && !t.fits(t.left, col) {
		t.left++
	}
}

// PageRows implements TableLayout.
func (t *DataTable) PageRows() int { return max(t.h-2, 1) }

// Measure implements TableLayout.
func (t *DataTable) Measure(s string) int { return t.measure(s) }

// Changed implements TableLayout.
func (t *DataTable) Changed() { t.MarkDirty() }

// fits reports whether fields from..to fit the width when from is the first shown.
func (t *DataTable) fits(from, to int) bool {
	x := 0
	for col := from; col <= to; col++ {
		if col > from {
			x += tableGap
		}
		x += t.core.Width(col)
	}
	return x <= t.w
}

// Layout fills the space it is given; offered unbounded height it asks for a header and its rows.
func (t *DataTable) Layout(c tui.Constraints) tui.Size {
	w := boundedMax(c.MaxW, max(c.MinW, 1))
	h := boundedMax(c.MaxH, max(c.MinH, 1+max(t.core.Len(), 1)))
	if w != t.w || h != t.h {
		// a new size keeps the cursor in sight; the same size keeps the wheel's scroll. A bound
		// layout keeps it in sight at its own size, which is in its units.
		t.w, t.h = w, h
		if !t.bound() {
			t.Reveal(t.core.Cursor().Row, t.core.Cursor().Col)
		}
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

// Render paints the header, then the rows from the first shown. A bound layout is drawn by its
// backend: the cells under it are filled with the rows' colours, which the native view reads as
// its own.
func (t *DataTable) Render(s tui.Surface) {
	t.theme = s.Theme()
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	if t.bound() {
		base := style.New().Foreground(style.TokenForeground).Background(style.TokenBackground)
		s.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", t.styles.Cell.Inherit(base))
		return
	}
	fields := t.core.Fields()
	sortCol, desc, partial := t.core.Sorted()
	x := 0
	for col := t.left; col < len(fields) && x < sz.W; col++ {
		if col > t.left {
			x += tableGap
		}
		title := fields[col].Title
		if col == sortCol {
			title += map[bool]string{false: " ▲", true: " ▼"}[desc]
			if partial {
				title += " (loaded rows)"
			}
		}
		w := min(t.core.Width(col), sz.W-x)
		drawText(s, x, 0, truncate(padAligned(title, w, style.AlignLeft), w, s.StringWidth), t.styles.Header)
		x += t.core.Width(col)
	}
	if t.core.Len() == 0 {
		if sz.H > 1 {
			drawText(s, 0, 1, truncate(t.emptyText, sz.W, s.StringWidth), t.styles.Empty)
		}
		return
	}
	cur := t.core.Cursor()
	for y := 1; y < sz.H; y++ {
		row := t.top + y - 1
		if row >= t.core.Len() {
			break
		}
		x := 0
		for col := t.left; col < len(fields) && x < sz.W; col++ {
			if col > t.left {
				x += tableGap
			}
			w := min(t.core.Width(col), sz.W-x)
			st := t.styles.Cell
			switch {
			case row == cur.Row && col == cur.Col:
				st = t.styles.Cursor.Inherit(st)
			case t.core.InSelection(row, col):
				st = t.styles.Selected.Inherit(st)
			}
			text := strings.ReplaceAll(t.core.Cell(row, col), "\n", " ")
			drawText(s, x, y, padAligned(elide(text, w, ElideRight, s.StringWidth), w, fields[col].Align), st)
			x += t.core.Width(col)
		}
	}
}

// HandleEvent moves through the core's keys; a press puts the cursor on the cell under it (a
// second press activates), the wheel scrolls. A press on the header is consumed and does nothing;
// any other pointer event goes on to the ancestors. Under a bound layout the pointer is the
// layout's to place (TableLayoutPointer); one that does not place it lets the pointer go on.
func (t *DataTable) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.KeyEvent:
		return t.core.HandleKey(e)
	case tui.MouseEvent:
		p, placed := t.layout.(TableLayoutPointer)
		if t.bound() && !placed {
			return false
		}
		switch {
		case e.Kind == tui.MouseWheel && (e.Button == tui.WheelUp || e.Button == tui.WheelDown):
			step := 3
			if e.Button == tui.WheelUp {
				step = -3
			}
			if t.bound() {
				p.ScrollRows(step)
			} else {
				t.top = min(max(t.top+step, 0), max(t.core.Len()-max(t.h-1, 1), 0))
			}
			t.MarkDirty()
			return true
		case e.Kind == tui.MousePress && e.Button == tui.MouseLeft:
			var pos TablePos
			var header, ok bool
			if t.bound() {
				x, y := t.pixels(e)
				pos, header, ok = p.CellAt(x, y)
			} else {
				pos.Row, header = t.top+e.Y-1, e.Y == 0
				pos.Col, ok = t.fieldAt(e.X)
			}
			if header || !ok {
				return true
			}
			t.core.SetCursor(pos)
			if e.Count >= 2 {
				t.core.HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
			}
			return true
		}
	}
	return false
}

// pixels is the pointer's point in the view, in pixels: the cell it is over and where inside it,
// times a cell's pixels. A backend without pixels answers the cell.
func (t *DataTable) pixels(e tui.MouseEvent) (float32, float32) {
	x, y := float32(e.X)+e.SubX, float32(e.Y)+e.SubY
	if ctx := t.Context(); ctx != nil {
		if cp := ctx.CellPixels(); cp.W > 0 && cp.H > 0 {
			return x * cp.W, y * cp.H
		}
	}
	return x, y
}

// fieldAt is the field shown at column x.
func (t *DataTable) fieldAt(x int) (int, bool) {
	at := 0
	for col := t.left; col < len(t.core.Fields()); col++ {
		if col > t.left {
			at += tableGap
		}
		end := at + t.core.Width(col)
		if x < end {
			return col, x >= at || col > t.left
		}
		at = end
	}
	return 0, false
}
