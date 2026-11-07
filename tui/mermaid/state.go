package mermaid

import (
	"context"
	"strconv"
	"strings"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// A STATE DIAGRAM IN CELLS: the model adapted to the router's graph (graph.go), laid out by
// layGraph, as a flowchart is.
//
//	        ●
//	        │
//	        ▼
//	╭──────────────╮     a state: rounded, its descriptions in a section under its name
//	│ Still        │
//	╰──────────────╯
//	        │ push      a transition: a line with its label, an arrowhead into its target
//	        ▼
//	╭ Moving ──────────╮  a composite: a frame round its states, its name on its border
//	│       ●          │
//	│       ▼          │
//	│  ╭─────────╮     │
//	│  │ Crash   │     │
//	│  ╰─────────╯     │
//	╰──────────────────╯
//	        ◉            an end
//
// A transition to or from a composite itself ends on its frame's border, as Mermaid draws it: its
// arrowhead just outside the border, or the border taking a junction where its line leaves.
//
// A note is a dashed box (FormNote) tied to its state by a dashed line with no ends. A fork or a join is a bar, a
// choice the diamond's outline. Not drawn here, for the host's own rendering (ErrUnsupported): a
// transition between a composite and a state inside it, or from a composite to itself, a composite
// holding no state, and anything the router can't route.
//
// Not yet: a composite's own direction (it is laid in the diagram's, as a flowchart's subgraph),
// and a note's side (the layout places it, left or right as it lays the ranks).

func init() {
	register(pm.State, layAs(LayState), stateDrawn)
	registerNarrower(pm.State, stateDown)
}

// LayState lays d out in cells, through the router (layGraph): its composites are frames.
// ErrUnsupported for what stateDrawn refuses and for any edge the router can't route;
// layout.ErrTooLarge comes back as it is.
func LayState(ctx context.Context, d *pm.StateDiagram, m Measurer) (*Grid, error) {
	if err := stateDrawn(d); err != nil {
		return nil, err
	}
	g, _, err := layGraph(ctx, stateGraph(d), m)
	return g, err
}

// stateDrawn is ErrUnsupported for a state diagram with a transition whose line couldn't end on its
// composite's border: between a composite and a state inside it, from a composite to itself, or to or
// from a composite holding no state (no frame is drawn round nothing).
func stateDrawn(d pm.Diagram) error {
	sd, ok := d.(*pm.StateDiagram)
	if !ok {
		return ErrUnsupported
	}
	parent, composite, holds := map[string]string{}, map[string]bool{}, map[string]bool{}
	for _, s := range sd.States {
		parent[s.ID] = s.Parent
		composite[s.ID] = s.Composite
	}
	for _, s := range sd.States {
		for p, steps := s.Parent, 0; p != "" && !s.Composite && steps <= len(sd.States); p, steps = parent[p], steps+1 {
			holds[p] = true
		}
	}
	for _, t := range sd.Transitions {
		for _, c := range []string{t.From, t.To} {
			if composite[c] && (!holds[c] || t.From == t.To || within(parent, t.From, c) || within(parent, t.To, c)) {
				return ErrUnsupported
			}
		}
	}
	return nil
}

// within reports whether state id is inside composite c, at any depth (parent: each state's).
func within(parent map[string]string, id, c string) bool {
	for p, steps := parent[id], 0; p != "" && steps <= len(parent); p, steps = parent[p], steps+1 {
		if p == c {
			return true
		}
	}
	return false
}

// stateDown is a state diagram that runs across (LR, RL) as one running down (TB): its narrower
// form (LayWithin); nil for one already running down.
func stateDown(d pm.Diagram) pm.Diagram {
	sd, ok := d.(*pm.StateDiagram)
	if !ok || sd.Dir != pm.LR && sd.Dir != pm.RL {
		return nil
	}
	down := *sd
	down.Dir = pm.TB
	return &down
}

// stateGraph is d as the router's graph:
//   - a node per state that isn't a composite: rounded, its descriptions in a section under its
//     name; a start ●, an end ◉, a fork or join a bar, a choice the diamond's outline;
//   - a frame per composite, nested as the composites are (a frame comes after the one it is in);
//   - an edge per transition, with its label and an arrowhead into its target; one to or from a
//     composite ends on its frame (gedge's toFrame, fromFrame), laid out to the composite's anchor;
//   - a node per note, joined to its state by a dashed edge with no ends.
//
// A transition naming no state, or a composite holding none, is given -1, which layGraph refuses.
func stateGraph(d *pm.StateDiagram) graph {
	gr := graph{dir: d.Dir}
	if gr.dir == 0 {
		gr.dir = pm.TB
	}
	// frames, each after the frame it is nested in: a composite's parent first, then it
	frameOf := map[string]int{}
	var addFrame func(s pm.StateNode)
	byID := map[string]pm.StateNode{}
	for _, s := range d.States {
		byID[s.ID] = s
	}
	addFrame = func(s pm.StateNode) {
		if _, done := frameOf[s.ID]; done {
			return
		}
		parent := -1
		if p, ok := byID[s.Parent]; ok && s.Parent != "" && p.Composite {
			addFrame(p)
			parent = frameOf[p.ID]
		}
		frameOf[s.ID] = len(gr.frames)
		gr.frames = append(gr.frames, gframe{title: s.Label, parent: parent})
	}
	for _, s := range d.States {
		if s.Composite {
			addFrame(s)
		}
	}
	frameIn := func(parent string) int {
		if f, ok := frameOf[parent]; ok && parent != "" {
			return f
		}
		return -1
	}
	index := map[string]int{}
	for _, s := range d.States {
		if s.Composite {
			continue
		}
		index[s.ID] = len(gr.nodes)
		gr.nodes = append(gr.nodes, stateNode(s, frameIn(s.Parent)))
	}
	anchor := stateAnchors(d, index)
	for _, t := range d.Transitions {
		e := gedge{from: -1, to: -1, stroke: StrokeLight, head: EndArrow, label: t.Label}
		from, okf := index[t.From]
		to, okt := index[t.To]
		if f, ok := frameOf[t.From]; ok {
			from, okf, e.fromFrame = anchor(t.From, false), true, f+1
		}
		if f, ok := frameOf[t.To]; ok {
			to, okt, e.toFrame = anchor(t.To, true), true, f+1
		}
		if okf && okt {
			e.from, e.to = from, to
		}
		gr.edges = append(gr.edges, e)
	}
	for i, n := range d.Notes {
		at, ok := index[n.State]
		note := len(gr.nodes)
		frame := -1
		if ok {
			frame = gr.nodes[at].frame
		} else {
			at = -1
		}
		gr.nodes = append(gr.nodes, gnode{id: noteID(i), form: FormNote, label: firstLine(n.Text),
			sections: noteSections(n.Text), frame: frame})
		if at < 0 {
			note = -1
		}
		gr.edges = append(gr.edges, gedge{from: note, to: at, stroke: StrokeDashed, head: EndNone, tail: EndNone})
	}
	return gr
}

// stateAnchors is the node (index: each state's) a transition into composite c (in) or out of it
// (!in) is laid out to, inside its frame, where its line is cut at the border: the composite's start
// for a way in, its end for a way out; else the first state of its own no transition from inside it
// reaches (in), or none leaves for inside it (!in); else its first state; else one in the first
// composite inside it that holds any. -1: it holds no state.
func stateAnchors(d *pm.StateDiagram, index map[string]int) func(c string, in bool) int {
	parent := map[string]string{}
	for _, s := range d.States {
		parent[s.ID] = s.Parent
	}
	var anchor func(c string, in bool, depth int) int
	anchor = func(c string, in bool, depth int) int {
		scope := "[*]end@" + c
		if in {
			scope = "[*]start@" + c
		}
		if i, ok := index[scope]; ok {
			return i
		}
		first := -1
		for _, s := range d.States {
			i, ok := index[s.ID]
			if !ok || s.Parent != c {
				continue
			}
			if first < 0 {
				first = i
			}
			free := true // no transition from inside c reaches it (in), or leaves it for inside c
			for _, t := range d.Transitions {
				self, other := t.To, t.From
				if !in {
					self, other = other, self
				}
				free = free && (self != s.ID || !within(parent, other, c))
			}
			if free {
				return i
			}
		}
		if first >= 0 || depth > len(d.States) {
			return first
		}
		for _, s := range d.States {
			if s.Composite && s.Parent == c {
				if i := anchor(s.ID, in, depth+1); i >= 0 {
					return i
				}
			}
		}
		return -1
	}
	return func(c string, in bool) int { return anchor(c, in, 0) }
}

// stateNode is a state as the router's node, in frame (-1 none).
func stateNode(s pm.StateNode, frame int) gnode {
	n := gnode{id: s.ID, label: s.Label, frame: frame}
	switch s.Kind {
	case pm.StateStart:
		n.form, n.label = FormDot, ""
	case pm.StateEnd:
		n.form, n.label = FormEndDot, ""
	case pm.StateFork, pm.StateJoin:
		n.form, n.label = FormBar, ""
	case pm.StateChoice:
		n.form, n.label = FormOfShape(pm.Diamond), ""
	default:
		n.form = FormRounded
		if len(s.Descriptions) > 0 {
			n.sections = [][]string{{s.Label}, s.Descriptions}
		}
	}
	return n
}

// noteID is note i's Box.ID: no state's id has the prefix, so a host maps a selection back to the
// note, never to a state.
func noteID(i int) string { return "note:" + strconv.Itoa(i) }

// noteSections is a note's text as one section of its lines; nil for a one-line note, which its
// label draws.
func noteSections(text string) [][]string {
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return nil
	}
	return [][]string{lines}
}

// firstLine is text's first line: a multi-line note's label, as a host's selection reads it.
func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}
