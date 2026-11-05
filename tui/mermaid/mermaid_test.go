package mermaid

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/graph/layout"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

var update = flag.Bool("update", false, "write the golden snapshots")

// cells is a tui.Surface over a plain grid of clusters, for snapshots: a wide cluster takes its
// cell and leaves the next empty, as a terminal draws it.
type cells struct {
	w, h int
	at   [][]string
}

func newCells(w, h int) *cells {
	c := &cells{w: w, h: h, at: make([][]string, h)}
	for y := range c.at {
		c.at[y] = make([]string, w)
		for x := range c.at[y] {
			c.at[y][x] = " "
		}
	}
	return c
}

func (c *cells) SetCell(x, y int, content string, _ style.Style) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h || content == "" {
		return
	}
	c.at[y][x] = content
	if tui.StringWidth(content) == 2 && x+1 < c.w {
		c.at[y][x+1] = ""
	}
}
func (c *cells) Fill(r tui.Rect, content string, st style.Style) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			c.SetCell(x, y, content, st)
		}
	}
}
func (c *cells) Sub(tui.Rect) tui.Surface { return c }
func (c *cells) Size() tui.Size           { return tui.Size{W: c.w, H: c.h} }
func (c *cells) StringWidth(s string) int { return tui.StringWidth(s) }
func (c *cells) Theme() *style.Theme      { return nil }
func (c *cells) Caps() tui.Capabilities   { return tui.Capabilities{} }
func (c *cells) String() string {
	var b strings.Builder
	for _, row := range c.at {
		b.WriteString(strings.TrimRight(strings.Join(row, ""), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

var _ tui.Surface = (*cells)(nil)

// paint lays src out and paints it into a surface its own size.
func paint(t *testing.T, src string) (*Grid, string) {
	t.Helper()
	d, err := pm.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	m := newCells(1, 1)
	g, err := Lay(context.Background(), d, m)
	if err != nil {
		t.Fatalf("lay: %v", err)
	}
	s := newCells(g.Size.W, g.Size.H)
	g.Paint(s, 0, 0, -1, Theme{})
	return g, s.String()
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (go test ./tui/mermaid -run %s -update)", path, err, t.Name())
	}
	if got != string(want) {
		t.Errorf("%s differs:\n--- got\n%s--- want\n%s", path, got, want)
	}
}

// Every direction mask has its own box-drawing character: no two share one, and none is blank.
func TestEveryLineJunctionHasItsCharacter(t *testing.T) {
	seen := map[string]dirs{}
	for d := dirs(1); d < 16; d++ {
		r := lineRune[d]
		if r == "" || r == " " {
			t.Errorf("dirs %04b has no character", d)
			continue
		}
		if o, ok := seen[r]; ok {
			t.Errorf("dirs %04b and %04b both draw %q", d, o, r)
		}
		seen[r] = d
	}
}

// A mind map draws as a two-sided tree: the root in the middle, a child each side, a parent's
// children on one shared spine, each shape its own outline.
func TestAMindmapDrawsAsATwoSidedTree(t *testing.T) {
	cases := map[string]string{
		"mindmap-two-sides": "mindmap\n  root((AutoDoc))\n    left[Search]\n    right(Agents)\n",
		"mindmap-spine":     "mindmap\n  root))Hub((\n    a[One]\n      a1(Leaf A)\n      a2(Leaf B)\n      a3(Leaf C)\n    b{{Two}}\n",
		"mindmap-plain":     "mindmap\n  Root\n    child one\n    child two\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, got := paint(t, src)
			golden(t, name, got)
		})
	}
}

// Boxes never overlap or touch, and no line runs through a box, whatever the tree: random trees of
// mixed shapes, depths and label widths, wide ones among them.
func TestNoBoxesOverlapAndNoLineCrossesOne(t *testing.T) {
	labels := []string{"a", "node", "a longer label", "日本語", "🙂 emoji", "x"}
	shapes := []pm.MindShape{pm.MindDefault, pm.MindSquare, pm.MindRounded, pm.MindHexagon, pm.MindBang, pm.MindCircle}
	rng := rand.New(rand.NewPCG(7, 11))
	for trial := range 200 {
		n := 1 + rng.IntN(40)
		d := &pm.MindmapDiagram{}
		for i := range n {
			parent := -1
			if i > 0 {
				parent = rng.IntN(i)
			}
			d.Nodes = append(d.Nodes, pm.MindNode{ID: fmt.Sprint(i), Label: labels[rng.IntN(len(labels))],
				Shape: shapes[rng.IntN(len(shapes))], Parent: parent})
		}
		g, err := LayMindmap(context.Background(), d, newCells(1, 1))
		if err != nil {
			t.Fatalf("trial %d: %v", trial, err)
		}
		for i, a := range g.Boxes {
			if a.Rect.X < 0 || a.Rect.Y < 0 || a.Rect.X+a.Rect.W > g.Size.W || a.Rect.Y+a.Rect.H > g.Size.H {
				t.Fatalf("trial %d: box %d %+v outside the grid %+v", trial, i, a.Rect, g.Size)
			}
			for j := i + 1; j < len(g.Boxes); j++ {
				b := g.Boxes[j]
				if a.Rect.X < b.Rect.X+b.Rect.W+1 && b.Rect.X < a.Rect.X+a.Rect.W+1 &&
					a.Rect.Y < b.Rect.Y+b.Rect.H+1 && b.Rect.Y < a.Rect.Y+a.Rect.H+1 {
					t.Fatalf("trial %d: boxes %d %+v and %d %+v overlap or touch", trial, i, a.Rect, j, b.Rect)
				}
			}
			for c := range g.lines {
				if c.x >= a.Rect.X && c.x < a.Rect.X+a.Rect.W && c.y >= a.Rect.Y && c.y < a.Rect.Y+a.Rect.H {
					t.Fatalf("trial %d: a line runs through box %d %+v at %v", trial, i, a.Rect, c)
				}
			}
		}
	}
}

// A wide label is measured in cells, so its box closes: the outline is the label's width plus
// four, and the right border sits on that column.
func TestAWideLabelsBoxCloses(t *testing.T) {
	g, got := paint(t, "mindmap\n  root[日本語のノード]\n")
	b := g.Boxes[0]
	if want := tui.StringWidth("日本語のノード") + 4; b.Rect.W != want {
		t.Fatalf("the box is %d wide, want %d", b.Rect.W, want)
	}
	rows := strings.Split(got, "\n")
	if !strings.HasPrefix(rows[0], "┌") || !strings.HasSuffix(rows[0], "┐") ||
		!strings.HasPrefix(rows[1], "│ 日本語のノード │") || !strings.HasPrefix(rows[2], "└") {
		t.Fatalf("the box isn't closed:\n%s", got)
	}
}

// A kind not in Kinds is ErrUnsupported, for the host to show its own way; a flowchart with
// subgraphs is drawn, each subgraph a frame.
func TestAKindNotDrawnIsUnsupported(t *testing.T) {
	d, err := pm.Parse("classDiagram\n  class A\n")
	if err != nil {
		t.Fatal(err)
	}
	if Supported(d.Kind()) {
		t.Fatal("a class diagram is reported drawn")
	}
	if _, err := Lay(context.Background(), d, newCells(1, 1)); err != ErrUnsupported {
		t.Fatalf("Lay of a class diagram: %v, want ErrUnsupported", err)
	}
	d, err = pm.Parse("flowchart TB\n  subgraph S [Group]\n    a --> b\n  end\n")
	if err != nil {
		t.Fatal(err)
	}
	if g, err := Lay(context.Background(), d, newCells(1, 1)); err != nil || len(g.frames) != 1 {
		t.Fatalf("Lay of a flowchart with a subgraph: %v, %d frames; want it drawn, one frame", err, len(g.frames))
	}
}

// Timelines, journeys and sequences draw in cells, each as its golden snapshot.
//   - timeline: a section boxed heavy on the time axis, each period boxed on it, its events boxed
//     to its right one under another, a period with none, and the axis ending in ▼;
//   - journey: each section boxed heavy on the spine, its tasks boxed right of it with their score
//     bar and actors, a task before any section hanging off the spine alone;
//   - sequence: participants boxed top and bottom (an actor rounded), lifelines joined to them
//     (┬ ┴), messages with each head (▶ ◀ × ▷, none), a dotted one dashed, one to itself as a
//     loop, notes left, right and over (the lifelines broken around them), activations heavy
//     (┃ ┠ ┨), frames named in the margin, autonumber.
func TestTheEasyKindsDrawInCells(t *testing.T) {
	cases := map[string]string{
		"timeline-sections": "timeline\n  title Social media\n  section Early\n    2002 : LinkedIn\n    2004 : Facebook : Google\n  section Later\n    2005 : YouTube\n    2006\n",
		"timeline-plain":    "timeline\n  2020 : Plan\n  2021 : Build : Test : Ship\n",
		"journey-day":       "journey\n  title My working day\n  section Go to work\n    Make tea: 5: Me\n    Go upstairs: 3: Me\n    Do work: 1: Me, Cat\n  section Go home\n    Go downstairs: 5: Me\n    Sit down: 4: Me\n",
		"journey-untitled":  "journey\n  Wake up: 2: Me\n  section Morning\n    Coffee: 5: Me, Partner\n",
		"sequence-messages": "sequenceDiagram\n  participant A as Alice\n  actor B as Bob\n  participant C as Carol\n  A->>B: Hello Bob\n  B-->>A: Hi\n  A-xC: Lost\n  C-)A: Later\n  A->C: plain\n  B->>B: think\n",
		"sequence-notes":    "sequenceDiagram\n  participant A\n  participant B\n  Note left of A: left\n  Note right of B: right\n  Note over A,B: shared\n  A->>B: done\n",
		"sequence-active":   "sequenceDiagram\n  autonumber\n  participant A as Client\n  participant B as Server\n  participant C as Store\n  A->>+B: request\n  B->>+C: query\n  C-->>-B: rows\n  loop Every page\n    A->>B: next\n  end\n  alt ok\n    B-->>A: data\n  else failed\n    B-->>-A: error\n  end\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			g, got := paint(t, src)
			golden(t, name, got)
			checkGrid(t, name, g)
		})
	}
}

