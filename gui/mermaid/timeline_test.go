package mermaid

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

const timelineExample = `timeline
    title Timeline of Industrial Revolution
    section 17th-20th century
        Industry 1.0 : Machinery, Water power, Steam <br>power
        Industry 2.0 : Electricity, Internal combustion engine, Mass production
        Industry 3.0 : Electronics, Computers, Automation
    section 21st century
        Industry 4.0 : Internet, Robotics, Internet of Things
        Industry 5.0 : Artificial intelligence, Big data, 3D printing
                     : Quantum`

func timelineOverlap(a, b gui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func timelineInside(in, r gui.Rect) bool {
	return r.X >= in.X && r.Y >= in.Y && r.X+r.W <= in.X+in.W && r.Y+r.H <= in.Y+in.H
}

// The documented timeline lays out: periods left to right on one row, each period's events under
// it, nothing over anything, each section's band round its periods, and every text painted.
func TestATimelineIsLaidOutAndPainted(t *testing.T) {
	l, err := lay(t, timelineExample, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.nodes) != 5+6 || len(l.group) != 2 {
		t.Fatalf("%d boxes and %d bands, want 11 and 2", len(l.nodes), len(l.group))
	}
	for i, a := range l.nodes {
		for _, b := range l.nodes[i+1:] {
			if timelineOverlap(a.box, b.box) {
				t.Errorf("%q at %+v overlaps %q at %+v", a.label.lines, a.box, b.label.lines, b.box)
			}
		}
	}
	// the periods: one row, left to right; each one's events under it, in its column
	var periods []laidNode
	under := map[int][]laidNode{}
	for _, n := range l.nodes {
		if n.label.font.Bold {
			periods = append(periods, n)
		} else {
			under[len(periods)-1] = append(under[len(periods)-1], n)
		}
	}
	for i, p := range periods {
		if p.box.Y != periods[0].box.Y || (i > 0 && p.box.X <= periods[i-1].box.X) {
			t.Errorf("period %q at %+v is out of its row", p.label.lines, p.box)
		}
		for _, e := range under[i] {
			if e.box.Y <= p.box.Y+p.box.H || e.box.X < p.box.X || e.box.X+e.box.W > p.box.X+p.box.W {
				t.Errorf("event %q at %+v is not under its period %q at %+v", e.label.lines, e.box, p.label.lines, p.box)
			}
		}
	}
	if len(under[4]) != 2 {
		t.Errorf("Industry 5.0 has %d events, want 2", len(under[4]))
	}
	// each band holds its own periods and their events, and no other's
	for gi, g := range l.group {
		lo, hi := 0, 3
		if gi == 1 {
			lo, hi = 3, 5
		}
		for i, p := range periods {
			if in := timelineInside(g.box, p.box); in != (i >= lo && i < hi) {
				t.Errorf("band %q holds period %q: %v", g.title.lines, p.label.lines, in)
			}
		}
		if !timelineInside(g.box, g.title.box) {
			t.Errorf("band %q's title is outside it", g.title.lines)
		}
	}
	// the axis, with an arrowhead; a line down from each period with events
	var axis, joins int
	for _, e := range l.edges {
		if e.head == endArrow {
			axis++
		} else {
			joins++
		}
	}
	if axis != 1 || joins != 5 {
		t.Errorf("%d axes and %d joins, want 1 and 5", axis, joins)
	}

	rc := gui.NewRecordingCanvas(gui.Size{W: 4000, H: 1000}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	texts := 0
	for _, c := range rc.Calls {
		if c.Op == "DrawText" {
			texts++
		}
	}
	// a line drawn for each line of each label: the title, two section titles, five periods and
	// six events
	want := 0
	for _, lb := range l.texts {
		want += len(lb.lines)
	}
	for _, g := range l.group {
		want += len(g.title.lines)
	}
	for _, n := range l.nodes {
		want += len(n.label.lines)
	}
	if len(l.texts) != 1 || want < 1+2+5+6 || texts != want {
		t.Errorf("%d text lines drawn, want %d (of %d titles and %d labels)", texts, want, len(l.texts)+len(l.group), len(l.nodes))
	}
}

// A timeline wider than the width asked is scaled down to it.
func TestATimelineFitsItsWidth(t *testing.T) {
	l, err := lay(t, timelineExample, 300)
	if err != nil {
		t.Fatal(err)
	}
	if l.Size().W != 300 || l.fit >= 1 {
		t.Errorf("size %+v fit %v", l.Size(), l.fit)
	}
}

// A timeline with nothing, or with a title alone, lays out and paints.
func TestAnEmptyTimelineLaysOut(t *testing.T) {
	for _, src := range []string{"timeline", "timeline\n  title Only a title", "timeline\n  section Alone", "timeline\n  2007"} {
		l, err := lay(t, src, 400)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if s := l.Size(); s.W <= 0 || s.H <= 0 {
			t.Errorf("%q: size %+v", src, s)
		}
		l.Paint(gui.NewRecordingCanvas(gui.Size{W: 400, H: 400}, gui.Size{W: 8, H: 16}))
	}
}
