package widget

import (
	"container/list"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/mermaid"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// Diagrams chains golib's native Mermaid, then fallback (nil: none) for what native declines: a
// diagram type it does not draw (mermaid.ErrUnsupported) or one over a limit (ErrTooLarge). A
// malformed diagram is Ready: a picture of the error and its line, in place.
//
// One Diagrams serves a window: its cache of laid diagrams (an LRU of 64) is shared by every block
// that asks, and each diagram is laid out once, by one job on a worker, whoever else waits for it.
// A job is cancelled when its last waiter withdraws; one that finishes fills the cache whether or
// not anyone still waits, so a block shown again finds it.
func Diagrams(fallback Diagrammer) Diagrammer {
	return &diagrams{fallback: fallback, jobs: map[diagramHash]*diagramJob{}, cache: newLaidCache(64)}
}

type diagramHash [32]byte

type diagrams struct {
	fallback Diagrammer
	mu       sync.Mutex
	jobs     map[diagramHash]*diagramJob
	cache    *laidCache
}

// diagramJob is one diagram being laid out, and who waits for it.
type diagramJob struct {
	cancel  context.CancelFunc
	waiters map[int]*diagramWaiter
	next    int
}

type diagramWaiter struct {
	ready func()
	stop  func() bool // stops the waiter's withdrawal; false once its ctx is done
}

// laidResult is a diagram's layout, or why there is none.
type laidResult struct {
	laid *mermaid.Laid
	err  error
}

func (d *diagrams) Diagram(ctx context.Context, req DiagramRequest, ready func()) DiagramAnswer {
	if req.Lang != "mermaid" {
		return d.fall(ctx, req, ready)
	}
	k := hashOf(req)
	d.mu.Lock()
	if r, ok := d.cache.get(k); ok {
		d.mu.Unlock()
		return d.answer(ctx, req, ready, r)
	}
	j := d.jobs[k]
	if j == nil {
		j = d.start(k, req)
	}
	id := j.next
	j.next++
	w := &diagramWaiter{ready: ready}
	j.waiters[id] = w
	w.stop = context.AfterFunc(ctx, func() { d.leave(k, id) })
	d.mu.Unlock()
	return DiagramAnswer{State: Pending, Size: estimate(req)}
}

// start runs a job laying out req, on a worker of its own, with a measurer it holds for the job.
// Called with d.mu held.
func (d *diagrams) start(k diagramHash, req DiagramRequest) *diagramJob {
	ctx, cancel := context.WithCancel(context.Background())
	j := &diagramJob{cancel: cancel, waiters: map[int]*diagramWaiter{}}
	d.jobs[k] = j
	th := mermaidTheme(req.Theme)
	go func() {
		m, release := gui.AcquireMeasurer(req.Scale)
		laid, err := mermaid.Lay(ctx, req.Src, req.Width, th, m)
		release()
		d.finish(k, j, ctx, laidResult{laid, err})
	}()
	return j
}

// finish caches a job's result (not a cancelled one's) and calls each waiter's ready, once, unless
// its ctx was done first.
func (d *diagrams) finish(k diagramHash, j *diagramJob, ctx context.Context, r laidResult) {
	d.mu.Lock()
	if d.jobs[k] == j {
		delete(d.jobs, k)
	}
	if ctx.Err() == nil {
		d.cache.put(k, r)
	}
	waiters := j.waiters
	j.waiters = nil
	d.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	for _, w := range waiters {
		if w.stop() { // its ctx was not done: it still waits
			w.ready()
		}
	}
}

// leave withdraws waiter id; the last one out cancels the job.
func (d *diagrams) leave(k diagramHash, id int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	j := d.jobs[k]
	if j == nil || j.waiters == nil {
		return
	}
	delete(j.waiters, id)
	if len(j.waiters) == 0 {
		j.cancel()
		delete(d.jobs, k)
	}
}

// answer is a cached result's answer: the picture, the error's picture, or the fallback's.
func (d *diagrams) answer(ctx context.Context, req DiagramRequest, ready func(), r laidResult) DiagramAnswer {
	var se *pm.SyntaxError
	switch {
	case r.err == nil:
		return DiagramAnswer{State: Ready, Pic: r.laid, Size: r.laid.Size()}
	case errors.As(r.err, &se):
		pic, size := syntaxPicture(se, req)
		return DiagramAnswer{State: Ready, Pic: pic, Size: size}
	case errors.Is(r.err, pm.ErrUnsupported), errors.Is(r.err, pm.ErrTooLarge):
		return d.fall(ctx, req, ready)
	}
	return DiagramAnswer{State: Declined}
}

func (d *diagrams) fall(ctx context.Context, req DiagramRequest, ready func()) DiagramAnswer {
	if d.fallback == nil {
		return DiagramAnswer{State: Declined}
	}
	return d.fallback.Diagram(ctx, req, ready)
}

// hashOf is a request's cache key: its source, its theme, its width in device pixels and its scale.
// Two widths that wrap differently are two keys.
func hashOf(req DiagramRequest) diagramHash {
	device := int(math.Round(float64(req.Width * max(req.Scale, 1))))
	return sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%g\x00%v", req.Lang, req.Src, device, req.Scale, req.Theme)))
}

// estimate is a pending diagram's size before it is laid out: the width, and its source's lines.
func estimate(req DiagramRequest) gui.Size {
	lines := strings.Count(req.Src, "\n") + 1
	return gui.Size{W: req.Width, H: float32(lines) * max(req.Theme.Prose.Size, 10) * 1.4}
}

// mermaidTheme is a diagram's theme from the editor's: its page and text, the Raw view's colours
// for the strokes, and its prose font a little smaller.
func mermaidTheme(th Theme) mermaid.Theme {
	stroke := or(th.Heading, th.Accent)
	f := th.Prose
	f.Size = max(f.Size*0.9, 10)
	return mermaid.Theme{
		Background:    th.Background,
		Text:          th.Text,
		Line:          mix(th.Text, th.Background, 0.6),
		NodeFill:      mix(stroke, th.Background, 0.12),
		NodeStroke:    mix(stroke, th.Background, 0.7),
		ClusterFill:   mix(th.Text, th.Background, 0.04),
		ClusterStroke: mix(th.Text, th.Background, 0.3),
		Font:          f,
	}
}

// syntaxPicture is a malformed diagram's place in the view: the error and its line, muted, on the
// code's tint, so the writer sees what to fix where the diagram would be.
func syntaxPicture(se *pm.SyntaxError, req DiagramRequest) (gui.View, gui.Size) {
	th := req.Theme
	f := th.Prose
	f.Size = max(f.Size*0.9, 10)
	text := fmt.Sprintf("Mermaid: %s (line %d, column %d)", se.Msg, se.Line, se.Col)
	size := gui.Size{W: req.Width, H: f.Size * 2.4}
	return gui.ViewFunc(func(c gui.Canvas) {
		c.FillRect(gui.Rect{W: size.W, H: size.H}, gui.Solid(th.CodeBackground))
		t := c.Text().Layout(text, f, size.W-f.Size)
		c.DrawText(t, gui.Pt(f.Size*0.5, (size.H-t.Height)/2), gui.Solid(or(th.Quiet, th.Muted)))
	}), size
}

// laidCache is an LRU of laid diagrams by key.
type laidCache struct {
	max   int
	order *list.List // front: most recent; values are diagramHash
	items map[diagramHash]*list.Element
	vals  map[diagramHash]laidResult
}

func newLaidCache(n int) *laidCache {
	return &laidCache{max: n, order: list.New(), items: map[diagramHash]*list.Element{}, vals: map[diagramHash]laidResult{}}
}

func (c *laidCache) get(k diagramHash) (laidResult, bool) {
	e, ok := c.items[k]
	if !ok {
		return laidResult{}, false
	}
	c.order.MoveToFront(e)
	return c.vals[k], true
}

func (c *laidCache) put(k diagramHash, r laidResult) {
	if e, ok := c.items[k]; ok {
		c.order.MoveToFront(e)
		c.vals[k] = r
		return
	}
	c.items[k] = c.order.PushFront(k)
	c.vals[k] = r
	for c.order.Len() > c.max {
		last := c.order.Back()
		old := last.Value.(diagramHash)
		c.order.Remove(last)
		delete(c.items, old)
		delete(c.vals, old)
	}
}
