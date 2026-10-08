package mermaid

import (
	"context"
	"image/color"
	"math"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func init() { layers[pm.Journey] = layJourney }

// layJourney lays a user journey out on one line: the actors' legend at the left, then each
// section's header over its tasks, every task a box with its actors' dots, and its score a face
// under it, joined by a dotted line; an arrow under the faces runs the journey's way.
func layJourney(ctx context.Context, d pm.Diagram, width float32, th Theme, m gui.Measurer) (*Laid, error) {
	jd := d.(*pm.JourneyDiagram)
	em, f := th.Font.Size, th.Font
	l := &Laid{th: th}
	pad := em
	y := pad
	var title *label
	if jd.Title != "" {
		t := newLabel(m, jd.Title, gui.Font{Family: f.Family, Size: em * 1.15, Bold: true}, 0, th.Text)
		title = &t
		y += t.box.H + em
	}

	// the legend: a dot and a name for each actor, in a column at the left
	actor := map[string]color.NRGBA{}
	dot := em * 0.8
	x := pad
	if len(jd.Actors) > 0 {
		var nameW float32
		ly := y
		for i, a := range jd.Actors {
			c := journeyHue(th.NodeStroke, 0.5+float64(i)*0.381966)
			actor[a] = c
			lb := newLabel(m, a, f, 0, th.Text)
			rowH := max(lb.box.H, dot)
			l.nodes = append(l.nodes, laidNode{form: formCircle, box: gui.Rect{X: pad, Y: ly + (rowH-dot)/2, W: dot, H: dot},
				fill: c, stroke: c, width: 1})
			t := lb.at(pad+dot+em*0.5, ly+(rowH-lb.box.H)/2)
			t.left = true
			l.texts = append(l.texts, t)
			nameW = max(nameW, lb.box.W)
			ly += rowH + em*0.4
		}
		x += dot + em*0.5 + nameW + em*1.5
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// one size for every task: the widest name, and room for a row of dots
	type laidTask struct {
		lb   label
		task pm.JourneyTask
	}
	var taskW, nameH float32 = em * 7, 0
	tasks := make([][]laidTask, len(jd.Sections))
	for i, s := range jd.Sections {
		for _, t := range s.Tasks {
			lb := newLabel(m, t.Name, f, em*9, th.Text)
			row := float32(len(t.Actors))*(dot+em*0.3) - em*0.3 // the dots, one row along the box's bottom
			taskW, nameH = max(taskW, lb.box.W+em*1.6, row+em*1.6), max(nameH, lb.box.H)
			tasks[i] = append(tasks[i], laidTask{lb, t})
		}
	}
	taskH := nameH + em*1.2
	if len(jd.Actors) > 0 {
		taskH += dot + em*0.3
	}
	var headH float32
	heads := make([]label, len(jd.Sections))
	for i, s := range jd.Sections {
		heads[i] = newLabel(m, s.Name, gui.Font{Family: f.Family, Size: f.Size, Bold: true}, 0, th.Text)
		headH = max(headH, heads[i].box.H+em*0.8)
	}
	gap := em * 0.8
	taskY := y + headH + em*0.6
	faceD := em * 2
	faceY := taskY + taskH + em*1.6
	left, right := x, x
	for i, s := range jd.Sections {
		hue := journeyHue(th.NodeStroke, float64(i)*0.13)
		stroke := mix(hue, th.Text, 0.25)
		w := float32(len(s.Tasks))*(taskW+gap) - gap
		w = max(w, heads[i].box.W+em*1.6, taskW)
		head := gui.Rect{X: x, Y: y, W: w, H: headH}
		l.nodes = append(l.nodes, laidNode{form: formRect, box: head, fill: mix(hue, th.Background, 0.55), stroke: stroke, width: 1,
			label: heads[i].centredIn(head)})
		if s.Name == "" { // tasks before any section: no header to show
			l.nodes = l.nodes[:len(l.nodes)-1]
		}
		tx := x
		for _, lt := range tasks[i] {
			box := gui.Rect{X: tx, Y: taskY, W: taskW, H: taskH}
			n := laidNode{form: formRound, box: box, fill: mix(hue, th.Background, 0.8), stroke: stroke, width: 1.2}
			n.label = lt.lb.at(tx+(taskW-lt.lb.box.W)/2, taskY+em*0.6)
			l.nodes = append(l.nodes, n)
			// the actors' dots, a row centred along the box's bottom
			row := float32(len(lt.task.Actors))*(dot+em*0.3) - em*0.3
			dx := tx + (taskW-row)/2
			for _, a := range lt.task.Actors {
				c := actor[a]
				l.nodes = append(l.nodes, laidNode{form: formCircle, box: gui.Rect{X: dx, Y: taskY + taskH - em*0.5 - dot, W: dot, H: dot},
					fill: c, stroke: c, width: 1})
				dx += dot + em*0.3
			}
			cx := tx + taskW/2
			face := gui.Rect{X: cx - faceD/2, Y: faceY, W: faceD, H: faceD}
			l.nodes = append(l.nodes, laidNode{form: formFace, box: face, fill: th.NodeFill, stroke: th.Text, width: 1.2, mood: lt.task.Score})
			l.edges = append(l.edges, laidEdge{pts: []gui.Point{{X: cx, Y: taskY + taskH}, {X: cx, Y: faceY}}, line: pm.Dotted,
				color: mix(th.Line, th.Background, 0.3)})
			tx += taskW + gap
		}
		x += w + gap
		right = x - gap
	}
	bottom := y
	if right > left { // the journey's way, under the faces
		axisY := faceY + faceD + em*0.8
		l.edges = append(l.edges, laidEdge{pts: []gui.Point{{X: left, Y: axisY}, {X: right + em, Y: axisY}}, line: pm.Solid,
			head: endArrow, color: th.Line})
		bottom = axisY
		right += em
	}
	for _, n := range l.nodes {
		bottom = max(bottom, n.box.Y+n.box.H)
	}
	for _, t := range l.texts {
		bottom = max(bottom, t.box.Y+t.box.H)
	}
	w := right + pad
	if title != nil {
		w = max(w, title.box.W+2*pad)
		t := title.at((w-title.box.W)/2, pad)
		l.texts = append(l.texts, t)
	}
	l.size = gui.Size{W: max(w, 2*pad), H: bottom + pad}
	return l, nil
}

// journeyHue is c with its hue turned by turn (a fraction of the wheel), its lightness and
// saturation kept within reach of the theme: the journey's sections and actors take their colours
// from the theme's stroke, so they suit it, light or dark.
func journeyHue(c color.NRGBA, turn float64) color.NRGBA {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	hi, lo := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	light := (hi + lo) / 2
	var h, s float64
	if hi != lo {
		d := hi - lo
		s = d / (1 - math.Abs(2*light-1))
		switch hi {
		case r:
			h = math.Mod((g-b)/d, 6)
		case g:
			h = (b-r)/d + 2
		default:
			h = (r-g)/d + 4
		}
		h /= 6
	}
	s = math.Max(s, 0.45) // a grey theme still tells its actors apart
	light = math.Min(math.Max(light, 0.35), 0.65)
	h = math.Mod(h+turn+1, 1)
	ch := (1 - math.Abs(2*light-1)) * s
	xc := ch * (1 - math.Abs(math.Mod(h*6, 2)-1))
	var rr, gg, bb float64
	switch int(h * 6) {
	case 0:
		rr, gg = ch, xc
	case 1:
		rr, gg = xc, ch
	case 2:
		gg, bb = ch, xc
	case 3:
		gg, bb = xc, ch
	case 4:
		rr, bb = xc, ch
	default:
		rr, bb = ch, xc
	}
	m := light - ch/2
	to := func(v float64) uint8 { return uint8(math.Round(math.Min(math.Max(v+m, 0), 1) * 255)) }
	return color.NRGBA{R: to(rr), G: to(gg), B: to(bb), A: c.A}
}