// checkGrid is the no-overlap property for any grid: every box inside the grid; no two boxes
// overlap or touch; no line, mark or label cell inside a box.
func checkGrid(t *testing.T, what string, g *Grid) {
	t.Helper()
	inside := func(r tui.Rect, x, y int) bool { return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H }
	for i, a := range g.Boxes {
		if a.Rect.X < 0 || a.Rect.Y < 0 || a.Rect.X+a.Rect.W > g.Size.W || a.Rect.Y+a.Rect.H > g.Size.H {
			t.Fatalf("%s: box %d %+v outside the grid %+v", what, i, a.Rect, g.Size)
		}
		for j := i + 1; j < len(g.Boxes); j++ {
			b := g.Boxes[j]
			if a.Rect.X < b.Rect.X+b.Rect.W+1 && b.Rect.X < a.Rect.X+a.Rect.W+1 &&
				a.Rect.Y < b.Rect.Y+b.Rect.H+1 && b.Rect.Y < a.Rect.Y+a.Rect.H+1 {
				t.Fatalf("%s: boxes %d %+v and %d %+v overlap or touch", what, i, a.Rect, j, b.Rect)
			}
		}
		for c := range g.lines {
			if inside(a.Rect, c.x, c.y) {
				t.Fatalf("%s: a line runs through box %d %+v at %v", what, i, a.Rect, c)
			}
		}
		for c := range g.marks {
			if inside(a.Rect, c.x, c.y) {
				t.Fatalf("%s: a mark %q in box %d %+v at %v", what, g.marks[c], i, a.Rect, c)
			}
		}
		for _, tx := range g.texts {
			for x := tx.x; x < tx.x+tx.w; x++ {
				if inside(a.Rect, x, tx.y) {
					t.Fatalf("%s: the label %q runs into box %d %+v", what, tx.s, i, a.Rect)
				}
			}
		}
	}
}

