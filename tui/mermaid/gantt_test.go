package mermaid

import (
	"context"
	"reflect"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

const ganttSrc = `gantt
  title A Gantt Diagram
  dateFormat YYYY-MM-DD
  axisFormat %m-%d
  section Section
    A task          :a1, 2014-01-01, 30d
    Another task    :after a1, 20d
  section Another
    Task in Another :done, crit, 2014-01-12, 12d
    another task    :active, 24d
    Ship            :milestone, m1, 2014-03-01, 0d
`

// A gantt chart draws in cells as its golden snapshot: the title, the axis's dates over their ┬,
// a section's title on its own row, each task's name indented under it and its bar along the day
// axis (█ a task, ░ done, ▓ active, ! before a crit one, ◆ a milestone), the grid's ┊ under each
// tick where no bar covers it.
func TestAGanttDrawsInCells(t *testing.T) {
	_, got := paint(t, ganttSrc)
	golden(t, "gantt-sections", got)
}

// ganttRow is the painted row of the box whose label is name.
func ganttRow(t *testing.T, g *Grid, rows []string, name string) (string, Box) {
	t.Helper()
	for _, b := range g.Boxes {
		if b.Label == name {
			return rows[b.Rect.Y], b
		}
	}
	t.Fatalf("no box %q in %+v", name, g.Boxes)
	return "", Box{}
}

// ganttSpan is the first and last rune column of glyph in row, -1 when it has none.
func ganttSpan(row, glyph string) (first, last int) {
	first, last = -1, -1
	for i, r := range []rune(row) {
		if string(r) == glyph {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	return first, last
}

// Each task is a box a host selects, its ID the task's (or its name), on its own row, its bar where
// its days are: a task after another starts where that one ends, a 30-day bar is about 1.5 times a
// 20-day one, and each state has its glyph.
func TestAGanttsBarsFollowTheirDays(t *testing.T) {
	g, out := paint(t, ganttSrc)
	rows := strings.Split(out, "\n")
	if len(g.Boxes) != 5 {
		t.Fatalf("%d boxes, want a box per task: %+v", len(g.Boxes), g.Boxes)
	}
	for i, b := range g.Boxes {
		if b.Form != FormText || b.Rect.H != 1 || (i > 0 && b.Rect.Y <= g.Boxes[i-1].Rect.Y) {
			t.Errorf("box %d %+v: want a one-row name under the one before", i, b)
		}
	}
	a, ab := ganttRow(t, g, rows, "A task")
	an, anb := ganttRow(t, g, rows, "Another task")
	if ab.ID != "a1" || anb.ID != "Another task" {
		t.Errorf("IDs %q %q; want the task's ID, else its name", ab.ID, anb.ID)
	}
	a0, a1 := ganttSpan(a, "█")
	n0, n1 := ganttSpan(an, "█")
	if a0 < 0 || n0 != a1+1 {
		t.Errorf("A task's bar %d..%d, after it %d..%d; want it to start where A task ends", a0, a1, n0, n1)
	}
	if r := float64(a1-a0+1) / float64(n1-n0+1); r < 1.3 || r > 1.7 {
		t.Errorf("30d and 20d bars are %d and %d cells: ratio %.2f, want about 1.5", a1-a0+1, n1-n0+1, r)
	}
	done, _ := ganttRow(t, g, rows, "Task in Another")
	if d0, _ := ganttSpan(done, "░"); d0 < 1 || []rune(done)[d0-1] != '!' || strings.Contains(done, "█") {
		t.Errorf("a done crit task: %q; want ░ with ! before it", done)
	}
	if active, _ := ganttRow(t, g, rows, "another task"); !strings.Contains(active, "▓") {
		t.Errorf("an active task: %q; want ▓", active)
	}
	ship, sb := ganttRow(t, g, rows, "Ship")
	if strings.Count(ship, "◆") != 1 || sb.ID != "m1" {
		t.Errorf("the milestone: %q, ID %q; want one ◆, m1", ship, sb.ID)
	}
}

// styled is a surface that keeps each cell's style, to say which theme token drew it.
type styled struct {
	*cells
	st map[[2]int]style.Style
}

func (s *styled) SetCell(x, y int, content string, st style.Style) {
	s.cells.SetCell(x, y, content, st)
	if x >= 0 && y >= 0 && x < s.w && y < s.h && content != "" {
		s.st[[2]int{x, y}] = st
	}
}
func (s *styled) Fill(r tui.Rect, content string, st style.Style) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			s.SetCell(x, y, content, st)
		}
	}
}

