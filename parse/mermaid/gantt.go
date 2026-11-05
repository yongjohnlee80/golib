package mermaid

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Gantt is a gantt chart: a gantt header.
const Gantt Kind = 10

func init() { register(Gantt, "gantt", []string{"gantt"}, (*parser).gantt) }

// GanttDiagram is a parsed gantt chart: its title and its tasks in sections, each task's dates
// resolved. Dates are days (UTC midnights): the subset reads the YYYY-MM-DD date format only. A
// chart spans at most ganttMaxYears; past it, ErrTooLarge.
//
// No today line is drawn, so a chart reads the same on every day it is shown: `todayMarker off`
// is read (it says as much), and any other todayMarker is ErrUnsupported, mermaid.js's to draw.
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
	// A milestone keeps both, as Mermaid does (a task after it starts at End); it is drawn at Mid.
	Start, End time.Time
	Done       bool // done
	Active     bool // active
	Crit       bool // crit
	Milestone  bool // milestone
	Span       [2]int
}

// Mid is the middle of t's days: where a milestone is drawn, as Mermaid draws it (Start for one of
// length 0d).
func (t GanttTask) Mid() time.Time { return t.Start.Add(t.End.Sub(t.Start) / 2) }

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

// ganttMaxYears bounds a chart's span, and so a task's length: past it, ErrTooLarge. Every date a
// renderer subtracts is then well inside time.Duration's range (about 292 years).
const ganttMaxYears = 100

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
	start, end, ok := g.d.Range()
	switch {
	case !ok && p.syntax == nil:
		// Mermaid draws an empty chart: nothing of this chart's is drawn here
		p.unsupport(restOff, "a gantt chart with no task")
	case ok && end.After(start.AddDate(ganttMaxYears, 0, 0)):
		g.tooLarge(restOff, fmt.Sprintf("a gantt chart spanning more than %d years", ganttMaxYears))
	}
	return p.done(g.d)
}

// tooLarge is the chart's answer for what is past a bound: ErrTooLarge, on off's line.
func (g *ganttParser) tooLarge(off int, what string) {
	if g.p.unsupported == nil {
		l, _ := g.p.pos(off)
		g.p.unsupported = notDrawn(ErrTooLarge, l, what, "%s (line %d)", what, l)
	}
}

// statement is one line: a setting, a section, an accessibility line or a task.
func (g *ganttParser) statement(st stmt) {
	if strings.HasPrefix(st.text, "#") {
		// Mermaid's gantt reads # to the line's end as a comment, there and after a setting
		g.p.unsupport(st.off, "a gantt # comment")
		return
	}
	w, rest := firstWord(st.text)
	restOff := st.off + len(st.text) - len(rest)
	switch w {
	case "title":
		if s, err := g.p.label(ganttValue(rest), restOff); err == nil {
			g.d.Title = s
		}
		return
	case "dateFormat":
		if strings.TrimSpace(ganttValue(rest)) != ganttDateFormat {
			g.p.unsupport(st.off, "a gantt dateFormat other than "+ganttDateFormat)
		}
		return
	case "axisFormat":
		f := strings.TrimSpace(ganttValue(rest))
		if bad := ganttAxisDirective(f); bad != "" {
			// AxisLabel would write it as it is, where mermaid.js formats it
			g.p.unsupport(st.off, "the gantt axis directive "+bad)
			return
		}
		g.d.AxisFormat = f
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
			// Mermaid reads it (a.b, x y): this parse's `after` lists don't
			g.p.unsupport(metaOff, "a gantt task ID other than letters, digits, _ and -")
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
// A length past ganttMaxYears is ErrTooLarge, checked before it is added.
func (g *ganttParser) end(t *GanttTask, s string, off int) bool {
	if n, unit, ok := ganttLength(s); ok {
		per := 1
		if unit == 'w' {
			per = 7
		}
		if n > ganttMaxYears*366/per {
			g.tooLarge(off, fmt.Sprintf("a gantt task longer than %d years", ganttMaxYears))
			return false
		}
		t.End = t.Start.AddDate(0, 0, n*per)
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

// ganttValue is a setting's value as Mermaid's gantt reads it: up to a # (a comment) or a ;.
func ganttValue(s string) string {
	if i := strings.IndexAny(s, "#;"); i >= 0 {
		return s[:i]
	}
	return s
}

// ganttAxisDirectives are the strftime directives AxisLabel writes, after the %.
const ganttAxisDirectives = "YymdebBaAj%"

// ganttAxisDirective is f's first directive AxisLabel doesn't write ("%H", "%-d", a % at its end);
// "" when it writes them all.
func ganttAxisDirective(f string) string {
	for i := 0; i < len(f); i++ {
		if f[i] != '%' {
			continue
		}
		if i+1 == len(f) || !strings.ContainsRune(ganttAxisDirectives, rune(f[i+1])) {
			return f[i:min(i+2, len(f))]
		}
		i++
	}
	return ""
}

// ganttDefaultAxis is the axis format when the chart writes none, as Mermaid's.
const ganttDefaultAxis = "%Y-%m-%d"

// AxisLabel is day as the chart's axis writes it: AxisFormat's strftime directives (%Y %y %m %d %e
// %b %B %a %A %j %%), or "%Y-%m-%d" when it has none. Parse refuses any other directive; one set
// on the field by hand is written as it is.
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
// 14 or 28 days, and so on, then whole multiples of 364) that leaves at most n of them across its
// whole Range. nil for a chart with no task, or n < 1.
func (d *GanttDiagram) Ticks(n int) []time.Time {
	start, end, ok := d.Range()
	if !ok || n < 1 {
		return nil
	}
	days := int(end.Sub(start).Hours()/24) + 1
	last := ganttSteps[len(ganttSteps)-1]
	step := last * ((days + last*n - 1) / (last * n)) // past the table: years enough that n cover it
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
