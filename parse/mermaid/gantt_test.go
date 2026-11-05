package mermaid_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

func ganttParse(t *testing.T, src string) *mermaid.GanttDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	g, ok := d.(*mermaid.GanttDiagram)
	if !ok || d.Kind() != mermaid.Gantt {
		t.Fatalf("%q parsed as %T", src, d)
	}
	return g
}

// ganttShape is a chart as section › task [tags] start..end, for comparing.
func ganttShape(d *mermaid.GanttDiagram) []string {
	var out []string
	for _, s := range d.Sections {
		for _, k := range s.Tasks {
			tags := ""
			for _, f := range []struct {
				on   bool
				name string
			}{{k.Done, "done"}, {k.Active, "active"}, {k.Crit, "crit"}, {k.Milestone, "milestone"}} {
				if f.on {
					tags += " " + f.name
				}
			}
			out = append(out, s.Title+" › "+k.Name+" ("+k.ID+")"+tags+" "+k.Start.Format("01-02")+".."+k.End.Format("01-02"))
		}
	}
	return out
}

// Mermaid's documented gantt: sections, tags, IDs, dates, lengths in days and weeks, `after` one
// task and after several (the latest), a task starting where the one before ends, and a milestone.
func TestGanttOfSectionsAndTasks(t *testing.T) {
	d := ganttParse(t, `gantt
    title A Gantt Diagram
    dateFormat YYYY-MM-DD
    axisFormat %m-%d
    todayMarker off
    %% a comment
    accTitle: The plan
    section Section
    A task           :a1, 2014-01-01, 30d
    Another task     :after a1, 20d
    section Another
    Task in Another  :done, crit, b1, 2014-01-12, 2014-01-24
    another task     :24d
    Two weeks        :active, 2w
    Last             :after b1 a1, 1d
    Ship             :milestone, m1, 2014-03-01, 0d`)
	if d.Title != "A Gantt Diagram" || d.AxisFormat != "%m-%d" {
		t.Errorf("title %q, axis %q", d.Title, d.AxisFormat)
	}
	want := []string{
		"Section › A task (a1) 01-01..01-31",
		"Section › Another task () 01-31..02-20",
		"Another › Task in Another (b1) done crit 01-12..01-24",
		"Another › another task () 01-24..02-17",
		"Another › Two weeks () active 02-17..03-03",
		"Another › Last () 01-31..02-01", // after the later of b1 (01-24) and a1 (01-31)
		"Another › Ship (m1) milestone 03-01..03-01",
	}
	if got := ganttShape(d); !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
	start, end, ok := d.Range()
	if !ok || start.Format("2006-01-02") != "2014-01-01" || end.Format("2006-01-02") != "2014-03-03" {
		t.Errorf("Range %v %v %v; want 2014-01-01 to 2014-03-03", start, end, ok)
	}
}

// Tasks before any section are in one with no title; the date format may be left out (it is the
// only one the subset reads); a span covers the task's line.
func TestGanttUntitledSectionAndSpans(t *testing.T) {
	src := "gantt\n  a :2026-01-01, 3d\n  b :1d"
	d := ganttParse(t, src)
	if len(d.Sections) != 1 || d.Sections[0].Title != "" || len(d.Sections[0].Tasks) != 2 {
		t.Fatalf("sections %+v", d.Sections)
	}
	a := d.Sections[0].Tasks[0]
	if got := src[a.Span[0]:a.Span[1]]; got != "a :2026-01-01, 3d" {
		t.Errorf("span %q", got)
	}
}

// Every construct outside the subset makes the whole chart another renderer's, and says which,
// on its line: a renderer never draws a chart whose dates it read differently from Mermaid.
func TestGanttOutsideTheSubset(t *testing.T) {
	for _, c := range []struct {
		src  string
		line int
		what string
	}{
		{"gantt\n  dateFormat DD-MM-YYYY\n  a :01-01-2026, 3d", 2, "a gantt dateFormat other than YYYY-MM-DD"},
		{"gantt\n  excludes weekends\n  a :2026-01-01, 3d", 2, "gantt excludes"},
		{"gantt\n  a :2026-01-01, 3d\n  inclusiveEndDates", 3, "gantt inclusiveEndDates"},
		{"gantt\n  tickInterval 1week\n  a :2026-01-01, 3d", 2, "gantt tickInterval"},
		{"gantt\n  a :a1, 2026-01-01, 3d\n  click a1 href \"x\"", 3, "gantt click"},
		{"gantt\n  todayMarker stroke-width:5px\n  a :2026-01-01, 3d", 2, "a gantt today marker"},
		{"gantt\n  a :3d", 2, "a first gantt task with no start date"},
		{"gantt\n  a :after b1, 3d\n  b :b1, 2026-01-01, 1d", 2, "a gantt task after one not defined before it"},
		{"gantt\n  a :2026-01-01, 12h", 2, "a gantt end that is neither a length in days or weeks nor a YYYY-MM-DD date"},
		{"gantt\n  a :2026-01-01, 1.5d", 2, "a gantt end that is neither a length in days or weeks nor a YYYY-MM-DD date"},
		{"gantt\n  a :a1, 2026-01-01, until b1", 2, "a gantt end that is neither a length in days or weeks nor a YYYY-MM-DD date"},
		{"gantt\n  a :2026-01-01 10:00, 3d", 2, "a gantt start that is not a YYYY-MM-DD date"},
		{"gantt LR\n  a :2026-01-01, 3d", 1, "text after gantt"},
	} {
		_, err := mermaid.Parse(c.src)
		var nd *mermaid.NotDrawnError
		if !errors.As(err, &nd) || !errors.Is(err, mermaid.ErrUnsupported) {
			t.Errorf("%q: %v; want ErrUnsupported", c.src, err)
			continue
		}
		if nd.Line != c.line || nd.What != c.what {
			t.Errorf("%q: line %d %q; want line %d %q", c.src, nd.Line, nd.What, c.line, c.what)
		}
	}
}

// A chart in the subset's grammar that is wrong is a syntax error at its line, so the editor
// shows it in place.
func TestGanttSyntaxErrors(t *testing.T) {
	for _, c := range []struct {
		src  string
		line int
	}{
		{"gantt\n  title only", 1},                                     // no task
		{"gantt\n  a 2026-01-01, 3d", 2},                               // no colon
		{"gantt\n  :2026-01-01, 3d", 2},                                // no name
		{"gantt\n  a :done", 2},                                        // tags and no dates
		{"gantt\n  a :x, 2026-01-01, 3d, 1d", 2},                       // four items
		{"gantt\n  a :2026-02-30, 3d", 2},                              // no such day
		{"gantt\n  a :2026-01-05, 2026-01-01", 2},                      // ends before it starts
		{"gantt\n  a :x1, 2026-01-01, 1d\n  b :x1, 2026-01-02, 1d", 3}, // a second x1
		{"gantt\n  a :x y, 2026-01-01, 1d", 2},                         // an ID with a space
		{"gantt\n  section\n  a :2026-01-01, 1d", 2},                   // a section with no title
	} {
		_, err := mermaid.Parse(c.src)
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%q: %v; want a *SyntaxError", c.src, err)
			continue
		}
		if se.Line != c.line {
			t.Errorf("%q: line %d (%v); want line %d", c.src, se.Line, se, c.line)
		}
	}
}
