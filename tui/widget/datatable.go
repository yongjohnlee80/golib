package widget

import (
	"math"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/style"
)

// DATA TABLE — the terminal view of a TableCore: a header (the titles over a rule), then the rows
// with a cell cursor, scrolled both ways to keep the cursor in sight, and each row's number in a
// gutter, ruled off from the cells, when the core says so. The core owns the rows, the cursor, the
// selection, the sort and the paging; DataTable paints them and turns the pointer into the core's
// presses and drags. A right press opens its menu (SetMenu), the selection left as it is.
//
// Its layout is the backend's. In a terminal it lays the rows out in cells. A native backend binds
// a pixel layout with BindLayout before the first paint, which then scrolls, measures and paints
// the same core; the keys, the cursor and the sort are the same code on both.

// DataTableStyles are DataTable's looks. Zero fields keep the defaults.
type DataTableStyles struct {
	// Header is the titles, the row numbers and the rules between them and the cells. Its border's
	// Top is the rule under the titles and its Left the gutter's (default: BorderNormal's).
	// default: muted, bold
	Header style.Style
	Cell   style.Style // default: the theme's
	Cursor style.Style // default: reversed
	// CursorBlurred is the cursor's cell while the table does not have the keys, so a screen with
	// two panes shows which one the keyboard is in, and still where this one's cursor waits.
	// default: Cursor, faint
	CursorBlurred style.Style
	Selected      style.Style // default: the secondary fill
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
	caret     *tui.Point // the open field's caret, view-local, as last painted; nil for none
}

// tableEdgeWidth is the cells the edge field (TableAppendEdges) takes.
const tableEdgeWidth = 3

// tableHeaderRows is the rows above the first row shown: the titles and the rule under them.
const tableHeaderRows = 2

// tableRuleRow is the row of the rule under the titles, the header's last.
const tableRuleRow = tableHeaderRows - 1

// tableRuleGap is the cells either side of the gutter's rule: the digits, the gap, the rule, the
// gap, then the first field.
const tableRuleGap = 1

// tableRuleJunctions are where the gutter's rule crosses the titles', by their glyphs (vertical,
// horizontal): the box-drawing cross of the prefab borders' weight, and ASCII's +. A pair not here
// that is one glyph twice (BorderHidden's blank) crosses as that glyph; any other, as the vertical,
// the gutter's rule unbroken.
var tableRuleJunctions = map[[2]string]string{
	{"│", "─"}: "┼", // BorderNormal, BorderRounded
	{"┃", "━"}: "╋", // BorderThick
	{"║", "═"}: "╬", // BorderDouble
	{"|", "-"}: "+",
}

// ruleJunction is where rules of these glyphs cross (tableRuleJunctions).
func ruleJunction(vertical, horizontal string) string {
	if j, ok := tableRuleJunctions[[2]string{vertical, horizontal}]; ok {
		return j
	}
	return vertical // the same glyph twice included
}

// tableCellLine is a cell's value on one line, as a table shows it: a newline is ⏎, a tab →. The
// value itself is unchanged (an open field holds it whole).
var tableCellLine = strings.NewReplacer("\r\n", "⏎", "\n", "⏎", "\r", "⏎", "\t", "→").Replace

// TableCellLine is a cell's value as a table's one-line cell shows it: a newline as ⏎, a tab as →.
func TableCellLine(value string) string { return tableCellLine(value) }

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
		Header:   style.New().Foreground(style.TokenTextMuted).Bold(true),
		Cursor:   style.New().Reverse(true),
		Selected: style.New().Background(style.TokenSecondary).Foreground(style.TokenTextOnSecondary),
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
	body := t.bodyRows()
	// rows gone (a filter) leave no blank below the last
	t.top = revealRow(clampTop(t.top, t.core.Len(), body), row, body)
	if col < t.left {
		t.left = col
	}
	for t.left < col && !t.fits(t.left, col) {
		t.left++
	}
}

// ScrollFields implements TableLayoutFields: the first field shown moves by n, clamped so the last
// field still shows. A bound layout that scrolls its fields is asked instead.
func (t *DataTable) ScrollFields(n int) {
	if t.bound() {
		if l, ok := t.layout.(TableLayoutFields); ok {
			l.ScrollFields(n)
		}
		return
	}
	last := len(t.core.Fields()) - 1
	if t.core.EdgeField() {
		last++
	}
	t.left = min(max(t.left+n, 0), max(last, 0))
	t.MarkDirty()
}

