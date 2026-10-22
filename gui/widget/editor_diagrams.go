package widget

import (
	"context"

	"github.com/yongjohnlee80/golib/tui"
)

// # Editor Diagram Async Lifecycle Architecture
//
// Below is the lifecycle and task handoff pipeline for inline editor diagrams:
//
//	  pixelLayout.Diagram(req)
//	            │
//	    ┌───────┴───────┐
//	    │ Cache Hit?    │─── Yes ───► Return s.answer() (Ready / Pending)
//	    └───────┬───────┘
//	            │ No (New / Dirty)
//	            ▼
//	  Create diagramSlot(gen++)
//	  Launch d.Diagram(ctx, req, readyFor)
//	            │
//	            ├───────────────► Pending: keep previous picture during re-render
//	            │
//	     (Background Task)
//	            │
//	        ready() fires
//	            ▼
//	    tc.Go Task Result (diagramReady{line, gen})
//	            ▼
//	    pixelLayout.diagramReady(r)
//	            │
//	            ▼
//	    Slot marked refresh=true, block invalidated, body.MarkDirty()

// diagramReady is a request's ready callback, brought to the loop as a task result addressed to
// the editor's body.
type diagramReady struct {
	line int
	gen  uint64
}

// Diagram is the layout's DiagramHost. An unchanged request answers from its slot; a new one,
// or one whose ready has fired, asks d with a context of its own and a ready that the body's
// loop receives. Pending keeps the block's last picture, so an edit never flashes it away.
func (l *pixelLayout) Diagram(d Diagrammer, b Block, req DiagramRequest) DiagramAnswer {
	s := l.diagrams[b.From]
	if s.current(keyOf(req)) {
		return s.answer()
	}
	l.gen++
	gen := l.gen
	s = askAfter(s, d, req, gen, func(ctx context.Context) func() {
		ready, _, _ := readyTask(l.e.body.Context(), ctx, diagramReady{line: b.From, gen: gen})
		return ready
	})
	s.to = b.To
	l.diagrams[b.From] = s
	return s.answer()
}

// diagramReady handles a request's ready on the loop: a current generation marks the slot to
// be asked again and lays its block out anew; an older one is dropped.
func (l *pixelLayout) diagramReady(r diagramReady) bool {
	s := l.diagrams[r.line]
	if s == nil || s.gen != r.gen {
		return false
	}
	s.refresh = true
	l.ensure()
	for i, b := range l.blocks {
		if b.From == r.line && i < len(l.laid) {
			l.laid[i] = nil
		}
	}
	l.e.body.MarkDirty()
	return true
}

// dropDiagrams cancels the requests of blocks that end after line: a change there is inside
// them or above them, so their source or their place has changed. Their next lay asks afresh,
// and each keeps its last picture for its block to show meanwhile, so a change, a whole text set
// each second included, never flashes a drawn diagram back to its source. ensure forgets the
// slot of a line no block starts at any more.
func (l *pixelLayout) dropDiagrams(fromLine int) {
	for _, s := range l.diagrams {
		if s.to > fromLine {
			s.cancel()
			s.refresh = true
		}
	}
}

// forgetDiagrams cancels and forgets the slots no block starts at: the lines were reflowed.
func (l *pixelLayout) forgetDiagrams(blocks []Block) {
	if len(l.diagrams) == 0 {
		return
	}
	starts := make(map[int]bool, len(blocks))
	for _, b := range blocks {
		starts[b.From] = true
	}
	for line, s := range l.diagrams {
		if !starts[line] {
			s.cancel()
			delete(l.diagrams, line)
		}
	}
}

// cancelDiagrams withdraws every request: the body unmounted.
func (l *pixelLayout) cancelDiagrams() {
	for line, s := range l.diagrams {
		s.cancel()
		delete(l.diagrams, line)
	}
}

var _ DiagramHost = (*pixelLayout)(nil)

// diagramResult unwraps a diagram request's ready, delivered as a task result to the body.
func diagramResult(ev tui.Event) (diagramReady, bool) {
	t, ok := ev.(tui.TaskResult)
	if !ok || t.Err != nil {
		return diagramReady{}, false
	}
	r, ok := t.Value.(diagramReady)
	return r, ok
}
