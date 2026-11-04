package mermaid

import (
	"context"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/graph/layout"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
)

// A timeline and a journey are lists down a spine: a column of heading boxes (a timeline's
// sections and periods, a journey's sections) left of one vertical line, and each heading's items
// boxed to its right (a period's events, a section's tasks), a note after each (a task's score
// and actors). Every box joins the spine at its middle row, so the spine reads ┼ where a heading
// has items, ├ ┤ where it has none or an item has no heading. A timeline's spine is its time axis,
// and ends in ▼.

// spineGroup is one heading and its items; head is nil for items with no heading (a journey's
// tasks before any section).
type spineGroup struct {
	head  *spineBox
	items []spineBox
}

// spineBox is one box of a spine list and the note after it.
type spineBox struct {
	label string
	form  Form
	note  string
}

// spineMaxBoxes bounds a spine list's boxes: past it Lay answers layout.ErrTooLarge.
const spineMaxBoxes = 2000

// laySpine lays the groups out down one spine, title above; arrow ends the spine in ▼. Each
// heading takes its rows, or its items' if more (a blank row between items), and a blank row
// follows each group, so no two boxes touch.
func laySpine(ctx context.Context, title string, groups []spineGroup, arrow bool, m Measurer) (*Grid, error) {
	g := newGrid()
	n := 0
	for _, gr := range groups {
		n += len(gr.items)
		if gr.head != nil {
			n++
		}
	}
	if n > spineMaxBoxes {
		return nil, layout.ErrTooLarge
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	colW := 0 // the headings' column
	for _, gr := range groups {
		if gr.head != nil {
			w, _ := formSize(gr.head.form, m.StringWidth(gr.head.label))
			colW = max(colW, w)
		}
	}
	spine := colW + 1 // a cell of line between the widest heading and the spine
	if colW == 0 {
		spine = 0
	}
	itemX := spine + 2 // a cell of line between the spine and an item
	y := 0
	if title != "" {
		g.label(0, 0, title, m)
		y = 2
	}
	first, last := -1, -1
	joined := func(row int) {
		if first < 0 {
			first = row
		}
		last = row
	}
	for _, gr := range groups {
		top, h := y, 0
		if gr.head != nil {
			w, bh := formSize(gr.head.form, m.StringWidth(gr.head.label))
			r := tui.Rect{X: 0, Y: top, W: w, H: bh}
			g.Boxes = append(g.Boxes, Box{Label: gr.head.label, Rect: r, Form: gr.head.form})
			mid := r.Y + r.H/2
			g.run(cell{w, mid}, cell{spine, mid})
			g.lines[cell{w, mid}] |= west // into the heading's side
			joined(mid)
			h = bh
		}
		iy := top
		for _, it := range gr.items {
			lw := m.StringWidth(it.label)
			w, bh := formSize(it.form, lw)
			r := tui.Rect{X: itemX, Y: iy, W: w, H: bh}
			g.Boxes = append(g.Boxes, Box{Label: it.label, Rect: r, Form: it.form})
			mid := r.Y + r.H/2
			g.run(cell{spine, mid}, cell{itemX - 1, mid})
			g.lines[cell{itemX - 1, mid}] |= east // into the item's side
			joined(mid)
			g.label(itemX+w+1, mid, it.note, m)
			h = max(h, iy+bh-top)
			iy += bh + 1
		}
		if h > 0 {
			y = top + h + 1
		}
	}
	if first >= 0 {
		g.run(cell{spine, first}, cell{spine, last})
		if arrow {
			g.link(cell{spine, last}, cell{spine, last + 1})
			g.marks[cell{spine, last + 1}] = "▼"
		}
	}
	g.settle()
	if g.tooLarge() {
		return nil, layout.ErrTooLarge
	}
	return g, nil
}

// LayTimeline lays d out down its time axis: a section's title boxed on the axis, heavy, each
// period after it boxed on the axis too, and the period's events boxed to the axis's right, one
// under another; the title above. layout.ErrTooLarge for one past the limits.
func LayTimeline(ctx context.Context, d *pm.TimelineDiagram, m Measurer) (*Grid, error) {
	var groups []spineGroup
	for _, sec := range d.Sections {
		if sec.Title != "" {
			groups = append(groups, spineGroup{head: &spineBox{label: sec.Title, form: FormBang}})
		}
		for _, p := range sec.Periods {
			gr := spineGroup{head: &spineBox{label: p.Label, form: FormSquare}}
			for _, e := range p.Events {
				gr.items = append(gr.items, spineBox{label: e.Text, form: FormRounded})
			}
			groups = append(groups, gr)
		}
	}
	return laySpine(ctx, d.Title, groups, true, m)
}

// journeyScoreMax is a journey task's best score.
const journeyScoreMax = 5

// LayJourney lays d out down one spine: each section boxed on it, heavy, its tasks boxed to its
// right, each followed by its score (●●●○○ 3) and who took it; the title above.
// layout.ErrTooLarge for one past the limits.
func LayJourney(ctx context.Context, d *pm.JourneyDiagram, m Measurer) (*Grid, error) {
	var groups []spineGroup
	for _, sec := range d.Sections {
		gr := spineGroup{}
		if sec.Name != "" {
			gr.head = &spineBox{label: sec.Name, form: FormBang}
		}
		for _, t := range sec.Tasks {
			gr.items = append(gr.items, spineBox{label: t.Name, form: FormSquare, note: journeyNote(t)})
		}
		groups = append(groups, gr)
	}
	return laySpine(ctx, d.Title, groups, false, m)
}

// journeyNote is a task's score as a bar of five, the score, and its actors.
func journeyNote(t pm.JourneyTask) string {
	s := min(max(t.Score, 0), journeyScoreMax)
	note := strings.Repeat("●", s) + strings.Repeat("○", journeyScoreMax-s) + " " + strconv.Itoa(t.Score)
	if len(t.Actors) > 0 {
		note += "  " + strings.Join(t.Actors, ", ")
	}
	return note
}
