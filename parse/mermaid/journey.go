package mermaid

import (
	"strconv"
	"strings"
)

// JourneyDiagram is a parsed user journey: its sections, left to right, each with its tasks, and
// the actors the tasks name.
type JourneyDiagram struct {
	Title    string
	Sections []JourneySection
	Actors   []string // in order of first mention
}

func (*JourneyDiagram) Kind() Kind { return Journey }

// JourneySection is a section and its tasks. A task before any section is in one with no name.
type JourneySection struct {
	Name  string
	Tasks []JourneyTask
	Span  [2]int
}

// JourneyTask is `Name: score: actor, actor`: a step of the journey, how it felt (1, worst, to 5,
// best), and who took it.
type JourneyTask struct {
	Name   string
	Score  int
	Actors []string
	Span   [2]int
}

// journey parses a journey's body: rest is its header line after the keyword, at restOff.
func (p *parser) journey(rest string, restOff int) (Diagram, error) {
	j := &journeyParser{p: p, d: &JourneyDiagram{}, seen: map[string]bool{}}
	if r := strings.TrimSpace(rest); r != "" && !strings.HasPrefix(r, "%%") {
		p.fail(restOff, "unexpected %q after journey", r)
	}
	for _, st := range journeyLines(p.src, restOff+len(rest)) {
		j.statement(st)
		if p.unsupported != nil {
			break
		}
	}
	return p.done(j.d)
}

type journeyParser struct {
	p    *parser
	d    *JourneyDiagram
	seen map[string]bool // actors named so far
}

// journeyLines splits src[from:] into its lines, trimmed; a blank line and a %% comment are none.
func journeyLines(src string, from int) []stmt {
	var out []stmt
	for start := from; start < len(src); {
		end := strings.IndexByte(src[start:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += start
		}
		line := src[start:end]
		lead := len(line) - len(strings.TrimLeft(line, " \t\r"))
		if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "%%") {
			out = append(out, stmt{t, start + lead})
		}
		start = end + 1
	}
	return out
}

func (j *journeyParser) statement(st stmt) {
	word, rest := firstWord(st.text)
	restOff := st.off + len(st.text) - len(rest)
	switch {
	case word == "title":
		t, err := j.p.label(rest, restOff)
		if err == nil {
			j.d.Title = t
		}
		return
	case word == "section":
		name, err := j.p.label(rest, restOff)
		if err != nil {
			return
		}
		if name == "" {
			j.p.fail(st.off, "a section needs a name")
			return
		}
		j.d.Sections = append(j.d.Sections, JourneySection{Name: name, Span: span(st)})
		return
	case strings.HasPrefix(st.text, "accTitle") || strings.HasPrefix(st.text, "accDescr"):
		if strings.Contains(st.text, "{") {
			j.p.unsupport(st.off, "accessibility blocks")
		}
		return // accessible text: nothing drawn
	}
	j.task(st)
}

// task is `Name: score` or `Name: score: actor, actor`.
func (j *journeyParser) task(st stmt) {
	parts := strings.Split(st.text, ":")
	if len(parts) < 2 {
		j.p.fail(st.off, "a task is `name: score: actors`")
		return
	}
	if len(parts) > 3 {
		j.p.fail(st.off+len(strings.Join(parts[:3], ":")), "a task has a name, a score and its actors: one colon too many")
		return
	}
	name, err := j.p.label(parts[0], st.off)
	if err != nil {
		return
	}
	if name == "" {
		j.p.fail(st.off, "a task needs a name")
		return
	}
	scoreOff := st.off + len(parts[0]) + 1
	score, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || score < 1 || score > 5 {
		j.p.fail(scoreOff, "a task's score is a whole number from 1 to 5, not %q", strings.TrimSpace(parts[1]))
		return
	}
	t := JourneyTask{Name: name, Score: score, Span: span(st)}
	if len(parts) == 3 {
		for _, a := range strings.Split(parts[2], ",") {
			a = strings.TrimSpace(a)
			if a == "" {
				j.p.fail(scoreOff+len(parts[1])+1, "an empty actor")
				return
			}
			t.Actors = append(t.Actors, a)
			if !j.seen[a] {
				j.seen[a] = true
				j.d.Actors = append(j.d.Actors, a)
			}
		}
	}
	if len(j.d.Sections) == 0 {
		j.d.Sections = append(j.d.Sections, JourneySection{Span: span(st)})
	}
	s := &j.d.Sections[len(j.d.Sections)-1]
	s.Tasks = append(s.Tasks, t)
}
