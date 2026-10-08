package widget

import (
	"context"
	"fmt"
	"sync"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
)

// diagramSlot is one block's diagram request: what was asked, under which generation, and the
// last picture the block had.
type diagramSlot struct {
	key     string // lang, source, width and scale: the request's identity
	to      int    // the block's end line, exclusive
	gen     uint64
	cancel  context.CancelFunc
	state   DiagramState
	refresh bool // ready was called: ask the Diagrammer again on the next lay
	pic     gui.View
	size    gui.Size
}

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
	key := fmt.Sprintf("%s\x00%s\x00%g\x00%g", req.Lang, req.Src, req.Width, req.Scale)
	s := l.diagrams[b.From]
	if s != nil && s.key == key && !s.refresh {
		return s.answer()
	}
	var pic gui.View
	var size gui.Size
	if s != nil {
		s.cancel()
		pic, size = s.pic, s.size
	}
	l.gen++
	ctx, cancel := context.WithCancel(context.Background())
	s = &diagramSlot{key: key, to: b.To, gen: l.gen, cancel: cancel, state: Pending, pic: pic, size: size}
	l.diagrams[b.From] = s
	a := d.Diagram(ctx, req, l.readyFor(ctx, b.From, s.gen))
	switch a.State {
	case Ready:
		s.state, s.pic, s.size = Ready, a.Pic, a.Size
		cancel()
	case Declined:
		s.state = Declined
		cancel()
	case Pending:
		if a.Pic != nil {
			s.pic, s.size = a.Pic, a.Size
		}
	}
	return s.answer()
}

func (s *diagramSlot) answer() DiagramAnswer {
	if s.state == Declined {
		return DiagramAnswer{State: Declined}
	}
	return DiagramAnswer{State: s.state, Pic: s.pic, Size: s.size}
}

// readyFor is the ready callback of one request. Called at most once, from any goroutine; it
// wakes a task on the body's Context that returns diagramReady to the body on its loop. The task
// ends without a result when the request is cancelled (an edit, a newer request) or the body
// unmounts, so a withdrawn ready never reaches the loop.
func (l *pixelLayout) readyFor(ctx context.Context, line int, gen uint64) func() {
	tc := l.e.body.Context()
	if tc == nil {
		return func() {}
	}
	fired := make(chan struct{})
	var once sync.Once
	tc.Go(func(task context.Context) (any, error) {
		select {
		case <-fired:
			return diagramReady{line: line, gen: gen}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-task.Done():
			return nil, task.Err()
		}
	})
	return func() { once.Do(func() { close(fired) }) }
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
// them or above them, so their source or their place has changed. Their next lay asks afresh.
func (l *pixelLayout) dropDiagrams(fromLine int) {
	for line, s := range l.diagrams {
		if s.to > fromLine {
			s.cancel()
			delete(l.diagrams, line)
		}
	}
}

// cancelDiagrams withdraws every request: the body unmounted.
func (l *pixelLayout) cancelDiagrams() { l.dropDiagrams(0) }

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
