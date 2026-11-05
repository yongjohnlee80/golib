package mermaid

import (
	"context"
	"time"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// init registers the gantt chart layout builder with the mermaid layout engine.
func init() { layers[pm.Gantt] = layGantt }

// Gantt Layout Architecture:
//
//	                 Title
//	          | 01-01   | 01-15   | 01-29     <- the axis: a tick's date over its grid line
//	+---------+---------+---------+------------+
//	| Section | Task a  [=========]            |  <- a band per titled section, behind its rows
//	|         | Task b            [====]       |
//	+---------+---------+---------+------------+
//	| Other   | Ship              <>           |  <- a milestone: a diamond on its day
//	+---------+---------+---------+------------+
//
// layGantt lays a gantt chart out in rows: its sections' titles in a column on the left, its tasks'
// names in the next, then each task's bar from its first day to its end along a day axis, under
// the axis's dates and their grid lines; a titled section is a band behind its rows; the title over
// all.
func layGantt(ctx context.Context, d pm.Diagram, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	gd := d.(*pm.GanttDiagram)
	f := th.Font
	em := f.Size
	small := f
	small.Size *= 0.85
	bold := f
	bold.Bold = true
	l := &Laid{th: th}
	margin := em
	padX := em * 0.6
	rowH := f.Size*gui.LineHeight + em*0.7

	// the two text columns: section titles, task names, each as wide as its widest
	var secW, nameW float32
	for _, s := range gd.Sections {
		if s.Title != "" {
			secW = max(secW, newLabel(m, s.Title, bold, 0, th.Text).box.W+2*padX)
		}
		for _, t := range s.Tasks {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			nameW = max(nameW, newLabel(m, t.Name, f, 0, th.Text).box.W+2*padX)
		}
	}
	y := margin
	title, room, _ := titleLabel(m, gd.Title, th)
	y += room

	start, end, ok := gd.Range()
	days := float32(1)
	if ok {
		days = max(float32(end.Sub(start).Hours()/24), 1)
	}
	// the chart is as wide as the width asked leaves, a day at least half an em; the fit scales a
	// longer one down
	chartX := margin + secW + nameW
	chartW := max(width-chartX-margin, em*20, days*em*0.5)
	rowH0 := rowH * 0.62
	// the days sit inset from the chart's edges by half a milestone, so one on the first or last
	// day stays inside its band
	inset := rowH0 * 0.6
	dayW := (chartW - 2*inset) / days
	dayX := func(t time.Time) float32 { return chartX + inset + float32(t.Sub(start).Hours()/24)*dayW }

	// the axis: each tick's date, centred on its day's line, no two closer than their widths
	axisY := y
	axisH := small.Size*gui.LineHeight + em*0.4
	rowsTop := axisY + axisH
	n := int(chartW / (em * 6))
	ticks := gd.Ticks(max(n, 2))
	rows := 0
	for _, s := range gd.Sections {
		rows += len(s.Tasks)
	}
	rowsBottom := rowsTop + float32(rows)*rowH
	grid := gui.Mix(th.Line, th.Background, 0.7)
	for _, t := range ticks {
		x := dayX(t)
		lb := newLabel(m, gd.AxisLabel(t), small, 0, th.Text)
		l.texts = append(l.texts, lb.at(x-lb.box.W/2, axisY))
		l.edges = append(l.edges, laidEdge{pts: []gui.Point{{X: x, Y: rowsTop}, {X: x, Y: rowsBottom}}, line: pm.Dotted, color: grid})
	}

	// the rows: a band per titled section, its title at its top left; each task's name and its bar
	ry := rowsTop
	barH := rowH0
	for si, s := range gd.Sections {
		top := ry
		for _, t := range s.Tasks {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			name := newLabel(m, t.Name, f, 0, th.Text)
			name.left = true
			l.texts = append(l.texts, name.at(margin+secW+padX, ry+(rowH-name.box.H)/2))
			id := t.ID
			if id == "" {
				id = t.Name
			}
			// the theme's own colours only, so a bar reads in every theme: done is faded towards the
			// page, active deepened towards the outline, crit outlined heavy and filled deepest
			fill, stroke, lw := th.NodeFill, th.NodeStroke, float32(1.2)
			switch {
			case t.Done:
				fill, stroke = gui.Mix(th.NodeFill, th.Background, 0.6), gui.Mix(th.NodeStroke, th.Background, 0.5)
			case t.Active:
				fill = gui.Mix(th.NodeFill, th.NodeStroke, 0.35)
			}
			if t.Crit {
				fill, stroke, lw = gui.Mix(fill, th.NodeStroke, 0.55), th.Text, 2.4
			}
			bar := laidNode{id: id, form: formRound, fill: fill, stroke: stroke, width: lw}
			if t.Milestone {
				side := barH * 1.1
				bar.form = formDiamond
				bar.box = gui.Rect{X: dayX(t.Start) - side/2, Y: ry + (rowH-side)/2, W: side, H: side}
			} else {
				x0, x1 := dayX(t.Start), dayX(t.End)
				bar.box = gui.Rect{X: x0, Y: ry + (rowH-barH)/2, W: max(x1-x0, em*0.3), H: barH}
			}
			l.nodes = append(l.nodes, bar)
			ry += rowH
		}
		if s.Title == "" {
			continue
		}
		fill := th.ClusterFill
		if si%2 == 1 {
			fill = gui.Mix(th.ClusterFill, th.Background, 0.5)
		}
		lb := newLabel(m, s.Title, bold, 0, th.Text)
		lb.left = true
		head := gui.Rect{X: margin, Y: top, W: secW, H: rowH}
		box := gui.Rect{X: margin, Y: top, W: chartX + chartW - margin, H: max(ry-top, rowH)}
		l.group = append(l.group, laidGroup{box: box, fill: fill, stroke: th.ClusterStroke,
			title: lb.at(head.X+padX, head.Y+(head.H-lb.box.H)/2)})
		if len(s.Tasks) == 0 {
			ry += rowH // a section with no task still shows its title
		}
	}

	w := max(chartX+chartW+margin, title.box.W+2*margin, em*4)
	if gd.Title != "" {
		l.texts = append(l.texts, title.at((w-title.box.W)/2, margin))
	}
	l.size = gui.Size{W: w, H: max(ry+margin, em*2)}
	return l, nil
}
