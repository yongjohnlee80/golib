package mermaid

import (
	"context"
	"strconv"

	"github.com/yongjohnlee80/golib/graph/layout"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
)

// A sequence diagram in cells: each participant boxed at the top and again at the bottom, a
// lifeline │ between, and the steps down it in source order. A message is a line from the
// sender's lifeline to the receiver's, its text on the row above and its head at the receiver
// (▶ ◀ for an arrow, × for a cross, ▷ for an async one; none for a plain line); a dotted one is
// dashed ╌; one to itself is a loop to its right. A note is a box beside or over its participants,
// and the lifelines it covers break around it. An activation is the lifeline drawn heavy (┃ ┠ ┨),
// never a box, so a message crossing an active participant crosses a line, not a box. A frame
// (loop, alt, opt, par, critical, break, rect) is named in a margin left of the lifelines where it
// opens, branches and ends. With autonumber the messages are numbered.

const (
	// seqMaxParticipants and seqMaxSteps bound a sequence: past them Lay answers
	// layout.ErrTooLarge.
	seqMaxParticipants = 100
	seqMaxSteps        = 2000
	seqGap             = 2 // blank cells at least between two participants' boxes
)

// seqHead is the mark a message's end takes, by its kind and side: [kind][0] pointing east, [1] west.
var seqHead = map[pm.SeqEnd][2]string{
	pm.SeqArrow: {"▶", "◀"},
	pm.SeqCross: {"×", "×"},
	pm.SeqAsync: {"▷", "◁"},
}

