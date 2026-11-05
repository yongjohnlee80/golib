package mermaid

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Pie is the pie kind's value, declared here and registered in init: the registry owns the list,
// so a new kind edits no shared file.
const Pie Kind = 10

func init() { register(Pie, "pie", []string{"pie"}, (*parser).pie) }

// PieDiagram is a parsed pie: its title, its slices in source order, and whether the legend shows
// each slice's value beside its share.
type PieDiagram struct {
	Title string
	// ShowData is the header's showData, as Mermaid's: the legend shows each slice's value with
	// its share.
	ShowData bool
	Slices   []PieSlice
}

func (*PieDiagram) Kind() Kind { return Pie }

// PieSlice is one slice of a pie: its label and its value, as written.
type PieSlice struct {
	Label string
	Value float64
	Span  [2]int // its statement
}

// Total is the sum of the pie's slices' values, what both renderers divide by.
func (d *PieDiagram) Total() float64 {
	var t float64
	for _, s := range d.Slices {
		t += s.Value
	}
	return t
}

// Share is the slice's fraction of total (0..1); 0 when the total is, so a valueless pie's
// legend shows every share zero and its circle has no angles.
func (s PieSlice) Share(total float64) float64 {
	if total == 0 {
		return 0
	}
	return s.Value / total
}

// pieParser is the parse of one pie. It holds the parser rather than embedding it, so the parser's
// own statement stays the flowchart's.
type pieParser struct {
	p *parser
	d *PieDiagram
}

// pie parses a pie's body: rest is its header line after the keyword, at restOff. The header may
// carry showData and a title — Mermaid's documented example puts the title there — and a title
// statement in the body sets it too. Each other line is one slice, `"label" : value`; a label is
// quoted, and its value a finite number, not negative.
func (p *parser) pie(rest string, restOff int) (Diagram, error) {
	t := &pieParser{p: p, d: &PieDiagram{}}
	s := strings.TrimLeft(rest, " \t")
	off := restOff + len(rest) - len(s)
	if strings.HasPrefix(s, "showData") {
		t.d.ShowData = true
		s = strings.TrimLeft(s[len("showData"):], " \t")
		off = restOff + len(rest) - len(s)
	}
	if s != "" {
		if w, tail, found := strings.Cut(s, " "); !found || w != "title" {
			bad, _ := firstWord(s)
			p.fail(off, "unexpected %q after pie", bad)
		} else if lbl, err := p.label(tail, off+len(s)-len(tail)); err == nil {
			t.d.Title = lbl
		}
	}
	for _, st := range timelineLines(p.src, restOff+len(rest)) {
		t.statement(st)
		if errors.Is(p.unsupported, ErrTooLarge) {
			return nil, p.unsupported
		}
	}
	return p.done(t.d)
}

// statement is one line: a title, an accessibility line, or one slice.
func (t *pieParser) statement(st stmt) {
	w, rest := firstWord(st.text)
	restOff := st.off + len(st.text) - len(rest)
	switch w {
	case "title":
		if s, err := t.p.label(rest, restOff); err == nil {
			t.d.Title = s
		}
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
	t.slice(st)
}

// slice reads one datum, `"label" : value`, into the pie. An unquoted label is wrong in the grammar
// the parser claims — Mermaid's pie labels are quoted — so it is a SyntaxError, not an unsupported
// construct.
func (t *pieParser) slice(st stmt) {
	if !strings.HasPrefix(st.text, `"`) {
		t.p.fail(st.off, "a pie slice wants a quoted label")
		return
	}
	end := strings.IndexByte(st.text[1:], '"')
	if end < 0 {
		t.p.fail(st.off, "a pie slice's label is not closed")
		return
	}
	raw := st.text[:end+2]
	label, err := t.p.label(raw, st.off)
	if err != nil {
		return
	}
	v, ok := t.value(st.text[len(raw):], st.off+len(raw))
	if !ok {
		return
	}
	t.d.Slices = append(t.d.Slices, PieSlice{Label: label, Value: v, Span: [2]int{st.off, st.off + len(st.text)}})
}

// value reads what follows a slice's label: `: value`, a finite number that is not negative. A
// NaN or an infinity would paste as text and draw as an angle no circle has, so they are wrong in
// the grammar the parser claims, like a value that is not a number at all.
func (t *pieParser) value(s string, off int) (float64, bool) {
	lead := len(s) - len(strings.TrimLeft(s, " \t"))
	s, off = s[lead:], off+lead
	if !strings.HasPrefix(s, ":") {
		t.p.fail(off, "a pie slice wants : and its value after its label")
		return 0, false
	}
	s, off = s[1:], off+1
	lead = len(s) - len(strings.TrimLeft(s, " \t"))
	s, off = s[lead:], off+lead
	v, err := strconv.ParseFloat(s, 64)
	switch {
	case err != nil:
		t.p.fail(off, "a pie slice's value %q is not a number", s)
		return 0, false
	case math.IsNaN(v) || math.IsInf(v, 0):
		t.p.fail(off, "a pie slice's value %q is not finite", s)
		return 0, false
	case v < 0:
		t.p.fail(off, "a pie slice's value %q is negative", s)
		return 0, false
	}
	return v, true
}
