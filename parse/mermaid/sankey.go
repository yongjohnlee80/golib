package mermaid

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Sankey is the sankey diagram kind's value, declared here and registered in init (kind.go).
const Sankey Kind = 14

func init() {
	register(Sankey, "sankey", []string{"sankey-beta", "sankey"}, (*parser).sankey)
}

// SankeyDiagram is a parsed sankey diagram: its nodes, in order of first mention, and the flows
// between them, each from a source to a target with its amount.
type SankeyDiagram struct {
	Nodes []string
	Links []SankeyLink // in source order
}

func (*SankeyDiagram) Kind() Kind { return Sankey }

// SankeyLink is a flow: from node Source to node Target (indexes into Nodes), Value of it.
type SankeyLink struct {
	Source, Target int
	Value          float64
	Span           [2]int // its line
}

// In is how much flows into node i, and Out how much flows out of it.
func (d *SankeyDiagram) In(i int) float64 {
	var v float64
	for _, l := range d.Links {
		if l.Target == i {
			v += l.Value
		}
	}
	return v
}

func (d *SankeyDiagram) Out(i int) float64 {
	var v float64
	for _, l := range d.Links {
		if l.Source == i {
			v += l.Value
		}
	}
	return v
}

// Through is how much passes through node i: the larger of what flows in and what flows out (a
// source has only an out, a sink only an in).
func (d *SankeyDiagram) Through(i int) float64 { return max(d.In(i), d.Out(i)) }

// Depths are each node's column, left to right: a source 0, any other one more than the deepest
// node flowing into it. The parser refuses a cycle, so every node has one.
func (d *SankeyDiagram) Depths() []int {
	depth := make([]int, len(d.Nodes))
	for range d.Nodes { // relax: a node's depth settles within as many passes as there are nodes
		changed := false
		for _, l := range d.Links {
			if depth[l.Target] < depth[l.Source]+1 {
				depth[l.Target] = depth[l.Source] + 1
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return depth
}

// sankey parses a sankey diagram: its header takes nothing, and each line is a flow, three CSV
// fields `source,target,value` (a field in "quotes" may hold commas, "" a quote). A value is a
// finite number above 0, a node's name isn't empty, and the flows make no cycle (a sankey runs one
// way): each is a syntax error at its line.
func (p *parser) sankey(rest string, restOff int) (Diagram, error) {
	d := &SankeyDiagram{}
	if s := strings.TrimSpace(rest); s != "" {
		bad, _ := firstWord(s)
		p.fail(restOff+strings.Index(rest, s), "unexpected %q after sankey", bad)
	}
	index := map[string]int{}
	node := func(name string) int {
		if i, ok := index[name]; ok {
			return i
		}
		index[name] = len(d.Nodes)
		d.Nodes = append(d.Nodes, name)
		return index[name]
	}
	for _, st := range timelineLines(p.src, restOff+len(rest)) {
		fields, ok := sankeyFields(st.text)
		if !ok {
			p.fail(st.off, "a quoted field is not closed")
			continue
		}
		if len(fields) != 3 {
			p.fail(st.off, "a sankey line wants three fields, source,target,value: it has %d", len(fields))
			continue
		}
		src, dst := strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1])
		if src == "" || dst == "" {
			p.fail(st.off, "a sankey line's source and target are names: one is empty")
			continue
		}
		for _, name := range []string{src, dst} {
			if len(name) > MaxLabel { // too long a name: the parser's own refusal, as for any label
				if _, err := p.label(name, st.off); errors.Is(err, ErrTooLarge) {
					return nil, p.unsupported
				}
			}
		}
		raw := strings.TrimSpace(fields[2])
		v, err := strconv.ParseFloat(raw, 64)
		switch {
		case err != nil:
			p.fail(st.off, "a sankey line's value %q is not a number", raw)
			continue
		case math.IsNaN(v) || math.IsInf(v, 0):
			p.fail(st.off, "a sankey line's value %q is not finite", raw)
			continue
		case v <= 0:
			p.fail(st.off, "a sankey line's value %q is not above 0", raw)
			continue
		}
		s, t := node(src), node(dst)
		if s == t {
			p.fail(st.off, "%q flows into itself: a sankey runs one way", src)
			continue
		}
		d.Links = append(d.Links, SankeyLink{Source: s, Target: t, Value: v, Span: [2]int{st.off, st.off + len(st.text)}})
		if sankeyCycle(d) {
			p.fail(st.off, "%q to %q closes a cycle: a sankey runs one way", src, dst)
			d.Links = d.Links[:len(d.Links)-1]
		}
	}
	return p.done(d)
}

// sankeyFields splits a CSV line: commas outside quotes, a quoted field's quotes removed and "" in
// it a quote. ok is false when a quote is left open.
func sankeyFields(line string) (fields []string, ok bool) {
	var b strings.Builder
	quoted := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"' && quoted && i+1 < len(line) && line[i+1] == '"':
			b.WriteByte('"')
			i++
		case c == '"':
			quoted = !quoted
		case c == ',' && !quoted:
			fields = append(fields, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	return append(fields, b.String()), !quoted
}

// sankeyCycle reports whether d's links go round in a cycle: a depth-first walk meets a node still
// on its path.
func sankeyCycle(d *SankeyDiagram) bool {
	out := make([][]int, len(d.Nodes))
	for _, l := range d.Links {
		out[l.Source] = append(out[l.Source], l.Target)
	}
	state := make([]uint8, len(d.Nodes)) // 0 unseen, 1 on the path, 2 done
	var walk func(int) bool
	walk = func(n int) bool {
		state[n] = 1
		for _, m := range out[n] {
			if state[m] == 1 || state[m] == 0 && walk(m) {
				return true
			}
		}
		state[n] = 2
		return false
	}
	for n := range d.Nodes {
		if state[n] == 0 && walk(n) {
			return true
		}
	}
	return false
}