// LaySequence lays d out: participants across the top, as far apart as the widest message, note
// or loop between two of them needs, and the steps down their lifelines.
// layout.ErrTooLarge for one past the limits.
func LaySequence(ctx context.Context, d *pm.SequenceDiagram, m Measurer) (*Grid, error) {
	g := newGrid()
	n := len(d.Participants)
	if n > seqMaxParticipants || len(d.Steps) > seqMaxSteps {
		return nil, layout.ErrTooLarge
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if n == 0 {
		g.label(0, 0, d.Title, m)
		g.settle()
		return g, nil
	}
	index := make(map[string]int, n)
	widths := make([]int, n)
	for i, p := range d.Participants {
		index[p.ID] = i
		widths[i], _ = formSize(FormSquare, m.StringWidth(p.Label))
	}
	of := func(id string) (int, bool) { i, ok := index[id]; return i, ok }

	// the messages' texts, numbered while autonumber is on, and the frames' names
	texts := make([]string, len(d.Steps))
	number, step, numbering := 1, 1, false
	margin := 0
	for i, st := range d.Steps {
		switch st.Kind {
		case pm.StepNumber:
			numbering = !st.Off
			if st.Start > 0 {
				number = st.Start
			}
			step = max(st.Increment, 1)
		case pm.StepMessage:
			texts[i] = st.Text
			if numbering {
				texts[i] = strconv.Itoa(number) + ". " + st.Text
				number += step
			}
		case pm.StepBlock, pm.StepBranch, pm.StepEnd:
			texts[i] = frameName(st)
			margin = max(margin, m.StringWidth(texts[i])+seqGap)
		case pm.StepNote:
			texts[i] = st.Text
		}
	}

	// the lifelines' columns: each as far right of the ones before as anything between them needs
	need := make([][]struct{ from, by int }, n) // need[j]: x[j] >= x[from] + by
	want := func(from, to, by int) {
		if from >= 0 && to < n && from < to {
			need[to] = append(need[to], struct{ from, by int }{from, by})
		}
	}
	for i := 1; i < n; i++ {
		// the boxes apart: i-1's right edge, a gap, then i's left edge
		want(i-1, i, widths[i-1]-widths[i-1]/2+seqGap+widths[i]/2)
	}
	for i, st := range d.Steps {
		lw := m.StringWidth(texts[i])
		switch st.Kind {
		case pm.StepMessage:
			a, okA := of(st.From)
			b, okB := of(st.To)
			if !okA || !okB {
				continue
			}
			if a == b {
				want(a, a+1, max(lw+2, 4)+seqGap) // the loop and its text, to the next lifeline
				continue
			}
			want(min(a, b), max(a, b), lw+4)
		case pm.StepNote:
			a, okA := of(st.From)
			if !okA {
				continue
			}
			w, _ := formSize(FormSquare, lw)
			switch st.Place {
			case pm.LeftOf:
				want(a-1, a, w+seqGap+2)
			case pm.RightOf:
				want(a, a+1, w+seqGap+2)
			}
		}
	}
	x := make([]int, n)
	x[0] = margin + widths[0]/2
	for j := 1; j < n; j++ {
		for _, c := range need[j] {
			x[j] = max(x[j], x[c.from]+c.by)
		}
	}

	top := 0
	if d.Title != "" {
		g.label(0, 0, d.Title, m)
		top = 2
	}
	boxAt := func(i, y int) Box {
		return Box{ID: d.Participants[i].ID, Label: d.Participants[i].Label, Form: participantForm(d.Participants[i]), Stem: true,
			Rect: tui.Rect{X: x[i] - widths[i]/2, Y: y, W: widths[i], H: 3}}
	}
	for i := range d.Participants {
		g.Boxes = append(g.Boxes, boxAt(i, top))
	}

	// the steps, row by row
	y := top + 4 // a row of lifeline under the boxes
	type span struct{ from, to int }
	active := make([]int, n)
	since := make([]int, n)
	bars := make([][]span, n)
	activate := func(i, row int) {
		if active[i] == 0 {
			since[i] = row
		}
		active[i]++
	}
	deactivate := func(i, row int) {
		if active[i] == 0 {
			return
		}
		if active[i]--; active[i] == 0 {
			bars[i] = append(bars[i], span{since[i], row})
		}
	}
	var notes []tui.Rect // where the lifelines break
	lifelineX := map[int]bool{}
	for _, xi := range x {
		lifelineX[xi] = true
	}
	for i, st := range d.Steps {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		switch st.Kind {
		case pm.StepMessage:
			a, okA := of(st.From)
			b, okB := of(st.To)
			if !okA || !okB {
				continue
			}
			if a == b {
				g.label(x[a]+2, y, texts[i], m)
				g.selfLoop(x[a], y+1, st)
				y += 3
			} else {
				line := y
				if texts[i] != "" {
					lw := m.StringWidth(texts[i])
					lo, hi := min(x[a], x[b]), max(x[a], x[b])
					g.label(lo+1+(hi-lo-1-lw)/2, y, texts[i], m)
					line = y + 1
				}
				g.message(x[a], x[b], line, st, lifelineX)
				y = line + 1
				if st.Activate {
					activate(b, line)
				}
				if st.Deactivate {
					deactivate(a, line)
				}
			}
		case pm.StepNote:
			a, okA := of(st.From)
			if !okA {
				continue
			}
			lw := m.StringWidth(texts[i])
			w, h := formSize(FormSquare, lw)
			var r tui.Rect
			switch st.Place {
			case pm.LeftOf:
				r = tui.Rect{X: x[a] - 2 - w, Y: y, W: w, H: h}
			case pm.RightOf:
				r = tui.Rect{X: x[a] + 2, Y: y, W: w, H: h}
			default: // over one participant, or from one to another
				b, okB := of(st.To)
				if !okB {
					b = a
				}
				lo, hi := min(x[a], x[b]), max(x[a], x[b])
				w = max(w, hi-lo+3)
				r = tui.Rect{X: lo - (w-(hi-lo))/2, Y: y, W: w, H: h}
			}
			g.Boxes = append(g.Boxes, Box{Label: texts[i], Rect: r, Form: FormSquare})
			notes = append(notes, r)
			y += h + 1
		case pm.StepActivate:
			if a, ok := of(st.From); ok {
				activate(a, y)
			}
		case pm.StepDeactivate:
			if a, ok := of(st.From); ok {
				deactivate(a, y-1)
			}
		case pm.StepBlock, pm.StepBranch, pm.StepEnd:
			g.label(0, y, texts[i], m)
			y++
		}
	}
	bottom := y + 1 // a row of lifeline above the bottom boxes
	for i := range d.Participants {
		for active[i] > 0 {
			deactivate(i, bottom-1)
		}
		g.Boxes = append(g.Boxes, boxAt(i, bottom))
	}

	// the lifelines, broken where a note covers them, heavy where active
	for i, xi := range x {
		inNote := func(row int) bool {
			for _, r := range notes {
				if xi >= r.X && xi < r.X+r.W && row >= r.Y && row < r.Y+r.H {
					return true
				}
			}
			return false
		}
		for row := top + 3; row < bottom; row++ {
			if inNote(row) {
				continue
			}
			c := cell{xi, row}
			if row == top+3 || inNote(row-1) {
				g.lines[c] |= north // up into the box above
			} else {
				g.link(cell{xi, row - 1}, c)
			}
			if row == bottom-1 || inNote(row+1) {
				g.lines[c] |= south // down into the box below
			}
		}
		for _, s := range bars[i] {
			for row := s.from; row <= s.to; row++ {
				g.heavy[cell{xi, row}] = true
			}
		}
	}
	g.settle()
	if g.tooLarge() {
		return nil, layout.ErrTooLarge
	}
	return g, nil
}

// participantForm is a participant's outline: an actor's rounded, as its stick figure would be.
func participantForm(p pm.Participant) Form {
	if p.Actor {
		return FormRounded
	}
	return FormSquare
}

// frameName is a frame's line in the margin: [loop] its text, [else] the next section's, [end].
func frameName(st pm.Step) string {
	word := st.Block.String()
	switch st.Kind {
	case pm.StepBranch:
		word = map[pm.BlockKind]string{pm.Alt: "else", pm.Par: "and", pm.Critical: "option"}[st.Block]
		if word == "" {
			word = "else"
		}
	case pm.StepEnd:
		return "[end]"
	}
	if st.Text == "" {
		return "[" + word + "]"
	}
	return "[" + word + "] " + st.Text
}

// message lines a message from the lifeline at column from to the one at to, on row y: its head
// on the cell before to's lifeline, a tail head (<<->>) on the cell after from's, dashes on the
// rest of a dotted one, every lifeline it crosses a junction.
func (g *Grid) message(from, to, y int, st pm.Step, lifelines map[int]bool) {
	rightward := to > from
	step := 1
	if !rightward {
		step = -1
	}
	g.run(cell{from, y}, cell{to, y})
	side := 0 // the head's arrow points east
	if !rightward {
		side = 1
	}
	if h, ok := seqHead[st.Head]; ok {
		g.marks[cell{to - step, y}] = h[side]
	}
	if h, ok := seqHead[st.Tail]; ok {
		g.marks[cell{from + step, y}] = h[1-side]
	}
	if st.Line == pm.Dotted {
		for c := from + step; c != to; c += step {
			if _, marked := g.marks[cell{c, y}]; !marked && !lifelines[c] {
				g.marks[cell{c, y}] = "╌"
			}
		}
	}
}

// selfLoop lines a message to its sender's own lifeline at column x: out of it on row y, two
// cells right, down a row and back, its head against the lifeline.
func (g *Grid) selfLoop(x, y int, st pm.Step) {
	g.run(cell{x, y}, cell{x + 2, y})
	g.run(cell{x + 2, y}, cell{x + 2, y + 1})
	g.run(cell{x + 2, y + 1}, cell{x, y + 1})
	if h, ok := seqHead[st.Head]; ok {
		g.marks[cell{x + 1, y + 1}] = h[1]
	}
	if st.Line == pm.Dotted {
		g.marks[cell{x + 1, y}] = "╌"
	}
}
