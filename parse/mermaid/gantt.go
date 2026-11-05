package mermaid

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// GanttDiagram is a parsed gantt chart: its title and its tasks in sections, each task's dates
// resolved. Dates are days (UTC midnights): the subset reads the YYYY-MM-DD date format only.
type GanttDiagram struct {
	Title string
	// AxisFormat is the axis's strftime format as written ("%m-%d"); "" for the renderer's own.
	AxisFormat string
	// Sections in source order. Tasks before the first section statement are in a section with no
	// title.
	Sections []GanttSection
}

func (*GanttDiagram) Kind() Kind { return Gantt }

// GanttSection is a section statement and the tasks after it, up to the next.
type GanttSection struct {
	Title string // "" for the tasks before any section statement
	Tasks []GanttTask
	Span  [2]int // its section statement; zero for the untitled one
}

// GanttTask is one task: `Write the parser :done, p1, 2026-01-01, 3d`.
type GanttTask struct {
	Name string
	ID   string // "" when the task names none
	// Start is the first day; End the day after the last (exclusive), so End-Start is its length.
	// A milestone's End is its Start.
	Start, End time.Time
	Done       bool // done
	Active     bool // active
	Crit       bool // crit
	Milestone  bool // milestone
	Span       [2]int
}

// Range is the days the chart covers: the earliest Start and the latest End of its tasks. ok is
// false for a chart with no task.
func (d *GanttDiagram) Range() (start, end time.Time, ok bool) {
	for _, s := range d.Sections {
		for _, t := range s.Tasks {
			if !ok || t.Start.Before(start) {
				start = t.Start
			}
			if !ok || t.End.After(end) {
				end = t.End
			}
			ok = true
		}
	}
	return start, end, ok
}

// ganttDateFormat is the one date format the subset reads, as dateFormat writes it.
const ganttDateFormat = "YYYY-MM-DD"

// ganttParser is the parse of one gantt chart.
type ganttParser struct {
	p    *parser
	d    *GanttDiagram
	ends map[string]time.Time // a task's ID → its End, for `after <id>`
	last *GanttTask           // the task before, whose End a task with no start begins at
}

// gantt parses a gantt chart's body: rest is its header line after the keyword, at restOff.
func (p *parser) gantt(rest string, restOff int) (Diagram, error) {
	g := &ganttParser{p: p, d: &GanttDiagram{}, ends: map[string]time.Time{}}
	if r := strings.TrimSpace(rest); r != "" {
		p.unsupport(restOff, "text after gantt")
		return p.done(nil)
	}
	for _, st := range timelineLines(p.src, restOff+len(rest)) {
		g.statement(st)
		if p.unsupported != nil {
			// a chart outside the subset is another renderer's whole; nothing after it matters,
			// and a later task may rest on one this parse didn't read
			return p.done(nil)
		}
	}
	if _, _, ok := g.d.Range(); !ok && p.syntax == nil {
		p.fail(restOff, "a gantt chart needs a task")
	}
	return p.done(g.d)
}

// statement is one line: a setting, a section, an accessibility line or a task.
func (g *ganttParser) statement(st stmt) {
	w, rest := firstWord(st.text)
	restOff := st.off + len(st.text) - len(rest)
	switch w {
	case "title":
		if s, err := g.p.label(rest, restOff); err == nil {
			g.d.Title = s
		}
		return
	case "dateFormat":
		if strings.TrimSpace(rest) != ganttDateFormat {
			g.p.unsupport(st.off, "a gantt dateFormat other than "+ganttDateFormat)
		}
		return
	case "axisFormat":
		g.d.AxisFormat = strings.TrimSpace(rest)
		return
	case "todayMarker":
		// the chart draws no today line, so only "off" says what it shows
		if strings.TrimSpace(rest) != "off" {
			g.p.unsupport(st.off, "a gantt today marker")
		}
		return
	case "excludes", "includes", "inclusiveEndDates", "weekday", "weekend", "tickInterval", "topAxis",
		"displayMode", "click", "call":
		// each changes a task's dates or what is drawn: outside the subset
		g.p.unsupport(st.off, "gantt "+w)
		return
	case "section":
		title, err := g.p.label(rest, restOff)
		if err != nil {
			return
		}
		if title == "" {
			g.p.fail(st.off, "a section needs a title")
			return
		}
		g.d.Sections = append(g.d.Sections, GanttSection{Title: title, Span: [2]int{st.off, st.off + len(st.text)}})
		return
	case "accTitle:", "accDescr:":
		return // accessibility text: nothing drawn
	case "accTitle", "accDescr":
		if strings.HasPrefix(rest, ":") {
			return
		}
		g.p.unsupport(st.off, w+" block")
		return
	}
	name, meta, ok := strings.Cut(st.text, ":")
	if !ok {
		g.p.fail(st.off, "a task needs its dates after a colon")
		return
	}
	g.task(name, meta, st)
}

