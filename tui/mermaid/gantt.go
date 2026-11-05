package mermaid

import (
	"context"
	"math"
	"time"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { register(pm.Gantt, layAs(LayGantt), nil) }

// A gantt chart in cells is rows: its sections' titles and its tasks' names in a column on the
// left, then each task's bar along a day axis under the axis's dates. A task's state is its bar's
// glyph, not a colour, so it reads in every theme, mono included: █ a task, ▓ active, ░ done, ◆ a
// milestone, and ! before a crit task's bar.
//
//	A Gantt Diagram
//
//	                   01-01     01-15     01-29
//	                   ┬─────────┬─────────┬──────
//	Section            ┊         ┊         ┊
//	  A task           ███████████████     ┊
//	  Another task     ┊         ┊    ██████████
//	Another
//	  Ship             ┊         ┊         ┊   ◆

// Bars, by a task's state.
const (
	ganttBar       = "█"
	ganttActive    = "▓"
	ganttDone      = "░"
	ganttMilestone = "◆"
	ganttCritMark  = "!"
	ganttGrid      = "┊"
)

// ganttMaxRows bounds a chart's rows: past it Lay answers layout.ErrTooLarge.
const ganttMaxRows = 2000

// LayGantt lays d out in rows: a section's title on a row of its own, each of its tasks indented
// under it, the task's name a box (FormText, its ID the task's ID, else its name) a host selects,
// and its bar to the right along the day axis, the chart 30 to 100 columns wide; the axis's dates
// and their ticks above; the title above all. layout.ErrTooLarge for one past the limits.
func LayGantt(ctx context.Context, d *pm.GanttDiagram, m Measurer) (*Grid, error) {
	rows := 0
	for _, s := range d.Sections {
		rows += len(s.Tasks)
		if s.Title != "" {
			rows++
		}
	}
	if err := guard(ctx, rows, ganttMaxRows); err != nil {
		return nil, err
	}
	g := newGrid()
	start, end, ok := d.Range()
	if !ok {
		start, end = time.Time{}, time.Time{}.AddDate(0, 0, 1)
	}
	// the names' column, each name capped as a label: section titles at its left, tasks indented two
	nameW := 0
	for _, s := range d.Sections {
		nameW = max(nameW, widest(m, s.Title))
		for _, t := range s.Tasks {
			nameW = max(nameW, 2+widest(m, t.Name))
		}
	}
	chartX := nameW + 3 // room for a crit mark between a name and a bar at the chart's first day
	days := max(end.Sub(start).Hours()/24, 1)
	chartW := min(max(int(days)*2, 30), 100)
	// col is a day's column: the chart's first day at chartX, its end at its last column
	col := func(t time.Time) int {
		return chartX + int(math.Round(t.Sub(start).Hours()/24/days*float64(chartW-1)))
	}

	y := g.title(d.Title, m)
	// the axis: each tick's date over its ┬, as many as leave a gap after each date
	labelW := m.StringWidth(d.AxisLabel(start)) + 3
	ticks := d.Ticks(max(chartW/labelW, 1))
	axisY := y + 1
	g.run(cell{chartX, axisY}, cell{chartX + chartW - 1, axisY})
	var tickCols []int
	for _, t := range ticks {
		x := col(t)
		tickCols = append(tickCols, x)
		g.label(x, y, d.AxisLabel(t), m)
		g.lines[cell{x, axisY}] |= south
	}
	// a ┬ needs a cell under it to point at: the grid's dotted line starts there
	y = axisY + 1

	for _, s := range d.Sections {
		if s.Title != "" {
			g.label(0, y, s.Title, m)
			g.gridRow(y, tickCols)
			y++
		}
		for _, t := range s.Tasks {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			id := t.ID
			if id == "" {
				id = t.Name
			}
			g.box(Box{ID: id, Label: t.Name, Form: FormText}, 2, y, m)
			g.gridRow(y, tickCols)
			x0 := col(t.Start)
			switch {
			case t.Milestone:
				g.fills[cell{x0, y}] = ganttMilestone
			default:
				glyph := ganttBar
				switch {
				case t.Done:
					glyph = ganttDone
				case t.Active:
					glyph = ganttActive
				}
				x1 := max(col(t.End), x0+1) // a bar is a cell at least, its end exclusive
				for x := x0; x < x1; x++ {
					g.fills[cell{x, y}] = glyph
				}
			}
			if t.Crit {
				g.fills[cell{x0 - 1, y}] = ganttCritMark
			}
			y++
		}
	}
	return g.finish(m)
}

// gridRow marks row y's grid cells, under each tick, dotted: a bar painted later covers them.
func (g *Grid) gridRow(y int, cols []int) {
	for _, x := range cols {
		g.marks[cell{x, y}] = ganttGrid
	}
}
