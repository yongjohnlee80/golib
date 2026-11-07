package mermaid

import (
	"context"
	"image/color"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	tuimermaid "github.com/yongjohnlee80/golib/tui/mermaid"
)

func init() { layers[pm.GitGraph] = layGitGraph }

// layGitGraph lays a gitGraph out: a lane per branch, its name at the lane's start, and a step per
// commit along the time axis (across, LR; down, TB; up, BT), in source order. A commit is a dot on
// its branch's lane in the lane's colour: a square for HIGHLIGHT, a crossed ring for REVERSE, a
// double ring for a merge, a ring for a cherry-pick (with a dotted line from its source). Each
// parent's line runs along a lane and turns into the other: a branch's first commit drops from its
// parent's step, and a merge runs along the merged lane to the merge's step. A tag sits beside its
// commit, an id given in the source under it.
func layGitGraph(ctx context.Context, d pm.Diagram, _ float32, th Theme, m gui.Measurer) (*Laid, error) {
	gd := d.(*pm.GitGraphDiagram)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := th.Font
	em := f.Size
	small := f
	small.Size *= 0.85
	l := &Laid{th: th}
	margin, step, laneGap, dot := em, em*3, em*3, em*0.9

	lanes := tuimermaid.GitLanes(gd)
	vertical := gd.Dir == pm.TB || gd.Dir == pm.BT
	// the branches' names, at each lane's start
	names := make([]label, len(gd.Branches))
	var nameRoom float32
	for i, b := range gd.Branches {
		names[i] = newLabel(m, b.Name, f, 0, th.Text)
		if vertical {
			nameRoom = max(nameRoom, names[i].box.H)
		} else {
			nameRoom = max(nameRoom, names[i].box.W)
		}
	}
	// the tags and ids, measured: the room they need beside the lanes
	tags := make([]*label, len(gd.Commits))
	ids := make([]*label, len(gd.Commits))
	for i, c := range gd.Commits {
		var side float32 // TB, BT: the labels' width beside the dot
		if c.Tag != "" {
			t := newLabel(m, c.Tag, small, 0, th.Text)
			tags[i] = &t
			step = max(step, t.box.W+em)
			side += t.box.W + em
		}
		if c.ID != "" {
			t := newLabel(m, c.ID, small, 0, th.Text)
			ids[i] = &t
			step = max(step, t.box.W+em)
			side += t.box.W + em*0.4
		}
		if vertical {
			// the labels sit right of the dot, so a lane is as wide as them; along the time axis,
			// a step only needs a line's height
			laneGap = max(laneGap, dot+side+em*1.5)
		}
	}
	if vertical {
		step = em * 2.4
	}
	steps := len(gd.Commits)
	timeStart := margin + nameRoom + em
	timeLen := float32(max(steps, 1)) * step
	laneStart := margin + em*2 // room for a tag above the first lane
	// at maps a step along the time axis and a lane to a point
	at := func(s, lane int) gui.Point {
		t := timeStart + (float32(s)+0.5)*step
		if gd.Dir == pm.BT {
			t = timeStart + timeLen - (float32(s)+0.5)*step
		}
		c := laneStart + float32(lane)*laneGap
		if vertical {
			return gui.Pt(c, t)
		}
		return gui.Pt(t, c)
	}
	laneColor := func(b int) color.NRGBA { return sliceColor(th, lanes[b], len(gd.Branches)) }

	// each lane: from its first step to the graph's end, under the commits
	for b := range gd.Branches {
		first := -1
		for i, c := range gd.Commits {
			if c.Branch == b {
				first = i
				break
			}
		}
		if first < 0 {
			continue // a branch with no commits draws only its name
		}
		from := first
		if ps := gd.Commits[first].Parents; len(ps) > 0 {
			from = ps[0]
		}
		a, z := at(from, lanes[b]), at(steps-1, lanes[b])
		l.edges = append(l.edges, laidEdge{pts: []gui.Point{a, z}, line: pm.Dotted, color: gui.Mix(th.Background, laneColor(b), 0.45)})
	}
	// each parent's line, in its branch's colour
	for i, c := range gd.Commits {
		p := at(i, lanes[c.Branch])
		for k, par := range c.Parents {
			pc := gd.Commits[par]
			q := at(par, lanes[pc.Branch])
			var pts []gui.Point
			switch {
			case pc.Branch == c.Branch:
				pts = []gui.Point{q, p}
			case k == 0: // a branch's first commit: down from its parent's step, then along its lane
				corner := at(par, lanes[c.Branch])
				pts = []gui.Point{q, corner, p}
			default: // a merge: along the merged lane to the merge's step, then across
				corner := at(i, lanes[pc.Branch])
				pts = []gui.Point{q, corner, p}
			}
			color := laneColor(c.Branch)
			if k > 0 {
				color = laneColor(pc.Branch)
			}
			l.edges = append(l.edges, laidEdge{pts: pts, line: pm.Thick, color: color})
		}
		if c.Picked >= 0 {
			src := gd.Commits[c.Picked]
			l.edges = append(l.edges, laidEdge{pts: []gui.Point{at(c.Picked, lanes[src.Branch]), p}, line: pm.Dotted, head: endArrow, color: th.Line})
		}
	}
	// the commits, over the lines
	for i, c := range gd.Commits {
		p := at(i, lanes[c.Branch])
		fill := laneColor(c.Branch)
		box := gui.Rect{X: p.X - dot/2, Y: p.Y - dot/2, W: dot, H: dot}
		n := laidNode{form: formStart, box: box, fill: fill, stroke: fill, width: 1}
		switch {
		case c.Merge:
			n = laidNode{form: formEnd, box: grow(box, dot*0.2), fill: fill, stroke: fill, width: 2}
		case c.Picked >= 0:
			n = laidNode{form: formCircle, box: box, fill: th.Background, stroke: fill, width: 2}
		case c.Type == pm.GitHighlight:
			n = laidNode{form: formRect, box: box, fill: fill, stroke: th.Text, width: 1}
		case c.Type == pm.GitReverse:
			n = laidNode{form: formCircle, box: box, fill: th.Background, stroke: fill, width: 2}
			l.edges = append(l.edges,
				laidEdge{pts: []gui.Point{{X: box.X + dot*0.25, Y: box.Y + dot*0.25}, {X: box.X + dot*0.75, Y: box.Y + dot*0.75}}, color: fill},
				laidEdge{pts: []gui.Point{{X: box.X + dot*0.75, Y: box.Y + dot*0.25}, {X: box.X + dot*0.25, Y: box.Y + dot*0.75}}, color: fill})
		}
		l.nodes = append(l.nodes, n)
		x := p.X + dot // TB, BT: the next label's left, right of the dot (off the lane's line)
		if t := tags[i]; t != nil {
			tx, ty := p.X-t.box.W/2, p.Y-dot-t.box.H-em*0.1 // LR: over the dot
			if vertical {
				tx, ty = x+em*0.3, p.Y-t.box.H/2
				x += t.box.W + em
			}
			l.nodes = append(l.nodes, laidNode{form: formRound, box: gui.Rect{X: tx - em*0.3, Y: ty - em*0.1, W: t.box.W + em*0.6, H: t.box.H + em*0.2},
				fill: th.NodeFill, stroke: th.NodeStroke, width: 1, label: t.at(tx, ty)})
		}
		if id := ids[i]; id != nil {
			if vertical {
				l.texts = append(l.texts, id.at(x+em*0.4, p.Y-id.box.H/2))
			} else {
				l.texts = append(l.texts, id.at(p.X-id.box.W/2, p.Y+dot))
			}
		}
	}
	// the names, at each lane's start
	for b, nm := range names {
		c := laneStart + float32(lanes[b])*laneGap
		if vertical {
			l.texts = append(l.texts, nm.at(c-nm.box.W/2, margin))
			if gd.Dir == pm.BT {
				l.texts[len(l.texts)-1] = nm.at(c-nm.box.W/2, timeStart+timeLen+em*0.5)
			}
		} else {
			l.texts = append(l.texts, nm.at(margin, c-nm.box.H/2))
		}
	}
	across := laneStart + float32(max(len(gd.Branches)-1, 0))*laneGap + em*2
	along := timeStart + timeLen + margin
	if gd.Dir == pm.BT {
		along += nameRoom + em
	}
	if vertical {
		l.size = gui.Size{W: max(across, em*4), H: along}
	} else {
		l.size = gui.Size{W: along, H: max(across, em*4)}
	}
	return l, nil
}

// grow is r grown by d on every side.
func grow(r gui.Rect, d float32) gui.Rect {
	return gui.Rect{X: r.X - d, Y: r.Y - d, W: r.W + 2*d, H: r.H + 2*d}
}