// task reads `<name> : [tags,] [id,] [start,] <end>`: the tags first, then one to three items. One
// is the end (the task starts where the one before ends); two are start and end; three are an ID,
// start and end.
func (g *ganttParser) task(rawName, meta string, st stmt) {
	name, err := g.p.label(rawName, st.off)
	if err != nil {
		return
	}
	if name == "" {
		g.p.fail(st.off, "a task needs a name")
		return
	}
	t := GanttTask{Name: name, Span: [2]int{st.off, st.off + len(st.text)}}
	metaOff := st.off + len(rawName) + 1
	var items []string
	for _, it := range strings.Split(meta, ",") {
		items = append(items, strings.TrimSpace(it))
	}
tags:
	for len(items) > 0 {
		switch items[0] {
		case "done":
			t.Done = true
		case "active":
			t.Active = true
		case "crit":
			t.Crit = true
		case "milestone":
			t.Milestone = true
		default:
			break tags
		}
		items = items[1:]
	}
	var startItem, endItem string
	switch len(items) {
	case 1:
		endItem = items[0]
	case 2:
		startItem, endItem = items[0], items[1]
	case 3:
		t.ID, startItem, endItem = items[0], items[1], items[2]
		if !ganttID(t.ID) {
			g.p.fail(metaOff, "a task ID is letters, digits, _ and -: %q", t.ID)
			return
		}
	default:
		g.p.fail(metaOff, "a task has one to three dates after its tags, not %d", len(items))
		return
	}
	if endItem == "" {
		g.p.fail(metaOff, "a task needs an end or a length")
		return
	}
	if !g.start(&t, startItem, metaOff) {
		return
	}
	if !g.end(&t, endItem, metaOff) {
		return
	}
	if t.ID != "" {
		if _, dup := g.ends[t.ID]; dup {
			g.p.fail(metaOff, "a second task with ID %q", t.ID)
			return
		}
		g.ends[t.ID] = t.End
	}
	sec := g.section()
	sec.Tasks = append(sec.Tasks, t)
	g.last = &sec.Tasks[len(sec.Tasks)-1]
}

// start sets t.Start from s: a date, `after <id> [<id> …]` (the latest of their ends), or nothing
// (where the task before ends).
func (g *ganttParser) start(t *GanttTask, s string, off int) bool {
	switch {
	case s == "":
		if g.last == nil {
			// Mermaid starts it today: not the same chart twice
			g.p.unsupport(off, "a first gantt task with no start date")
			return false
		}
		t.Start = g.last.End
	case strings.HasPrefix(s, "after "):
		ids := strings.Fields(s[len("after "):])
		if len(ids) == 0 {
			g.p.fail(off, "after needs a task ID")
			return false
		}
		for i, id := range ids {
			e, ok := g.ends[id]
			if !ok {
				// a task later in the chart, or none: Mermaid resolves both its own way
				g.p.unsupport(off, "a gantt task after one not defined before it")
				return false
			}
			if i == 0 || e.After(t.Start) {
				t.Start = e
			}
		}
	default:
		d, err := ganttDate(s)
		if err != nil {
			if errors.Is(err, errGanttNotDate) {
				g.p.unsupport(off, "a gantt start that is not a "+ganttDateFormat+" date")
			} else {
				g.p.fail(off, "%v", err)
			}
			return false
		}
		t.Start = d
	}
	return true
}

