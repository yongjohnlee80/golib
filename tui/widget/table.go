package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TableColumn describes one column of a [Table]: a header title, a width specification,
// and a cell extractor function for row items.
type TableColumn[T any] struct {
	// Title is rendered in the header row.
	Title string

	// Width is the fixed cell width in monospace columns.
	//
	// Setting Width = 0 designates a FLEX column:
	// The space remaining after fixed columns and inter-column gaps are subtracted
	// is divided EVENLY among all flex columns (subject to flexMinWidth = 8). Any integer
	// division remainder is assigned to the leftmost flex columns.
	//
	// Consequently, a table configured with all-flex columns divides the terminal width
	// equally among them, rather than allowing the first column to monopolize the row.
	Width int

	// Cell extracts the display string for this column from an item of type T.
	Cell func(T) string
}

const (
	tableGap     = 2 // horizontal space (cells) between adjacent columns
	flexMinWidth = 8 // minimum width (cells) guaranteed to any flex column
)

// Table provides a column-structured tabular view atop a virtualized [List].
// It pairs a fixed one-row header (styled muted, bold, and underlined) above a
// cursor-driven row list supporting keyboard navigation (↑/↓, PgUp/PgDn, Home/End),
// mouse selection, and empty-state placeholders.
//
// # What Table Solves
//
// Displaying tabular records in a terminal requires addressing three recurring challenges:
//  1. Column Alignment across Window Resizes: Table resolves column widths on every
//     Layout pass, dynamically recalculating flex widths so headers and cells remain
//     pixel-perfectly aligned across terminal resizing.
//  2. Eliminating Code Duplication: Instead of re-implementing scrolling, cursor tracking,
//     paging, and activation logic, Table embeds and mounts a standard [List], reusing
//     all list navigation mechanics while owning column formatting.
//  3. Renderer Hijacking Protection: Table ensures its internal column-formatting renderer
//     cannot be accidentally overridden by caller-provided ListOptions.
//
// # Column Width Resolution Model
//
// At each Layout pass with available width W:
//
//		 ┌───────────────┬───────────────────────────────┬───────────────────────────────┐
//		 │ Col 0 (Fixed) │         Col 1 (Flex)          │         Col 2 (Flex)          │
//		 │    Width=8    │   Width = share + remainder   │         Width = share         │
//		 └───────────────┴───────────────────────────────┴───────────────────────────────┘
//		        ▲                        ▲                               ▲
//		        │                        │                               │
//		        └── tableGap (2 spaces) ─┴───── tableGap (2 spaces) ─────┘
//
//	 1. Fixed Deduction: Fixed columns (Width > 0) and inter-column gaps
//	    (tableGap = 2 cells) are deducted from available width W.
//	 2. Flex Division: Remaining space is divided evenly across all flex columns (Width == 0),
//	    clamped to flexMinWidth (8 cells).
//	 3. Remainder Allocation: Any integer remainder from integer division is distributed to
//	    the leftmost flex columns (one extra cell each), guaranteeing complete horizontal fill
//	    without edge jitter.
//
// # Architectural Invariants
//
//  1. Column Alignment Determinism:
//     Headers and body cells use identical resolved widths computed in [Table.resolveWidths].
//     Width adjustments across window resizing preserve proportional flex shares without drift.
//  2. Header Invariance to Mouse Interaction:
//     Mouse presses on row 0 (the header) are strictly inert and never select a row.
//     Mouse wheel events on the header are transparently forwarded to scroll the underlying list.
//  3. Non-Swallowing Pointer Pass-Through:
//     Mouse motion and release events are never swallowed as sinks, allowing ancestor splitters
//     or drag handles to complete drags across table boundaries smoothly.
//
// # Concurrency & Goroutine Ownership
//
// Table and its inner [List] are loop-goroutine-owned. State modifications ([Table.SetItems],
// [Table.Selected]) must execute on the main application loop goroutine.
//
// # Usage Example
//
//	type Process struct {
//		PID    int
//		User   string
//		CPU    float64
//		Memory string
//		Cmd    string
//	}
//
//	cols := []widget.TableColumn[Process]{
//		{Title: "PID", Width: 8, Cell: func(p Process) string { return strconv.Itoa(p.PID) }},
//		{Title: "USER", Width: 12, Cell: func(p Process) string { return p.User }},
//		{Title: "CPU%", Width: 8, Cell: func(p Process) string { return fmt.Sprintf("%.1f", p.CPU) }},
//		{Title: "MEM", Width: 8, Cell: func(p Process) string { return p.Memory }},
//		{Title: "COMMAND", Width: 0, Cell: func(p Process) string { return p.Cmd }}, // flex
//	}
//
//	table := widget.NewTable(cols,
//		widget.WithEmptyText("No active processes"),
//	)
//	table.SetItems(runningProcesses)
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
// Pointer events are NOT forwarded wholesale. Under 's
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
