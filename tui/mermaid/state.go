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
// A note is a dashed box (FormNote) tied to its state by a dashed line with no ends. A fork or a join is a bar, a
// choice the diamond's outline. Not drawn here, for the host's own rendering (ErrUnsupported): a
// transition to or from a composite itself, which would end on its frame's border, and anything the
// router can't route.
//
// Not yet: a composite's own direction (it is laid in the diagram's, as a flowchart's subgraph),
// and a note's side (the layout places it, left or right as it lays the ranks).

func init() {
	register(pm.State, layAs(LayState), stateDrawn)
	registerNarrower(pm.State, stateDown)
}

// LayState lays d out in cells, through the router (layGraph): its composites are frames.
// ErrUnsupported for a transition to or from a composite (stateDrawn) and for any edge the router
// can't route; layout.ErrTooLarge comes back as it is.
func LayState(ctx context.Context, d *pm.StateDiagram, m Measurer) (*Grid, error) {
	if err := stateDrawn(d); err != nil {
		return nil, err
	}
	g, _, err := layGraph(ctx, stateGraph(d), m)
	return g, err
}

// stateDrawn is ErrUnsupported for a state diagram with a transition to or from a composite state:
// its line would end on the composite's frame, which the router doesn't draw yet.
func stateDrawn(d pm.Diagram) error {
	sd, ok := d.(*pm.StateDiagram)
	if !ok {
		return ErrUnsupported
	}
	composite := map[string]bool{}
	for _, s := range sd.States {
		if s.Composite {
			composite[s.ID] = true
		}
	}
	for _, t := range sd.Transitions {
		if composite[t.From] || composite[t.To] {
			return ErrUnsupported
		}
	}
	return nil
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
//   - an edge per transition, with its label and an arrowhead into its target;
//   - a node per note, joined to its state by a dashed edge with no ends.
//
// A transition naming a composite is refused before this (stateDrawn); one naming no state is
// given -1, which layGraph refuses.
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
	for _, t := range d.Transitions {
		from, okf := index[t.From]
		to, okt := index[t.To]
		if !okf || !okt {
			from, to = -1, -1
		}
		gr.edges = append(gr.edges, gedge{from: from, to: to, stroke: StrokeLight, head: EndArrow, label: t.Label})
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
