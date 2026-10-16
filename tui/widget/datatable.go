package widget

import (
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// DATA TABLE — the terminal view of a TableCore: a header row, then the rows with a cell cursor,
// scrolled both ways to keep the cursor in sight, and each row's number in a gutter when the core
// says so. The core owns the rows, the cursor, the selection, the sort and the paging; DataTable
// paints them and turns the pointer into the core's presses and drags. A right press opens its
// menu (SetMenu), the selection left as it is.
//
// Its layout is the backend's. In a terminal it lays the rows out in cells. A native backend binds
// a pixel layout with BindLayout before the first paint, which then scrolls, measures and paints
// the same core; the keys, the cursor and the sort are the same code on both.

// DataTableStyles are DataTable's looks. Zero fields keep the defaults.
type DataTableStyles struct {
	Header style.Style // default: muted, bold, underlined; the row numbers too
	Cell   style.Style // default: the theme's
	Cursor style.Style // default: reversed
	// CursorBlurred is the cursor's cell while the table does not have the keys, so a screen with
	// two panes shows which one the keyboard is in, and still where this one's cursor waits.
	// default: Cursor, faint
	CursorBlurred style.Style
	Selected      style.Style // default: bold
	Empty         style.Style // default: muted
}

// DataTable is the terminal view of a TableCore.
type DataTable struct {
	Base
	core      *TableCore
	layout    TableLayout // the table itself (the cell layout) until a backend binds another
	menu      *EditorMenu // opened by a right press; nil: the press goes on
	menuPos   TablePos    // where the right press that opened the menu was
	menuHit   TableHit
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

// TableLayoutHitter is a TableLayoutPointer that tells every part of the table apart, the row
// numbers' gutter included. DataTable asks HitAt first; a layout without it is placed by CellAt,
// which has no gutter: its header row is TableHitHeader, a cell TableHitCell, anything else
// TableHitNone. CellAt names no header field a press can trust, so a header press there selects
// nothing and leaves the selection as it is.
type TableLayoutHitter interface {
	TableLayoutPointer
	// HitAt is what is at (x, y): a cell (p), a field's title (p.Col), a row's number (p.Row), or
	// nothing.
	HitAt(x, y float32) (p TablePos, hit TableHit)
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
	cursor := pick(st.Cursor, def.Cursor)
	t.styles = DataTableStyles{
		Header: pick(st.Header, def.Header), Cell: st.Cell, Cursor: cursor,
		CursorBlurred: pick(st.CursorBlurred, cursor.Faint(true)),
		Selected:      pick(st.Selected, def.Selected), Empty: pick(st.Empty, def.Empty),
	}
	t.MarkDirty()
}

// CursorStyle is the cursor's look now: Cursor while the table has the keys, CursorBlurred while
// it has not. A bound layout paints its cursor with it.
func (t *DataTable) CursorStyle() style.Style {
	if ctx := t.Context(); ctx != nil && ctx.Focused() {
		return t.styles.Cursor
	}
	return t.styles.CursorBlurred
}

// SetMenu gives the table m, the menu a right press opens at the pointer; nil takes it away, and a
// right press then goes on to the ancestors. Its rows are the caller's (EditorMenu.SetRows), and it
// opens only while enabled. NewEditorMenu(table, nil) is a menu with no editor core.
func (t *DataTable) SetMenu(m *EditorMenu) {
	if t.menu != nil && t.menu != m {
		t.menu.Close()
	}
	t.menu = m
}

// Menu is the table's right-click menu, nil when it has none.
func (t *DataTable) Menu() *EditorMenu { return t.menu }

// MenuAt is where the right press that last opened the menu was, and on what: what a row acting on
// "the field under the pointer" reads. Its rows are built as it opens, so they read the press
// they open for.
func (t *DataTable) MenuAt() (TablePos, TableHit) { return t.menuPos, t.menuHit }

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

// gutter is the cells the row numbers take, their gap included; 0 without them.
func (t *DataTable) gutter() int {
	if d := t.core.RowNumberDigits(); d > 0 {
		return d + tableGap
	}
	return 0
}

// fits reports whether fields from..to fit the width when from is the first shown.
func (t *DataTable) fits(from, to int) bool {
	x := t.gutter()
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
	keys, partial := t.core.SortedKeys()
	gutter, digits := t.gutter(), t.core.RowNumberDigits()
	x := gutter
	for col := t.left; col < len(fields) && x < sz.W; col++ {
		if col > t.left {
			x += tableGap
		}
		title := fields[col].Title
		for i, k := range keys {
			if k.Col != col {
				continue
			}
			title += map[bool]string{false: " ▲", true: " ▼"}[k.Desc]
			if i == 0 && partial {
				title += " (loaded rows)"
			}
			break
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
	cur, cursor := t.core.Cursor(), t.CursorStyle()
	for y := 1; y < sz.H; y++ {
		row := t.top + y - 1
		if row >= t.core.Len() {
			break
		}
		if digits > 0 {
			n := strconv.Itoa(row + 1)
			drawText(s, 0, y, truncate(strings.Repeat(" ", max(digits-len(n), 0))+n, sz.W, s.StringWidth), t.styles.Header)
		}
		x := gutter
		for col := t.left; col < len(fields) && x < sz.W; col++ {
			if col > t.left {
				x += tableGap
			}
			w := min(t.core.Width(col), sz.W-x)
			st := t.styles.Cell
			switch {
			case row == cur.Row && col == cur.Col:
				st = cursor.Inherit(st)
			case t.core.InSelection(row, col):
				st = t.styles.Selected.Inherit(st)
			}
			text := strings.ReplaceAll(t.core.Cell(row, col), "\n", " ")
			drawText(s, x, y, padAligned(elide(text, w, ElideRight, s.StringWidth), w, fields[col].Align), st)
			x += t.core.Width(col)
		}
	}
}

// HandleEvent moves through the core's keys and drives its pointer: a left press is the core's
// PressAt on what is under it (a second press on a cell activates its row), a drag with the button
// held is its DragTo, with the pointer kept until the release, and the wheel scrolls. A right
// press opens the menu, if the table has one enabled. Motion with no drag goes on to the
// ancestors. Under a bound layout the pointer is the layout's to place (TableLayoutPointer); one
// that does not place it lets the pointer go on.
func (t *DataTable) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.FocusEvent:
		t.MarkDirty() // the cursor's look follows the focus; the news is everyone's
		return false
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
			pos, hit := t.hitAt(e)
			if t.unplacedHeader(hit) {
				return true // which field is unknown: the selection is left as it is
			}
			t.core.PressAt(pos, hit)
			if t.core.Dragging() {
				if ctx := t.Context(); ctx != nil {
					ctx.CapturePointer()
				}
			}
			if hit == TableHitCell && e.Count >= 2 {
				t.core.HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
			}
			return true
		case e.Kind == tui.MousePress && e.Button == tui.MouseRight && t.menu != nil && t.menu.Enabled():
			// The selection is left as it is: the menu's rows act on it.
			t.menuPos, t.menuHit = t.hitAt(e)
			if t.unplacedHeader(t.menuHit) {
				t.menuPos, t.menuHit = TablePos{}, TableHitNone
			}
			return t.menu.OpenAt(tui.Point{X: e.X, Y: e.Y})
		case e.Kind == tui.MouseMotion && e.Button == tui.MouseLeft && t.core.Dragging():
			if pos, ok := t.dragAt(e); ok {
				t.core.DragTo(pos)
			}
			return true
		case e.Kind == tui.MouseRelease && t.core.Dragging():
			t.core.EndDrag()
			if ctx := t.Context(); ctx != nil && ctx.HasPointerCapture() {
				ctx.ReleasePointer()
			}
			return true
		}
	}
	return false
}

// hitAt is what the pointer is over: a bound layout's HitAt, else its CellAt (with no gutter); the
// cell layout's own reckoning when nothing is bound.
func (t *DataTable) hitAt(e tui.MouseEvent) (TablePos, TableHit) {
	if !t.bound() {
		return t.cellHit(e.X, e.Y)
	}
	x, y := t.pixels(e)
	if h, ok := t.layout.(TableLayoutHitter); ok {
		return h.HitAt(x, y)
	}
	pos, header, ok := t.layout.(TableLayoutPointer).CellAt(x, y)
	switch {
	case header:
		return pos, TableHitHeader
	case ok:
		return pos, TableHitCell
	}
	return pos, TableHitNone
}

// unplacedHeader reports a header hit from a bound layout without HitAt. Its CellAt says a press
// is on the header row but not on which field (a layout may answer TablePos{} for the whole row),
// so selecting a column from it would select the wrong one.
func (t *DataTable) unplacedHeader(hit TableHit) bool {
	if hit != TableHitHeader || !t.bound() {
		return false
	}
	_, placed := t.layout.(TableLayoutHitter)
	return !placed
}

// dragAt is the cell a drag is over. The cell layout reckons one wherever the pointer is, past the
// edges included, so a drag below the rows reaches the next and the view scrolls to it; a bound
// layout's is its hit, and nothing off the table.
func (t *DataTable) dragAt(e tui.MouseEvent) (TablePos, bool) {
	if t.bound() {
		pos, hit := t.hitAt(e)
		return pos, hit != TableHitNone && !t.unplacedHeader(hit)
	}
	col := t.left
	if x := e.X - t.gutter(); x >= 0 {
		var ok bool
		if col, ok = t.fieldAt(x); !ok {
			col = len(t.core.Fields()) - 1
		}
	}
	return TablePos{Row: t.top + e.Y - 1, Col: col}, true
}

// cellHit is what is at cell (x, y) of the cell layout.
func (t *DataTable) cellHit(x, y int) (TablePos, TableHit) {
	g := t.gutter()
	if y == 0 {
		if col, ok := t.fieldAt(x - g); ok && x >= g {
			return TablePos{Col: col}, TableHitHeader
		}
		return TablePos{}, TableHitNone
	}
	row := t.top + y - 1
	switch {
	case y < 0 || row >= t.core.Len():
		return TablePos{}, TableHitNone
	case x < g:
		return TablePos{Row: row}, TableHitGutter
	}
	if col, ok := t.fieldAt(x - g); ok {
		return TablePos{Row: row, Col: col}, TableHitCell
	}
	return TablePos{}, TableHitNone
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

// fieldAt is the field shown at column x, counted from the end of the gutter.
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