// The property holds for random timelines, journeys and sequences: mixed label widths (wide ones
// among them), empty sections and periods, and every kind of sequence step.
func TestNoBoxesOverlapInTheEasyKinds(t *testing.T) {
	labels := []string{"a", "node", "a longer label", "日本語", "🙂 emoji", "x", ""}
	rng := rand.New(rand.NewPCG(3, 5))
	pick := func() string { return labels[rng.IntN(len(labels))] }
	ctx := context.Background()
	for trial := range 200 {
		tl := &pm.TimelineDiagram{Title: pick()}
		for range rng.IntN(4) {
			sec := pm.TimelineSection{Title: pick()}
			for range rng.IntN(5) {
				p := pm.TimelinePeriod{Label: pick()}
				for range rng.IntN(4) {
					p.Events = append(p.Events, pm.TimelineEvent{Text: pick()})
				}
				sec.Periods = append(sec.Periods, p)
			}
			tl.Sections = append(tl.Sections, sec)
		}
		g, err := LayTimeline(ctx, tl, newCells(1, 1))
		if err != nil {
			t.Fatalf("trial %d timeline: %v", trial, err)
		}
		checkGrid(t, fmt.Sprintf("trial %d timeline", trial), g)

		jd := &pm.JourneyDiagram{Title: pick()}
		for range rng.IntN(4) {
			sec := pm.JourneySection{Name: pick()}
			for range rng.IntN(5) {
				sec.Tasks = append(sec.Tasks, pm.JourneyTask{Name: pick(), Score: rng.IntN(7) - 1, Actors: []string{pick()}})
			}
			jd.Sections = append(jd.Sections, sec)
		}
		if g, err = LayJourney(ctx, jd, newCells(1, 1)); err != nil {
			t.Fatalf("trial %d journey: %v", trial, err)
		}
		checkGrid(t, fmt.Sprintf("trial %d journey", trial), g)

		sd := &pm.SequenceDiagram{Title: pick()}
		np := 1 + rng.IntN(5)
		for i := range np {
			sd.Participants = append(sd.Participants, pm.Participant{ID: fmt.Sprint("p", i), Label: pick() + "p", Actor: rng.IntN(3) == 0})
		}
		who := func() string { return fmt.Sprint("p", rng.IntN(np)) }
		ends := []pm.SeqEnd{pm.SeqNone, pm.SeqArrow, pm.SeqCross, pm.SeqAsync}
		for range rng.IntN(25) {
			switch rng.IntN(7) {
			case 0, 1, 2:
				sd.Steps = append(sd.Steps, pm.Step{Kind: pm.StepMessage, From: who(), To: who(), Text: pick(),
					Line: pm.LineKind(rng.IntN(2)), Head: ends[rng.IntN(4)], Tail: ends[rng.IntN(2)],
					Activate: rng.IntN(3) == 0, Deactivate: rng.IntN(3) == 0})
			case 3:
				sd.Steps = append(sd.Steps, pm.Step{Kind: pm.StepNote, From: who(), To: who(), Text: pick(),
					Place: pm.NotePlace(1 + rng.IntN(3))})
			case 4:
				sd.Steps = append(sd.Steps, pm.Step{Kind: pm.StepActivate, From: who()})
			case 5:
				sd.Steps = append(sd.Steps, pm.Step{Kind: pm.StepDeactivate, From: who()})
			case 6:
				sd.Steps = append(sd.Steps, pm.Step{Kind: []pm.StepKind{pm.StepBlock, pm.StepBranch, pm.StepEnd}[rng.IntN(3)],
					Block: pm.BlockKind(1 + rng.IntN(7)), Text: pick()})
			}
		}
		if g, err = LaySequence(ctx, sd, newCells(1, 1)); err != nil {
			t.Fatalf("trial %d sequence: %v", trial, err)
		}
		checkGrid(t, fmt.Sprintf("trial %d sequence", trial), g)
	}
}