// end sets t.End from s: a length (`3d`, `2w`) after Start, or a date (the task's end, exclusive).
// A milestone ends where it starts, whatever its length.
func (g *ganttParser) end(t *GanttTask, s string, off int) bool {
	if n, unit, ok := ganttLength(s); ok {
		days := n
		if unit == 'w' {
			days = 7 * n
		}
		t.End = t.Start.AddDate(0, 0, days)
	} else {
		d, err := ganttDate(s)
		if err != nil {
			if errors.Is(err, errGanttNotDate) {
				g.p.unsupport(off, "a gantt end that is neither a length in days or weeks nor a "+ganttDateFormat+" date")
			} else {
				g.p.fail(off, "%v", err)
			}
			return false
		}
		t.End = d
	}
	if t.Milestone {
		t.End = t.Start
	}
	if t.End.Before(t.Start) {
		g.p.fail(off, "a task that ends before it starts")
		return false
	}
	return true
}

// section is the section a task goes in: the last, or an untitled one for tasks before any.
func (g *ganttParser) section() *GanttSection {
	if len(g.d.Sections) == 0 {
		g.d.Sections = append(g.d.Sections, GanttSection{})
	}
	return &g.d.Sections[len(g.d.Sections)-1]
}

var errGanttNotDate = errors.New("not a date")

// ganttDate reads a YYYY-MM-DD date. errGanttNotDate: not that shape (another format, a time, a
// word); any other error is that shape naming no day (2026-02-30).
func ganttDate(s string) (time.Time, error) {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return time.Time{}, errGanttNotDate
	}
	for i, c := range []byte(s) {
		if i != 4 && i != 7 && (c < '0' || c > '9') {
			return time.Time{}, errGanttNotDate
		}
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, errors.New("no such day: " + s)
	}
	return d, nil
}

// ganttLength reads a whole number of days or weeks: `3d`, `2w`. Mermaid's other units (hours,
// minutes, months) and fractions are outside the subset: ok is false and the item reads as a date.
func ganttLength(s string) (n int, unit byte, ok bool) {
	if len(s) < 2 {
		return 0, 0, false
	}
	unit = s[len(s)-1]
	if unit != 'd' && unit != 'w' {
		return 0, 0, false
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n < 0 || s[0] == '+' {
		return 0, 0, false
	}
	return n, unit, true
}

// ganttID reports whether s is a task ID: letters, digits, _ and -.
func ganttID(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

// ganttDefaultAxis is the axis format when the chart writes none, as Mermaid's.
const ganttDefaultAxis = "%Y-%m-%d"

// AxisLabel is day as the chart's axis writes it: AxisFormat's strftime directives (%Y %y %m %d %e
// %b %B %a %A %j %%), or "%Y-%m-%d" when it has none. Any other directive is written as it is.
func (d *GanttDiagram) AxisLabel(day time.Time) string {
	f := d.AxisFormat
	if f == "" {
		f = ganttDefaultAxis
	}
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		if f[i] != '%' || i+1 == len(f) {
			b.WriteByte(f[i])
			continue
		}
		i++
		switch f[i] {
		case 'Y':
			b.WriteString(day.Format("2006"))
		case 'y':
			b.WriteString(day.Format("06"))
		case 'm':
			b.WriteString(day.Format("01"))
		case 'd':
			b.WriteString(day.Format("02"))
		case 'e':
			b.WriteString(day.Format("_2"))
		case 'b':
			b.WriteString(day.Format("Jan"))
		case 'B':
			b.WriteString(day.Format("January"))
		case 'a':
			b.WriteString(day.Format("Mon"))
		case 'A':
			b.WriteString(day.Format("Monday"))
		case 'j':
			b.WriteString(day.Format("002"))
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(f[i])
		}
	}
	return b.String()
}

// ganttSteps are the tick steps Ticks picks from, in days, smallest first.
var ganttSteps = []int{1, 2, 7, 14, 28, 56, 91, 182, 364}

// Ticks are the days the axis marks: from the chart's first day, at the smallest step (1, 2, 7,
// 14 or 28 days, and so on) that leaves at most n of them in its Range. nil for a chart with no
// task, or n < 1.
func (d *GanttDiagram) Ticks(n int) []time.Time {
	start, end, ok := d.Range()
	if !ok || n < 1 {
		return nil
	}
	days := int(end.Sub(start).Hours()/24) + 1
	step := ganttSteps[len(ganttSteps)-1]
	for _, s := range ganttSteps {
		if (days+s-1)/s <= n {
			step = s
			break
		}
	}
	var out []time.Time
	for t := start; !t.After(end) && len(out) < n; t = t.AddDate(0, 0, step) {
		out = append(out, t)
	}
	return out
}