// Every cell a gantt paints takes one of the host's theme tokens, never a colour of its own, so the
// chart follows every theme: bars and their marks the box style, the axis and grid the line style,
// names, dates and titles the label style, a selected task's name the selected style. The token map
// is a snapshot: B box, L line, T label, S selected.
func TestAGanttPaintsInThemeTokens(t *testing.T) {
	d, err := pm.Parse(ganttSrc)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Lay(context.Background(), d, newCells(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	th := Theme{
		Line:     style.New().Foreground(style.RGB(1, 0, 0)),
		Box:      style.New().Foreground(style.RGB(2, 0, 0)),
		Label:    style.New().Foreground(style.RGB(3, 0, 0)),
		Selected: style.New().Foreground(style.RGB(4, 0, 0)),
	}
	s := &styled{cells: newCells(g.Size.W, g.Size.H), st: map[[2]int]style.Style{}}
	g.Paint(s, 0, 0, 4, th) // the milestone's name selected
	var b strings.Builder
	for y := range g.Size.H {
		for x := range g.Size.W {
			st, ok := s.st[[2]int{x, y}]
			switch {
			case !ok || s.at[y][x] == " ":
				b.WriteByte(' ')
			case reflect.DeepEqual(st, th.Box):
				b.WriteByte('B')
			case reflect.DeepEqual(st, th.Line):
				b.WriteByte('L')
			case reflect.DeepEqual(st, th.Label):
				b.WriteByte('T')
			case reflect.DeepEqual(st, th.Selected):
				b.WriteByte('S')
			default:
				t.Fatalf("cell %d,%d %q painted in a style that is no theme token: %+v", x, y, s.at[y][x], st)
			}
		}
		b.WriteString("\n")
	}
	tokens := strings.Join(func() []string {
		var out []string
		for _, l := range strings.Split(b.String(), "\n") {
			out = append(out, strings.TrimRight(l, " "))
		}
		return out
	}(), "\n")
	golden(t, "gantt-tokens", tokens)
	for _, glyph := range []string{"█", "░", "▓", "◆", "!"} {
		for y, row := range s.at {
			for x, c := range row {
				if c == glyph && !reflect.DeepEqual(s.st[[2]int{x, y}], th.Box) {
					t.Errorf("%q at %d,%d is not in the box style", glyph, x, y)
				}
			}
		}
	}
}

// A chart is listed in Kinds and laid by Lay; one too long is refused, for the host's notice.
func TestAGanttIsDrawnAndRefusesWhatIsTooLarge(t *testing.T) {
	d, err := pm.Parse("gantt\n  a :2026-01-01, 3d\n")
	if err != nil {
		t.Fatal(err)
	}
	if !Supported(d.Kind()) {
		t.Errorf("%v isn't listed in Kinds", d.Kind())
	}
	if g, err := Lay(context.Background(), d, newCells(1, 1)); err != nil || g.Size.H == 0 {
		t.Errorf("Lay: %v, size %+v", err, g)
	}
	big := &pm.GanttDiagram{Sections: []pm.GanttSection{{}}}
	for range ganttMaxRows + 1 {
		big.Sections[0].Tasks = append(big.Sections[0].Tasks, pm.GanttTask{Name: "t"})
	}
	if _, err := Lay(context.Background(), big, newCells(1, 1)); err == nil {
		t.Error("a chart past ganttMaxRows laid out")
	}
}

// A milestone with a length is drawn at its days' middle, where Mermaid draws it, and a task after
// it starts at its end: the ◆ sits halfway along the next task's lead-in, and a crit one's ! is just
// before the ◆.
func TestAGanttMilestoneSitsAtItsMiddle(t *testing.T) {
	g, out := paint(t, "gantt\n  span :s1, 2026-01-01, 40d\n  m :milestone, crit, m1, 2026-01-01, 40d\n  after :after m1, 10d\n")
	rows := strings.Split(out, "\n")
	span, _ := ganttRow(t, g, rows, "span")
	s0, s1 := ganttSpan(span, "█")
	m, _ := ganttRow(t, g, rows, "m")
	x, _ := ganttSpan(m, "◆")
	if mid := (s0 + s1 + 1) / 2; x < mid-1 || x > mid+1 {
		t.Errorf("the milestone's ◆ at %d; want its days' middle, about %d (its 40 days are %d..%d)", x, mid, s0, s1)
	}
	if x < 1 || []rune(m)[x-1] != '!' {
		t.Errorf("a crit milestone: %q; want ! just before its ◆", m)
	}
	after, _ := ganttRow(t, g, rows, "after")
	if a0, _ := ganttSpan(after, "█"); a0 != s1+1 {
		t.Errorf("the task after the milestone starts at %d; want its end, %d", a0, s1+1)
	}
}
