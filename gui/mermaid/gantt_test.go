package mermaid

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

const ganttExample = `gantt
    title A Gantt Diagram
    dateFormat YYYY-MM-DD
    axisFormat %m-%d
    section Section
    A task           :a1, 2014-01-01, 30d
    Another task     :after a1, 20d
    section Another
    Task in Another  :done, crit, 2014-01-12, 12d
    another task     :active, 24d
    Ship             :milestone, m1, 2014-03-01, 0d`

// The documented gantt lays out: a bar per task in its own row, top to bottom in source order,
// each from its first day to its end along one day axis (a bar after another starts where it ends);
// the milestone a diamond on its day; each section's band round its rows; and every text painted.
func TestAGanttIsLaidOutAndPainted(t *testing.T) {
	l, err := lay(t, ganttExample, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.nodes) != 5 || len(l.group) != 2 {
		t.Fatalf("%d bars and %d bands, want 5 and 2", len(l.nodes), len(l.group))
	}
	for i := 1; i < len(l.nodes); i++ {
		a, b := l.nodes[i-1].box, l.nodes[i].box
		if b.Y <= a.Y+a.H/2 {
			t.Errorf("bar %d at %+v is not in a row under bar %d at %+v", i, b, i-1, a)
		}
	}
	a1, after := l.nodes[0].box, l.nodes[1].box
	if d := after.X - (a1.X + a1.W); d < -0.01 || d > 0.01 {
		t.Errorf("'after a1' starts at %v, a1 ends at %v", after.X, a1.X+a1.W)
	}
	// 30 days are 1.5 times 20
	if r := a1.W / after.W; r < 1.49 || r > 1.51 {
		t.Errorf("30d and 20d bars are %v and %v wide: ratio %v, want 1.5", a1.W, after.W, r)
	}
	ship := l.nodes[4]
	if ship.form != formDiamond || ship.id != "m1" || ship.box.W != ship.box.H {
		t.Errorf("the milestone: form %v id %q box %+v; want a square diamond, m1", ship.form, ship.id, ship.box)
	}
	if l.nodes[1].id != "Another task" {
		t.Errorf("a task with no ID answers its name: %q", l.nodes[1].id)
	}
	// done is fainter than a plain bar, crit outlined heavier: the theme's colours, not fixed ones
	plain, crit := l.nodes[0], l.nodes[2]
	if crit.width <= plain.width || crit.stroke != testTheme.Text {
		t.Errorf("crit: width %v stroke %v; want heavier than %v, in the theme's text colour", crit.width, crit.stroke, plain.width)
	}
	for gi, g := range l.group {
		lo, hi := 0, 2
		if gi == 1 {
			lo, hi = 2, 5
		}
		for i, n := range l.nodes {
			if in := timelineInside(g.box, n.box); in != (i >= lo && i < hi) {
				t.Errorf("band %q holds bar %d: %v", g.title.lines, i, in)
			}
		}
	}
	// the axis: ticks left to right, each over a grid line, every date inside the chart
	var prev float32 = -1
	ticks := 0
	for _, lb := range l.texts {
		if lb.font.Size < testTheme.Font.Size && len(lb.lines) == 1 && len(lb.lines[0]) == 5 { // "01-15"
			if lb.box.X <= prev {
				t.Errorf("tick %q at %v is left of the one before", lb.lines, lb.box.X)
			}
			prev = lb.box.X
			ticks++
		}
	}
	if ticks < 2 || ticks != len(l.edges) {
		t.Errorf("%d ticks and %d grid lines; want at least 2, one line each", ticks, len(l.edges))
	}

	rc := gui.NewRecordingCanvas(gui.Size{W: 2000, H: 1000}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	texts := 0
	for _, c := range rc.Calls {
		if c.Op == "DrawText" {
			texts++
		}
	}
	want := 0
	for _, lb := range l.texts {
		want += len(lb.lines)
	}
	for _, g := range l.group {
		want += len(g.title.lines)
	}
	// the title, two section titles, five names and the ticks
	if want < 1+2+5+ticks || texts != want {
		t.Errorf("%d text lines drawn, want %d", texts, want)
	}
}

// A long gantt is scaled to the width asked; one with a single milestone lays out.
func TestAGanttFitsItsWidthAndLaysOutSmall(t *testing.T) {
	l, err := lay(t, ganttExample, 300)
	if err != nil {
		t.Fatal(err)
	}
	if l.Size().W != 300 || l.fit >= 1 {
		t.Errorf("size %+v fit %v", l.Size(), l.fit)
	}
	for _, src := range []string{"gantt\n  m :milestone, 2026-01-01, 0d", "gantt\n  section Alone\n  a :2026-01-01, 1d"} {
		l, err := lay(t, src, 400)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if s := l.Size(); s.W <= 0 || s.H <= 0 || len(l.nodes) != 1 {
			t.Errorf("%q: size %+v, %d bars", src, s, len(l.nodes))
		}
	}
}