// PageRows implements TableLayout.
func (t *DataTable) PageRows() int { return max(t.bodyRows()-1, 1) }

// bodyRows is the rows shown below the header, at least one.
func (t *DataTable) bodyRows() int { return max(t.h-tableHeaderRows, 1) }

// Measure implements TableLayout.
func (t *DataTable) Measure(s string) int { return t.measure(s) }

// Changed implements TableLayout.
func (t *DataTable) Changed() { t.MarkDirty() }

// gutter is the cells the row numbers take, their rule and the gap either side of it included; 0
// without them.
func (t *DataTable) gutter() int {
	if d := t.core.RowNumberDigits(); d > 0 {
		return ruleX(d) + 1 + tableRuleGap
	}
	return 0
}

// ruleX is the column of the gutter's rule after digits row-number digits.
func ruleX(digits int) int { return digits + tableRuleGap }

// rules are the glyphs of the gutter's rule and of the titles': Header's border, else BorderNormal.
func (t *DataTable) rules() (vertical, horizontal string) {
	b, ok := t.styles.Header.GetBorder()
	if !ok || b.Left == "" || b.Top == "" {
		b = style.BorderNormal
	}
	return b.Left, b.Top
}

// drawRules draws the rule under the titles across the view, and the gutter's on every row shown,
// crossing at their ruleJunction.
func (t *DataTable) drawRules(s tui.Surface, digits int) {
	sz := s.Size()
	vertical, horizontal := t.rules()
	if sz.H > tableRuleRow {
		w := max(s.StringWidth(horizontal), 1) // a border's Top may be wider than a cell
		drawText(s, 0, tableRuleRow, strings.Repeat(horizontal, sz.W/w), t.styles.Header)
	}
	x := ruleX(digits)
	if digits <= 0 || x >= sz.W {
		return
	}
	for y := range sz.H {
		glyph := vertical
		if y == tableRuleRow {
			glyph = ruleJunction(vertical, horizontal)
		}
		drawText(s, x, y, glyph, t.styles.Header)
	}
}

// colSpan is one field's place in the view: field col (len(Fields) for the edge field) starts at
// cell x, gutter included, and is w cells wide, before the view clips it.
type colSpan struct{ col, x, w int }

// spans are the fields shown from the first one shown (left), in order, while they start inside
// width cells: the ONE geometry the header, the cells, the blank rows and the pointer's hit-test
// read. edge adds the edge field after the last, when the core has one.
func (t *DataTable) spans(width int, edge bool) []colSpan {
	n := len(t.core.Fields())
	last := n - 1
	if edge && t.core.EdgeField() {
		last = n
	}
	var out []colSpan
	x := t.gutter()
	for col := t.left; col <= last && x < width; col++ {
		if col > t.left {
			x += tableGap
		}
		w := tableEdgeWidth
		if col < n {
			w = t.core.Width(col)
		}
		out = append(out, colSpan{col, x, w})
		x += w
	}
	return out
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
	w := fillW(c)
	h := boundedMax(c.MaxH, max(c.MinH, tableHeaderRows+max(t.core.DrawnRows(), 1)))
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
	for _, sp := range t.spans(sz.W, true) {
		if sp.col == len(fields) { // the edge field
			drawText(s, sp.x, 0, truncate(" + ", sz.W-sp.x, s.StringWidth), t.styles.Empty)
			continue
		}
		title := fields[sp.col].Title
		for i, k := range keys {
			if k.Col != sp.col {
				continue
			}
			title += map[bool]string{false: " ▲", true: " ▼"}[k.Desc]
			if i == 0 && partial {
				title += " (loaded rows)"
			}
			break
		}
		drawText(s, sp.x, 0, fitCell(title, min(sp.w, sz.W-sp.x), ElideRight, style.AlignLeft, s.StringWidth), t.styles.Header)
	}
	t.caret = nil
	t.drawRules(s, digits)
	if t.core.DrawnRows() == 0 {
		if sz.H > tableHeaderRows {
			drawText(s, gutter, tableHeaderRows, truncate(t.emptyText, max(sz.W-gutter, 0), s.StringWidth), t.styles.Empty)
		}
		return
	}
	cur, cursor := t.core.Cursor(), t.CursorStyle()
	for y := tableHeaderRows; y < sz.H; y++ {
		v := t.top + y - tableHeaderRows
		if v >= t.core.DrawnRows() {
			break
		}
		kind, row := t.core.DrawnRow(v)
		if kind != TableRowData {
			t.drawBlankRow(s, y, kind, row, gutter, cur, cursor)
			continue
		}
		if digits > 0 {
			n := strconv.Itoa(row + 1)
			drawText(s, 0, y, truncate(strings.Repeat(" ", max(digits-len(n), 0))+n, sz.W, s.StringWidth), t.styles.Header)
		}
		for _, sp := range t.spans(sz.W, true) {
			w := min(sp.w, sz.W-sp.x)
			if sp.col == len(fields) { // the edge field: the cursor's, when it is there
				if row == cur.Row && cur.Col == sp.col {
					drawText(s, sp.x, y, strings.Repeat(" ", w), cursor.Inherit(t.styles.Cell))
				}
				continue
			}
			st := t.styles.Cell
			switch {
			case t.core.CursorCovers(row, sp.col):
				st = cursor.Inherit(st)
			case t.core.InSelection(row, sp.col):
				st = t.styles.Selected.Inherit(st)
			}
			text := tableCellLine(t.core.Cell(row, sp.col))
			drawText(s, sp.x, y, fitCell(text, w, ElideRight, fields[sp.col].Align, s.StringWidth), st)
		}
	}
	t.drawField(s, gutter, fields)
}

