package widget

import (
	"context"
	"sync"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
)

// DIAGRAM SLOTS — a view's hold on one diagram it asked a Diagrammer for, shared by the Editor's
// blocks and an HTMLView's pres. A slot answers while its request stands. A new request (another
// source, width, scale or theme, or a ready that fired) cancels the old one and keeps its picture
// until the new one lands, so a diagram never flashes back to its source.

// diagramSlot is one diagram's request: what was asked, under which generation, and the last
// picture it had.
type diagramSlot struct {
	key     diagramKey
	to      int // the Editor's block's end line, exclusive; unused by a page
	gen     uint64
	cancel  context.CancelFunc
	state   DiagramState
	refresh bool // ready was called: ask the Diagrammer again on the next lay
	pic     gui.View
	size    gui.Size
}

// diagramKey is a request's identity: everything the picture depends on. A theme change is a
// new request, as an edited source is.
type diagramKey struct {
	lang, src    string
	width, scale float32
	theme        Theme
}

// keyOf is req's identity.
func keyOf(req DiagramRequest) diagramKey {
	return diagramKey{req.Lang, req.Src, req.Width, req.Scale, req.Theme}
}

// current reports whether s (nil: none) answers key as it stands: asked for key, its ready not
// fired.
func (s *diagramSlot) current(key diagramKey) bool { return s != nil && s.key == key && !s.refresh }

// askAfter asks d for req as a new slot under generation gen, after old (nil: none), which it
// cancels. old's picture stands in while the new one is Pending, unless it is another language's:
// a mermaid picture never stands in for a chart. ready makes the request's ready from its context.
func askAfter(old *diagramSlot, d Diagrammer, req DiagramRequest, gen uint64, ready func(context.Context) func()) *diagramSlot {
	key := keyOf(req)
	s := &diagramSlot{key: key, gen: gen, state: Pending}
	if old != nil {
		old.cancel()
		if old.key.lang == key.lang {
			s.pic, s.size = old.pic, old.size
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	a := d.Diagram(ctx, req, ready(ctx))
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
	return s
}

// answer is the slot's diagram answer.
func (s *diagramSlot) answer() DiagramAnswer {
	if s.state == Declined {
		return DiagramAnswer{State: Declined}
	}
	return DiagramAnswer{State: s.state, Pic: s.pic, Size: s.size}
}

// readyTask is a request's ready: called at most once, from any goroutine, it wakes a task on tc
// that brings v to the loop as the task's result. The request's cancellation (ctx) or tc's end
// ends the task with no result, so a withdrawn ready never reaches the loop. ok is false, and
// the ready does nothing, with no Context (tc nil): there is no loop to bring it to.
func readyTask(tc *tui.Context, ctx context.Context, v any) (ready func(), id tui.TaskID, ok bool) {
	if tc == nil {
		return func() {}, id, false
	}
	fired := make(chan struct{})
	var once sync.Once
	id = tc.Go(func(task context.Context) (any, error) {
		select {
		case <-fired:
			return v, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-task.Done():
			return nil, task.Err()
		}
	})
	return func() { once.Do(func() { close(fired) }) }, id, true
}