// A diagram past the limits is refused at once, layout.ErrTooLarge for the host's notice, never
// laid out at length: too many participants or steps, too many boxes, or an area past maxCells.
func TestTheEasyKindsRefuseWhatIsTooLarge(t *testing.T) {
	ctx := context.Background()
	many := &pm.SequenceDiagram{}
	for i := range seqMaxParticipants + 1 {
		many.Participants = append(many.Participants, pm.Participant{ID: fmt.Sprint(i), Label: "p"})
	}
	long := &pm.SequenceDiagram{Participants: []pm.Participant{{ID: "a", Label: "a"}, {ID: "b", Label: "b"}}}
	for range seqMaxSteps + 1 {
		long.Steps = append(long.Steps, pm.Step{Kind: pm.StepMessage, From: "a", To: "b", Text: "m"})
	}
	boxes := &pm.TimelineDiagram{}
	sec := pm.TimelineSection{}
	for range spineMaxBoxes {
		sec.Periods = append(sec.Periods, pm.TimelinePeriod{Label: "p", Events: []pm.TimelineEvent{{Text: "e"}}})
	}
	boxes.Sections = []pm.TimelineSection{sec}
	// an area past maxCells: labels are capped, so the room is a sequence's at its limits, each
	// participant apart by a message at its cap, and a row or two for each of its steps
	wide := &pm.SequenceDiagram{}
	for i := range seqMaxParticipants {
		wide.Participants = append(wide.Participants, pm.Participant{ID: fmt.Sprint(i), Label: "p"})
	}
	for i := range seqMaxSteps {
		a := i % (seqMaxParticipants - 1)
		wide.Steps = append(wide.Steps, pm.Step{Kind: pm.StepMessage, From: fmt.Sprint(a), To: fmt.Sprint(a + 1), Text: strings.Repeat("w", 200)})
	}
	for name, d := range map[string]pm.Diagram{"participants": many, "steps": long, "boxes": boxes, "area": wide} {
		if _, err := Lay(ctx, d, newCells(1, 1)); !errors.Is(err, layout.ErrTooLarge) {
			t.Errorf("%s: %v, want layout.ErrTooLarge", name, err)
		}
	}
}

// Timeline, journey, sequence and flowchart (graph too) are drawn: Kinds lists them, and Lay lays
// each.
func TestTheEasyKindsAreDrawn(t *testing.T) {
	for _, src := range []string{"timeline\n  2020 : a\n", "journey\n  section s\n    t: 3: me\n", "sequenceDiagram\n  A->>B: hi\n",
		"flowchart LR\n  a --> b\n", "graph TD\n  a --> b\n"} {
		d, err := pm.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		if !Supported(d.Kind()) {
			t.Errorf("%v isn't listed in Kinds", d.Kind())
		}
		if g, err := Lay(context.Background(), d, newCells(1, 1)); err != nil || g.Size.H == 0 {
			t.Errorf("%v: %v, size %+v; want it laid", d.Kind(), err, g)
		}
	}
}