// drawBlankRow draws o's new row or the edge row: empty cells, the cursor's among them; the edge
// row's gutter says +.
func (t *DataTable) drawBlankRow(s tui.Surface, y int, kind TableRowKind, row, gutter int,
	cur TablePos, cursor style.Style) {
	sz := s.Size()
	if kind == TableRowEdge && gutter > 0 {
		drawText(s, 0, y, truncate(padAligned("+", t.core.RowNumberDigits(), style.AlignRight, s.StringWidth), sz.W, s.StringWidth), t.styles.Empty)
	}
	if kind != TableRowEdge || row != cur.Row {
		return
	}
	for _, sp := range t.spans(sz.W, true) {
		if sp.col == cur.Col {
			drawText(s, sp.x, y, strings.Repeat(" ", min(sp.w, sz.W-sp.x)), cursor.Inherit(t.styles.Cell))
		}
	}
}

// fieldX is where field col starts in the view, from the gutter; ok false when it is scrolled out.
func (t *DataTable) fieldX(col, gutter int) (int, bool) {
	if col < t.left {
		return 0, false
	}
	x := gutter
	for c := t.left; c < col; c++ { // past the view's width too: an open field reads where it would be
		x += t.core.Width(c) + tableGap
	}
	return x, true
}

// drawField draws the open field over its cell, as wide as its field (at least 12 cells) and as tall
// as its value's lines, growing downward, and keeps its caret for Cursor.
func (t *DataTable) drawField(s tui.Surface, gutter int, fields []TableField) {
	e := t.core.Editing()
	if e == nil {
		return
	}
	sz := s.Size()
	x, ok := t.fieldX(e.Pos.Col, gutter)
	y := tableHeaderRows + t.core.DrawnOf(e.Pos.Row) - t.top
	if e.Inserting() {
		y = tableHeaderRows + e.Pos.Row - t.top
	}
	if !ok || x >= sz.W || y < tableHeaderRows || y >= sz.H {
		return
	}
	w := tableEdgeWidth
	if e.Pos.Col < len(fields) {
		w = t.core.Width(e.Pos.Col)
	}
	w = min(max(w, 12), sz.W-x)
	// drawn here, line by line, not through the TextArea's own Layout and Render: a Render that lays
	// out or paints a component it does not own the placement of runs a second layout in the paint
	lines := strings.Split(e.Field.Value(), "\n")
	ln, col := e.Field.Caret()
	ln = min(ln, len(lines)-1)
	h := min(len(lines), sz.H-y)
	top := max(ln-h+1, 0)
	cs := clusters(lines[ln])
	caretX := s.StringWidth(strings.Join(cs[:min(col, len(cs))], ""))
	left := max(caretX-w+1, 0) // the caret kept in sight
	r := tui.Rect{X: x, Y: y, W: w, H: h}
	st := t.CursorStyle().Inherit(t.styles.Cell)
	s.Fill(r, " ", st)
	sub := s.Sub(r)
	for i := range h {
		drawText(sub, 0, i, fromCell(lines[top+i], left, s.StringWidth), st)
	}
	t.caret = &tui.Point{X: x + caretX - left, Y: y + ln - top}
}

