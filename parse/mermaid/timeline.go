package mermaid

import (
	"errors"
	"strings"
)

// TimelineDiagram is a parsed timeline: its title, and its periods in sections, left to right.
type TimelineDiagram struct {
	Title string
	// Sections in source order. Periods before the first section statement, or in a timeline with
	// none, are in a section with no title.
	Sections []TimelineSection
}

func (*TimelineDiagram) Kind() Kind { return Timeline }

// TimelineSection is a section statement and the periods after it, up to the next.
type TimelineSection struct {
	Title   string // "" for the periods before any section statement
	Periods []TimelinePeriod
	Span    [2]int // its section statement; zero for the untitled one
}

// TimelinePeriod is a time period and its events, in order: `2004 : Facebook : Google`, then any
// `: event` lines under it.
type TimelinePeriod struct {
	Label  string
	Events []TimelineEvent
	Span   [2]int // its line
}

// TimelineEvent is one event of a period.
type TimelineEvent struct {
	Text string
	Span [2]int
}

// timelineParser is the parse of one timeline. It holds the parser rather than embedding it, so
// the parser's own statement stays the flowchart's.
type timelineParser struct {
	p *parser
	d *TimelineDiagram
}

// timeline parses a timeline's body: rest is its header line after the keyword, at restOff.
func (p *parser) timeline(rest string, restOff int) (Diagram, error) {
	t := &timelineParser{p: p, d: &TimelineDiagram{}}
	if r := strings.TrimSpace(rest); r != "" {
		p.unsupport(restOff, "a timeline's direction") // timeline LR, timeline TD: outside the subset
		return p.done(nil)
	}
	for _, st := range timelineLines(p.src, restOff+len(rest)) {
		t.statement(st)
		if errors.Is(p.unsupported, ErrTooLarge) {
			return nil, p.unsupported
		}
	}
	return p.done(t.d)
}

// timelineLines splits src[from:] into its non-blank lines, trimmed. A line starting with %% is a
// comment, a %%{ }%% directive included, which is ignored.
func timelineLines(src string, from int) []stmt {
	var out []stmt
	for start := from; start < len(src); {
		end := strings.IndexByte(src[start:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += start
		}
		line := src[start:end]
		if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "%%") {
			out = append(out, stmt{t, start + strings.Index(line, t)})
		}
		start = end + 1
	}
	return out
}

// statement is one line: title, section, an accessibility line, a period with its events, or a
// `: event` line continuing the last period.
func (t *timelineParser) statement(st stmt) {
	w, rest := firstWord(st.text)
	restOff := st.off + len(st.text) - len(rest)
	switch w {
	case "title":
		if s, err := t.p.label(rest, restOff); err == nil {
			t.d.Title = s
		}
		return
	case "section":
		title, err := t.p.label(rest, restOff)
		if err != nil {
			return
		}
		if title == "" {
			t.p.fail(st.off, "a section needs a title")
			return
		}
		t.d.Sections = append(t.d.Sections, TimelineSection{Title: title, Span: [2]int{st.off, st.off + len(st.text)}})
		return
	case "accTitle:", "accDescr:":
		return // accessibility text: nothing drawn
	case "accTitle", "accDescr":
		if strings.HasPrefix(rest, ":") {
			return
		}
		t.p.unsupport(st.off, w+" block")
		return
	}
	if strings.HasPrefix(st.text, ":") {
		p := t.last()
		if p == nil {
			t.p.fail(st.off, "an event with no time period before it")
			return
		}
		t.events(p, st.text, st.off)
		return
	}
	label, _, hasEvents := strings.Cut(st.text, ":")
	text, err := t.p.label(label, st.off)
	if err != nil {
		return
	}
	if text == "" {
		t.p.fail(st.off, "a time period needs a label")
		return
	}
	sec := t.section()
	sec.Periods = append(sec.Periods, TimelinePeriod{Label: text, Span: [2]int{st.off, st.off + len(st.text)}})
	if hasEvents {
		t.events(&sec.Periods[len(sec.Periods)-1], st.text[len(label):], st.off+len(label))
	}
}

// events reads `: a : b`, one event after each colon, into p. s starts at a colon.
func (t *timelineParser) events(p *TimelinePeriod, s string, off int) {
	for s != "" {
		s, off = s[1:], off+1 // past the colon
		next := strings.IndexByte(s, ':')
		if next < 0 {
			next = len(s)
		}
		raw := s[:next]
		text, err := t.p.label(raw, off)
		if err != nil {
			return
		}
		if text == "" {
			t.p.fail(off, "an empty event")
			return
		}
		lead := len(raw) - len(strings.TrimLeft(raw, " \t"))
		p.Events = append(p.Events, TimelineEvent{Text: text, Span: [2]int{off + lead, off + len(strings.TrimRight(raw, " \t\r"))}})
		s, off = s[next:], off+next
	}
}

// section is the section a period goes in: the last, or an untitled one for periods before any.
func (t *timelineParser) section() *TimelineSection {
	if len(t.d.Sections) == 0 {
		t.d.Sections = append(t.d.Sections, TimelineSection{})
	}
	return &t.d.Sections[len(t.d.Sections)-1]
}

// last is the last period of the last section, nil when there is none.
func (t *timelineParser) last() *TimelinePeriod {
	if len(t.d.Sections) == 0 {
		return nil
	}
	s := &t.d.Sections[len(t.d.Sections)-1]
	if len(s.Periods) == 0 {
		return nil
	}
	return &s.Periods[len(s.Periods)-1]
}
