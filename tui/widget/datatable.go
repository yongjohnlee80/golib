package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// DATA TABLE — the terminal view of a TableCore: a header row, then the rows with a cell cursor,
// scrolled both ways to keep the cursor in sight. The core owns the rows, the cursor, the
// selection, the sort and the paging; DataTable paints them and turns presses into cursor moves.
// A GUI view of the same core paints in pixels instead.

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
	styles    DataTableStyles
	emptyText string
	top, left int // first row shown, first field shown
	w, h      int
}

var (
	_ tui.Focusable = (*DataTable)(nil)
	_ TableLayout   = (*DataTable)(nil)
)

// NewDataTable is a view of core.
func NewDataTable(core *TableCore) *DataTable {
	t := &DataTable{core: core, emptyText: "No rows"}
	t.SetStyles(DataTableStyles{})
	return t
}

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

// Init binds the core to this view.
func (t *DataTable) Init(ctx *tui.Context) {
	t.Base.Init(ctx)
	t.core.Bind(ctx, t)
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
		// a new size keeps the cursor in sight; the same size keeps the wheel's scroll
		t.w, t.h = w, h
		t.Reveal(t.core.Cursor().Row, t.core.Cursor().Col)
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

// Render paints the header, then the rows from the first shown.
func (t *DataTable) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
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
// any other pointer event goes on to the ancestors.
func (t *DataTable) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.KeyEvent:
		return t.core.HandleKey(e)
	case tui.MouseEvent:
		switch {
		case e.Kind == tui.MouseWheel && (e.Button == tui.WheelUp || e.Button == tui.WheelDown):
			step := 3
			if e.Button == tui.WheelUp {
				step = -3
			}
			t.top = min(max(t.top+step, 0), max(t.core.Len()-max(t.h-1, 1), 0))
			t.MarkDirty()
			return true
		case e.Kind == tui.MousePress && e.Button == tui.MouseLeft && e.Y == 0:
			return true
		case e.Kind == tui.MousePress && e.Button == tui.MouseLeft:
			col, ok := t.fieldAt(e.X)
			if !ok {
				return true
			}
			t.core.SetCursor(TablePos{Row: t.top + e.Y - 1, Col: col})
			if e.Count >= 2 {
				t.core.HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
			}
			return true
		}
	}
	return false
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
