package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TableColumn describes one column of a Table: a header title, a width, and
// the cell extractor for a row item.
type TableColumn[T any] struct {
	// Title is rendered in the header row.
	Title string
	// Width is the fixed cell width in columns. 0 marks the FLEX column: it
	// receives whatever width remains after the fixed columns and gaps
	// (minimum flexMinWidth). At most one flex column is honored; extra
	// zero-width columns fall back to flexMinWidth fixed.
	Width int
	// Cell extracts the cell text for an item.
	Cell func(T) string
}

const (
	tableGap     = 2 // spaces between columns
	flexMinWidth = 8
)

// Table is a List with column structure: a one-row header (muted, underlined)
// above a cursor-driven row list (ADR-0007 List semantics — ↑/↓, paging,
// selection, empty text). Rows are formatted from TableColumn cells, padded
// and truncated to the per-layout resolved column widths, so the header and
// the cells stay aligned at any terminal size.
//
// The embedded List remains the focusable/scrolling widget; Table is the
// container that owns the header and the column geometry. List options
// (WithEmptyText, WithListStyles, …) pass through NewTable; do not pass
// WithItems/WithSource — the row renderer is the Table's.
type Table[T any] struct {
	Base
	cols   []TableColumn[T]
	list   *List[T]
	widths []int // resolved at Layout for the current width
	headSt style.Style
}

// NewTable builds a table from column definitions. At least one column is
// required.
func NewTable[T any](cols []TableColumn[T], opts ...ListOption[T]) *Table[T] {
	if len(cols) == 0 {
		panic("widget: NewTable requires at least one column")
	}
	t := &Table[T]{
		cols:   cols,
		headSt: style.New().Foreground(style.TokenTextMuted).Bold(true).Underline(true),
	}
	all := append([]ListOption[T]{WithItems[T](nil, t.renderRow)}, opts...)
	t.list = NewList(all...)
	t.widths = make([]int, len(cols))
	return t
}

// List exposes the inner row list (focus target, Selected, SetItems…).
func (t *Table[T]) List() *List[T] { return t.list }

// SetItems replaces the rows.
func (t *Table[T]) SetItems(items []T) { t.list.SetItems(items) }

// Selected returns the cursor index; ok is false while the table is empty.
func (t *Table[T]) Selected() (int, bool) { return t.list.Selected() }

// Init mounts the row list.
func (t *Table[T]) Init(ctx *tui.Context) {
	t.Base.Init(ctx)
	ctx.Mount(t.list)
}

// resolveWidths distributes w over the columns: fixed widths as declared, the
// first zero-width column takes the remainder.
func (t *Table[T]) resolveWidths(w int) {
	fixed, flexIdx := 0, -1
	for i, c := range t.cols {
		if c.Width == 0 && flexIdx < 0 {
			flexIdx = i
			continue
		}
		width := c.Width
		if width == 0 {
			width = flexMinWidth
		}
		t.widths[i] = width
		fixed += width
	}
	if flexIdx >= 0 {
		rem := w - fixed - tableGap*(len(t.cols)-1)
		t.widths[flexIdx] = max(rem, flexMinWidth)
	}
}

// renderRow formats one item's cells to the resolved widths.
func (t *Table[T]) renderRow(item T) string {
	row := ""
	for i, c := range t.cols {
		if i > 0 {
			row += "  "
		}
		row += pad(c.Cell(item), t.widths[i])
	}
	return row
}

// pad truncates/pads s to exactly w cells (byte-width approximation like the
// %-*s formatting it replaces; the List truncates display overflow cleanly).
func pad(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > w {
		if w <= 1 {
			return string(r[:w])
		}
		return string(r[:w-1]) + "…"
	}
	return fmt.Sprintf("%-*s", w+(len(s)-len(string(r))), s)
}

// Layout gives the header one row and the list the rest.
func (t *Table[T]) Layout(c tui.Constraints) tui.Size {
	w := boundedMax(c.MaxW, max(c.MinW, 1))
	h := boundedMax(c.MaxH, max(c.MinH, 1))
	t.resolveWidths(w)
	if lh := max(h-1, 0); lh > 0 {
		t.Context().LayoutChild(t.list, tui.Tight(tui.Size{W: w, H: lh}))
		t.Context().PlaceChild(t.list, tui.Rect{X: 0, Y: 1, W: w, H: lh})
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

// Render paints the header row; the list renders itself below.
func (t *Table[T]) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	x := 0
	for i, c := range t.cols {
		if i > 0 {
			x += tableGap
		}
		if x >= sz.W {
			break
		}
		drawText(s, x, 0, truncate(pad(c.Title, t.widths[i]), sz.W-x, s.StringWidth), t.headSt)
		x += t.widths[i]
	}
}

// HandleEvent forwards to the row list so navigation keys work when they are
// delivered to the Table (e.g. a controller forwarding ↑/↓).
func (t *Table[T]) HandleEvent(ev tui.Event) bool { return t.list.HandleEvent(ev) }
