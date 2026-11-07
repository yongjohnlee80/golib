package mermaid

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
)

// A transition into a composite and one out of it, running down and across.
const stateInOutSrc = `stateDiagram-v2
  [*] --> Idle
  Idle --> Busy : start
  state Busy {
    [*] --> Slow
    Slow --> Fast : speed up
    Fast --> [*]
  }
  Busy --> Done : finish
  Done --> [*]
`

// A transition from one composite to another.
const stateBetweenSrc = `stateDiagram-v2
  [*] --> First
  state First {
    [*] --> a
    a --> [*]
  }
  First --> Second
  state Second {
    [*] --> b
    b --> [*]
  }
  Second --> [*]
`

// A composite nested in a composite, a transition into and out of each.
const stateNestedSrc = `stateDiagram-v2
  [*] --> Outer
  state Outer {
    [*] --> Inner
    state Inner {
      [*] --> deep
      deep --> [*]
    }
    Inner --> [*]
  }
  Outer --> [*]
`

// A transition to or from a composite ends on its frame's border, as Mermaid draws it, as the
// goldens: into it, the arrowhead just outside the border; out of it, the border's junction where
// the line leaves; running down and across, between two composites, and into and out of a
// composite nested in another.
func TestATransitionToACompositeEndsOnItsBorder(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"state-composite-in-out-tb", stateInOutSrc},
		{"state-composite-in-out-lr", strings.Replace(stateInOutSrc, "stateDiagram-v2\n", "stateDiagram-v2\n  direction LR\n", 1)},
		{"state-composite-to-composite", stateBetweenSrc},
		{"state-composite-nested", stateNestedSrc},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, got := paint(t, c.src)
			golden(t, c.name, got)
		})
	}
}

// Each transition to or from a composite stops at its frame: its route never reaches the border or
// inside it, and comes to a cell beside it; into the composite, its arrowhead beside the border;
// out of it, the border takes a junction where the line leaves, and the frame's title keeps clear
// of it.
func TestATransitionToACompositeStopsAtItsFrame(t *testing.T) {
	for _, src := range []string{stateInOutSrc, stateBetweenSrc, stateNestedSrc,
		strings.Replace(stateInOutSrc, "stateDiagram-v2\n", "stateDiagram-v2\n  direction LR\n", 1)} {
		sd := mustParse(t, src).(*pm.StateDiagram)
		gr := stateGraph(sd)
		g, routes, err := layGraph(context.Background(), gr, newCells(1, 1))
		if err != nil {
			t.Fatalf("layGraph: %v\n%s", err, src)
		}
		_, placed := graphFrames(gr)
		var frameEnds int
		for i, tr := range sd.Transitions {
			e := gr.edges[i]
			for _, end := range []struct {
				f    int
				mark End
				in   bool
			}{{e.toFrame, e.head, true}, {e.fromFrame, e.tail, false}} {
				if end.f == 0 {
					continue
				}
				frameEnds++
				fr := g.frames[placed[end.f-1]]
				r := fr.rect
				var beside bool
				for _, l := range routes[i] {
					for _, c := range l {
						if c.x >= r.X && c.x < r.X+r.W && c.y >= r.Y && c.y < r.Y+r.H {
							t.Errorf("%s --> %s runs into frame %q at %v", tr.From, tr.To, fr.title, c)
						}
						beside = beside || besideRect(r, c)
					}
				}
				if !beside {
					t.Errorf("%s --> %s doesn't come to frame %q's border", tr.From, tr.To, fr.title)
				}
				var marked, joined bool
				for c, mk := range g.marks {
					marked = marked || besideRect(r, c) && strings.ContainsAny(mk, "▼▲►◄")
				}
				for _, d := range fr.meets {
					joined = joined || d != 0
				}
				if end.in && !marked || !end.in && !joined {
					t.Errorf("%s --> %s at frame %q: an arrowhead beside it %v, a junction on it %v (want the arrowhead into it, the junction out of it)", tr.From, tr.To, fr.title, marked, joined)
				}
			}
		}
		if frameEnds == 0 {
			t.Fatalf("no transition to or from a composite in %q", src)
		}
	}
}

// besideRect reports whether c is outside r, next to its border (not diagonally).
func besideRect(r tui.Rect, c cell) bool {
	inX, inY := c.x >= r.X && c.x < r.X+r.W, c.y >= r.Y && c.y < r.Y+r.H
	return inX && (c.y == r.Y-1 || c.y == r.Y+r.H) || inY && (c.x == r.X-1 || c.x == r.X+r.W)
}

// The router's invariants hold with ends on frames: random graphs (every piece any kind brings, in
// every direction) with some ends moved from a node onto a frame it is in that the edge's other
// end isn't, the node being an anchor as an adapter picks one (checkGraph: the line stops beside
// the frame, never on it or inside it, its mark beside it). One the router can't draw that way is
// refused, never misdrawn; most are drawn.
func TestTheRoutersInvariantsHoldWithEndsOnFrames(t *testing.T) {
	var drawn, refused int
	for seed := range uint64(300) {
		r := rand.New(rand.NewPCG(seed, 0xf2a3))
		gr := randomGraph(r)
		chain := func(n int) []int { // n's frames, innermost first
			var out []int
			for f, steps := gr.nodes[n].frame, 0; f >= 0 && steps <= len(gr.frames); f, steps = gr.frames[f].parent, steps+1 {
				out = append(out, f)
			}
			return out
		}
		onFrame := false
		for i := range gr.edges {
			e := &gr.edges[i]
			if e.from == e.to {
				continue
			}
			for _, end := range []struct {
				n, other int
				f        *int
				in       bool // a way in (the to end): n is reached from outside the frame
			}{{e.to, e.from, &e.toFrame, true}, {e.from, e.to, &e.fromFrame, false}} {
				// the frames n is in that the other end isn't, where n is an anchor as an adapter
				// picks one (gedge): a way in to a node no edge from inside the frame reaches (a
				// composite's start), a way out from one no edge leaves for inside it (its end)
				var outer []int
				for _, f := range chain(end.n) {
					if slices.Contains(chain(end.other), f) {
						continue
					}
					free := true
					for _, o := range gr.edges {
						self, far := o.to, o.from
						if !end.in {
							self, far = far, self
						}
						free = free && (self != end.n || far == end.n || !slices.Contains(chain(far), f))
					}
					if free {
						outer = append(outer, f)
					}
				}
				if len(outer) > 0 && r.IntN(2) == 0 {
					*end.f = outer[r.IntN(len(outer))] + 1
					onFrame = true
				}
			}
		}
		if !onFrame {
			continue
		}
		if _, _, err := layGraph(context.Background(), gr, newCells(1, 1)); err == ErrUnsupported {
			refused++
			continue
		} else if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		drawn++
		checkGraph(t, fmt.Sprintf("seed %d (ends on frames)", seed), gr)
	}
	t.Logf("ends on frames: %d graphs drawn, %d refused", drawn, refused)
	if drawn < 2*refused || drawn < 30 {
		t.Errorf("ends on frames: %d graphs drawn, %d refused; want most drawn", drawn, refused)
	}
}