// fromCell is line from cell off on: the clusters before it dropped.
func fromCell(line string, off int, width func(string) int) string {
	at := 0
	for i, cl := range clusters(line) {
		if at >= off {
			return strings.Join(clusters(line)[i:], "")
		}
		at += width(cl)
	}
	return ""
}

// Cursor is the open field's caret (tui.CursorReporter), so a terminal shows it where the text is
// typed; none while no field is open.
func (t *DataTable) Cursor() (int, int, bool) {
	if t.caret == nil || t.core.Editing() == nil {
		return 0, 0, false
	}
	return t.caret.X, t.caret.Y, true
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
	case tui.PasteEvent:
		return t.core.Paste(e.Text)
	case tui.MouseEvent:
		p, placed := t.layout.(TableLayoutPointer)
		if t.bound() && !placed {
			return false
		}
		switch {
		case e.Kind == tui.MouseWheel && (e.Button == tui.WheelLeft || e.Button == tui.WheelRight ||
			e.Mods&tui.ModShift != 0 && (e.Button == tui.WheelUp || e.Button == tui.WheelDown)):
			n := 1 // Shift+wheel and a horizontal wheel move the fields, a field a step
			if e.Button == tui.WheelLeft || e.Button == tui.WheelUp {
				n = -1
			}
			t.core.ScrollFields(n)
			return true
		case e.Kind == tui.MouseWheel && (e.Button == tui.WheelUp || e.Button == tui.WheelDown):
			step := tableWheelRows
			if e.Button == tui.WheelUp {
				step = -step
			}
			if t.bound() {
				p.ScrollRows(step)
			} else {
				t.top = clampTop(t.top+step, t.core.Len(), t.bodyRows())
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
			if hit == TableHitCell && t.core.pairPress(e.Count, pos) {
				t.core.dropPending()
				if t.core.Editable() { // an editable table's double click edits the cell
					t.core.EditCell("")
				} else {
					t.core.activateCursor()
				}
			}
			return true
		case e.Kind == tui.MousePress && e.Button == tui.MouseRight && t.menu != nil && t.menu.Enabled():
			// Inside the selection it is left as it is, for the menu's rows to act on; outside, what
			// was pressed is selected first (selectForMenu).
			t.menuPos, t.menuHit = t.hitAt(e)
			if t.unplacedHeader(t.menuHit) {
				t.menuPos, t.menuHit = TablePos{}, TableHitNone
			}
			t.core.selectForMenu(t.menuPos, t.menuHit)
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
	return TablePos{Row: t.top + e.Y - tableHeaderRows, Col: col}, true
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
	v := t.top + y - tableHeaderRows
	if y < tableHeaderRows || v >= t.core.DrawnRows() { // the rule under the titles is no hit
		return TablePos{}, TableHitNone
	}
	kind, row := t.core.DrawnRow(v)
	switch {
	case kind == TableRowPending:
		return TablePos{}, TableHitNone
	case x < g:
		if kind == TableRowEdge {
			return TablePos{}, TableHitNone
		}
		return TablePos{Row: row}, TableHitGutter
	}
	if col, ok := t.fieldAt(x - g); ok {
		return TablePos{Row: row, Col: col}, TableHitCell
	}
	if at, ok := t.fieldX(len(t.core.Fields()), g); ok && t.core.EdgeField() && x >= at && x < at+tableEdgeWidth {
		return TablePos{Row: row, Col: len(t.core.Fields())}, TableHitCell
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
	x += t.gutter() // spans count the gutter
	for _, sp := range t.spans(math.MaxInt, false) {
		if x < sp.x+sp.w {
			return sp.col, x >= sp.x || sp.col > t.left // a gap is the field after it
		}
	}
	return 0, false
}

// KeyScope is the table's key-map scope.
func (t *DataTable) KeyScope() keymap.Scope { return TableScope }

// KeyBindings is the table's commands as m binds them (tui.KeyBinder). The digits of a count are its
// grammar, not commands.
func (t *DataTable) KeyBindings(m *keymap.Map) []keymap.Binding {
	return tui.BindingsOf(m, tableCommands...)
}
