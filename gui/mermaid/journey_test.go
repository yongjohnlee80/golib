package mermaid

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

const journeyExample = `journey
    title My working day
    section Go to work
      Make tea: 5: Me
      Go upstairs: 3: Me
      Do work: 1: Me, Cat
    section Go home
      Go downstairs: 5: Me
      Sit down: 5: Me`

// journeyTexts paints l and counts the texts it draws.
func journeyTexts(l *Laid) int {
	rc := gui.NewRecordingCanvas(gui.Size{W: 4000, H: 2000}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	n := 0
	for _, c := range rc.Calls {
		if c.Op == "DrawText" {
			n++
		}
	}
	return n
}

// The documented example: every task on one line, none over another, each with its score as a
// face under it, its actors as dots, the sections' headers over their tasks, the legend left of
// them, every text painted.
func TestJourneyIsLaidOutOnOneLine(t *testing.T) {
	l, err := lay(t, journeyExample, 4000)
	if err != nil {
		t.Fatal(err)
	}
	var tasks, faces, heads []laidNode
	dots := 0
	for _, n := range l.nodes {
		switch n.form {
		case formRound:
			tasks = append(tasks, n)
		case formFace:
			faces = append(faces, n)
		case formRect:
			heads = append(heads, n)
		case formCircle:
			dots++
		}
	}
	if len(tasks) != 5 || len(faces) != 5 || len(heads) != 2 {
		t.Fatalf("%d tasks, %d faces, %d section headers; want 5, 5, 2", len(tasks), len(faces), len(heads))
	}
	// two legend dots, then one per actor of each task: 1+1+2+1+1
	if dots != 2+6 {
		t.Errorf("%d dots, want 8", dots)
	}
	for i, tk := range tasks {
		if tk.box.Y != tasks[0].box.Y {
			t.Errorf("task %q at y %v, not on the first's line %v", tk.label.lines, tk.box.Y, tasks[0].box.Y)
		}
		if i > 0 && tasks[i-1].box.X+tasks[i-1].box.W >= tk.box.X {
			t.Errorf("task %q overlaps the one before", tk.label.lines)
		}
		f := faces[i]
		if f.box.Y <= tk.box.Y+tk.box.H || f.box.X+f.box.W/2 != tk.box.X+tk.box.W/2 {
			t.Errorf("face %d at %+v is not under task %+v", i, f.box, tk.box)
		}
	}
	for i, want := range []int{5, 3, 1, 5, 5} {
		if faces[i].mood != want {
			t.Errorf("face %d's mood %d, want the score %d", i, faces[i].mood, want)
		}
	}
	// the headers over their tasks: Go to work's over the first three, Go home's over the last two
	near := func(a, b float32) bool { return a-b < 0.01 && b-a < 0.01 }
	if h := heads[0].box; !near(h.X, tasks[0].box.X) || !near(h.X+h.W, tasks[2].box.X+tasks[2].box.W) || h.Y+h.H > tasks[0].box.Y {
		t.Errorf("Go to work's header %+v does not head its tasks", h)
	}
	if h := heads[1].box; !near(h.X, tasks[3].box.X) || !near(h.X+h.W, tasks[4].box.X+tasks[4].box.W) {
		t.Errorf("Go home's header %+v does not head its tasks", h)
	}
	// the legend is left of every task
	for _, txt := range l.texts {
		if txt.lines[0] == "Me" || txt.lines[0] == "Cat" {
			if txt.box.X+txt.box.W >= tasks[0].box.X {
				t.Errorf("legend %q runs into the tasks", txt.lines)
			}
		}
	}
	// title, two legend names, two headers, five task names
	if n := journeyTexts(l); n != 1+2+2+5 {
		t.Errorf("%d texts drawn, want 10", n)
	}
	if l.Size().W <= 0 || l.Size().H <= 0 || l.fit != 1 {
		t.Errorf("size %+v fit %v", l.Size(), l.fit)
	}
}

// Section and actor colours come from the theme, and tell sections and actors apart.
func TestJourneyColoursFollowTheTheme(t *testing.T) {
	a, b := journeyHue(testTheme.NodeStroke, 0), journeyHue(testTheme.NodeStroke, 0.381966)
	if a == b {
		t.Error("two turns of the wheel gave one colour")
	}
	if a.A != testTheme.NodeStroke.A {
		t.Errorf("the colour's alpha %d is not the theme's %d", a.A, testTheme.NodeStroke.A)
	}
}

// A journey wider than the width asked is scaled down to it; an empty one, or one with a title
// alone, lays out and paints.
func TestJourneyFitsAndEmptyOnesLayOut(t *testing.T) {
	l, err := lay(t, journeyExample, 300)
	if err != nil {
		t.Fatal(err)
	}
	if l.Size().W != 300 || l.fit >= 1 {
		t.Errorf("size %+v fit %v", l.Size(), l.fit)
	}
	for _, src := range []string{"journey", "journey\n  title Only a title", "journey\n  section Empty"} {
		e, err := lay(t, src, 400)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if s := e.Size(); s.W <= 0 || s.H <= 0 {
			t.Errorf("%q: size %+v", src, s)
		}
		journeyTexts(e)
	}
}

// A task's dots stay in its own box: the task width holds the widest row of actors, so a short
// task with many actors never spills them across the gap into its neighbour.
func TestJourneyDotsStayInTheirTask(t *testing.T) {
	src := "journey\n  section S\n    Tea: 5: A, B, C, D, E, F, G, H, I, J\n    Go: 3: A\n"
	l, err := lay(t, src, 4000)
	if err != nil {
		t.Fatal(err)
	}
	var tasks []gui.Rect
	for _, n := range l.nodes {
		if n.form == formRound {
			tasks = append(tasks, n.box)
		}
	}
	if len(tasks) != 2 {
		t.Fatalf("%d task boxes, want 2", len(tasks))
	}
	if tasks[0].X+tasks[0].W > tasks[1].X {
		t.Errorf("the tasks overlap: %+v and %+v", tasks[0], tasks[1])
	}
	for _, n := range l.nodes {
		if n.form != formCircle || n.box.X < tasks[0].X {
			continue // the legend's dots, left of the tasks, are no task's
		}
		in := false
		for _, b := range tasks {
			if n.box.X >= b.X && n.box.X+n.box.W <= b.X+b.W && n.box.Y >= b.Y && n.box.Y+n.box.H <= b.Y+b.H {
				in = true
			}
		}
		if !in {
			t.Errorf("a dot at %+v is outside every task box %+v", n.box, tasks)
		}
	}
}
