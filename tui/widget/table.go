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
	// Width is the fixed cell width in columns. 0 marks a FLEX column:
	// the width remaining after the fixed columns and gaps is shared
	// EVENLY among all flex columns (each at least flexMinWidth; the
	// division remainder goes to the leftmost ones). A table of all-flex
	// columns therefore renders equal-width columns rather than letting
	// the first one swallow the row.
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
	// The COLUMNS render a table's rows, always. A caller passing
	// WithItems/WithSource is supplying DATA, and its render func would
	// otherwise silently replace the column renderer — producing column
	// headers above rows drawn some other way (autodb M6: a history
	// table that showed the raw script under every header).
	t.list.render = t.renderRow
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

// resolveWidths distributes w over the columns: fixed widths as declared,
// the remainder shared evenly among every flex (zero-width) column.
func (t *Table[T]) resolveWidths(w int) {
	fixed := 0
	var flex []int
	for i, c := range t.cols {
		if c.Width == 0 {
			flex = append(flex, i)
			continue
		}
		t.widths[i] = c.Width
		fixed += c.Width
	}
	if len(flex) == 0 {
		return
	}
	rem := w - fixed - tableGap*(len(t.cols)-1)
	share := max(rem/len(flex), flexMinWidth)
	extra := 0
	if rem > 0 && share*len(flex) <= rem {
		extra = rem - share*len(flex) // spread the division remainder
	}
	for n, i := range flex {
		t.widths[i] = share
		if n < extra {
			t.widths[i]++
		}
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

// HandleEvent forwards KEYS to the row list so navigation works when they are
// delivered to the Table (e.g. a controller forwarding ↑/↓).
//
// Pointer events are NOT forwarded wholesale (ADR-0010 §2.2). Under ADR-0004's
// target-first routing the row list is a placed child at local Y:1, so a press in
// the body reaches it directly with correct local coordinates and never arrives
// here. What does arrive here is a press on the HEADER row, and forwarding that
// unchanged passed Table-local Y=0 to the list, which read it as body row `top+0`
// and moved the selection — clicking a column title silently jumped the cursor to
// the first visible row. That behaviour was determinate, not ambiguous, which is
// what made it look intentional.
//
// So a header press is INERT, and consumed rather than bubbled: this rectangle
// belongs to the Table, which chooses to do nothing with it today and is where
// column sorting will land. Bubbling it would let an ancestor act on a click the
// user aimed at a column title.
//
// That ownership covers the header PRESS and nothing else. Motion, release and
// non-left presses over the body are declined by the row list and must continue
// to an ancestor — a Split dragging its divider across a Table depends on it.
//
// The WHEEL is the exception and is forwarded: the header is part of the same
// scrollable surface as far as the reader is concerned, so scrolling over it
// scrolls the rows. The list's wheel handling ignores coordinates entirely, so
// forwarding a header-local event is safe.
func (t *Table[T]) HandleEvent(ev tui.Event) bool {
	if m, ok := ev.(tui.MouseEvent); ok {
		switch {
		case m.Kind == tui.MouseWheel:
			return t.list.HandleEvent(m)
		case m.Kind == tui.MousePress && m.Y == 0:
			// The header row, and ONLY it: inert and ours.
			return true
		}
		// Everything else must keep bubbling. Consuming all non-wheel pointer
		// events made the Table a sink: the row list declines body motion and
		// release (and right/middle presses), which then have to reach an
		// ANCESTOR. A Split whose divider drag continues over a Table body needs
		// exactly those, and swallowing the release left it dragging forever, so a
		// later unpressed motion resized it. Ownership is the header press, not
		// the pointer.
		return false
	}
	return t.list.HandleEvent(ev)
}
