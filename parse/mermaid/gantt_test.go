package mermaid_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

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
		// valid Mermaid this subset doesn't read: mermaid.js draws it, so never a syntax error
		{"gantt\n  title only", 1, "a gantt chart with no task"},
		{"gantt\n  a :a.b, 2026-01-01, 1d", 2, "a gantt task ID other than letters, digits, _ and -"},
		{"gantt\n  a :x y, 2026-01-01, 1d", 2, "a gantt task ID other than letters, digits, _ and -"},
		{"gantt\n  # a note\n  a :2026-01-01, 1d", 2, "a gantt # comment"},
		{"gantt\n  axisFormat %H:%M\n  a :2026-01-01, 1d", 2, "the gantt axis directive %H"},
		{"gantt\n  axisFormat %-d %b\n  a :2026-01-01, 1d", 2, "the gantt axis directive %-"},
		{"gantt\n  axisFormat %m-%\n  a :2026-01-01, 1d", 2, "the gantt axis directive %"},
		{"gantt\n  a :a1, 3d", 2, "a gantt start that is not a YYYY-MM-DD date"}, // `id, length`: a1 is read as the start
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
		{"gantt\n  a 2026-01-01, 3d", 2},                               // no colon
		{"gantt\n  :2026-01-01, 3d", 2},                                // no name
		{"gantt\n  a :done", 2},                                        // tags and no dates
		{"gantt\n  a :x, 2026-01-01, 3d, 1d", 2},                       // four items
		{"gantt\n  a :2026-02-30, 3d", 2},                              // no such day
		{"gantt\n  a :2026-01-05, 2026-01-01", 2},                      // ends before it starts
		{"gantt\n  a :x1, 2026-01-01, 1d\n  b :x1, 2026-01-02, 1d", 3}, // a second x1
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

// The axis writes a day in the chart's axisFormat, or %Y-%m-%d; its ticks are a fixed step from
// the first day, no more of them than asked.
func TestGanttAxis(t *testing.T) {
	d := ganttParse(t, "gantt\n  axisFormat %b %e, %a %%\n  a :2026-01-01, 60d")
	day := d.Sections[0].Tasks[0].Start.AddDate(0, 0, 4)
	if got := d.AxisLabel(day); got != "Jan  5, Mon %" {
		t.Errorf("AxisLabel %q", got)
	}
	d.AxisFormat = "%q" // a directive Parse refuses, set by hand: written as it is
	if got := d.AxisLabel(day); got != "%q" {
		t.Errorf("AxisLabel of %%q: %q", got)
	}
	d.AxisFormat = ""
	if got := d.AxisLabel(day); got != "2026-01-05" {
		t.Errorf("default AxisLabel %q", got)
	}
	ticks := d.Ticks(6) // 61 days: a 14-day step gives 5
	if len(ticks) != 5 || ticks[0] != d.Sections[0].Tasks[0].Start || ticks[1].Sub(ticks[0]).Hours() != 14*24 {
		t.Errorf("Ticks(6) = %v", ticks)
	}
	if got := d.Ticks(100); len(got) != 61 { // a day each
		t.Errorf("Ticks(100): %d ticks, want 61", len(got))
	}
	if d.Ticks(0) != nil || (&mermaid.GanttDiagram{}).Ticks(5) != nil {
		t.Error("Ticks of 0, or of an empty chart, is not nil")
	}
}

// A milestone keeps its length, as Mermaid's: a task after it, or with no start, begins at its
// End, and it is drawn at Mid, its days' middle (its Start for 0d).
func TestGanttMilestoneKeepsItsLength(t *testing.T) {
	d := ganttParse(t, "gantt\n  m :milestone, m1, 2026-01-01, 4d\n  b :after m1, 1d\n  c :1d\n  z :milestone, 2026-01-10, 0d")
	want := []string{
		" › m (m1) milestone 01-01..01-05",
		" › b () 01-05..01-06",
		" › c () 01-06..01-07",
		" › z () milestone 01-10..01-10",
	}
	if got := ganttShape(d); !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
	tasks := d.Sections[0].Tasks
	if got := tasks[0].Mid().Format("01-02"); got != "01-03" {
		t.Errorf("m's Mid %s, want 01-03", got)
	}
	if tasks[3].Mid() != tasks[3].Start {
		t.Errorf("a 0d milestone's Mid %v, want its Start %v", tasks[3].Mid(), tasks[3].Start)
	}
}

// Lines ending CRLF read as LF ones; a setting's value ends at # or ;, as Mermaid's gantt reads it.
func TestGanttCRLFAndSettingValues(t *testing.T) {
	d := ganttParse(t, "gantt\r\n  title Plan # draft\r\n  dateFormat YYYY-MM-DD ; the only one\r\n  axisFormat %m-%d # short\r\n  section S\r\n  a :a1, 2026-01-01, 3d\r\n  b :after a1, 2d\r\n")
	if d.Title != "Plan" || d.AxisFormat != "%m-%d" {
		t.Errorf("title %q, axis %q; want Plan and %%m-%%d", d.Title, d.AxisFormat)
	}
	want := []string{"S › a (a1) 01-01..01-04", "S › b () 01-04..01-06"}
	if got := ganttShape(d); !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

// Past a bound is ErrTooLarge, on its line: a length checked before it is added (no overflow), a
// chart spanning more than 100 years (no saturated Sub in a renderer), and a name past MaxLabel.
func TestGanttTooLarge(t *testing.T) {
	for _, c := range []struct {
		src  string
		line int
	}{
		{"gantt\n  a :2026-01-01, 1000000000000000d", 2},
		{"gantt\n  a :2026-01-01, 6000w", 2},
		{"gantt\n  a :0001-01-01, 1d\n  b :9999-01-01, 1d", 1},
		{"gantt\n  " + strings.Repeat("a", mermaid.MaxLabel+1) + " :2026-01-01, 1d", 2},
	} {
		_, err := mermaid.Parse(c.src)
		var nd *mermaid.NotDrawnError
		if !errors.As(err, &nd) || !errors.Is(err, mermaid.ErrTooLarge) {
			t.Errorf("%.60q: %v; want ErrTooLarge", c.src, err)
			continue
		}
		if nd.Line != c.line {
			t.Errorf("%.60q: line %d; want %d", c.src, nd.Line, c.line)
		}
	}
	// at the bound it is drawn
	ganttParse(t, "gantt\n  a :2000-01-01, 2100-01-01")
}

// Past the 364-day step, Ticks widens it by whole 364s: at most n ticks, still covering the
// whole Range.
func TestGanttTicksPastAYear(t *testing.T) {
	d := ganttParse(t, "gantt\n  a :2000-01-01, 2099-01-01")
	_, end, _ := d.Range()
	ticks := d.Ticks(5)
	if len(ticks) < 2 || len(ticks) > 5 {
		t.Fatalf("Ticks(5): %d ticks", len(ticks))
	}
	step := ticks[1].Sub(ticks[0])
	if days := int(step / (24 * time.Hour)); days%364 != 0 {
		t.Errorf("step %d days, want a multiple of 364", days)
	}
	if last := ticks[len(ticks)-1]; !last.Add(step).After(end) {
		t.Errorf("last tick %s, a step short of the end %s: the axis stops early", last.Format("2006-01-02"), end.Format("2006-01-02"))
	}
}
