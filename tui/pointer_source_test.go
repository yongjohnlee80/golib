package tui

import (
	"sync"
	"testing"
)

// resolverBackend resolves a pointer event's Source as a position in cells (its X, Y in whole
// cells, the fraction as SubX, SubY), and records the goroutine and the events it resolved.
type resolverBackend struct {
	*TestBackend
	mu    sync.Mutex
	gids  []int64
	calls []MouseEvent
}

func (b *resolverBackend) ResolvePointer(ev MouseEvent) MouseEvent {
	b.mu.Lock()
	b.gids = append(b.gids, goid())
	b.calls = append(b.calls, ev)
	b.mu.Unlock()
	ev.X, ev.Y = int(ev.Source.X), int(ev.Source.Y)
	ev.SubX, ev.SubY = ev.Source.X-float32(ev.X), ev.Source.Y-float32(ev.Y)
	return ev
}

func (b *resolverBackend) resolved() ([]int64, []MouseEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]int64(nil), b.gids...), append([]MouseEvent(nil), b.calls...)
}

type frameTok struct{ n int }

// pointerApp is a root with one child probe at cells (2, 1, 4, 2), over a resolverBackend.
func pointerApp(t *testing.T) (*harness, *resolverBackend, *probe) {
	t.Helper()
	child := &probe{name: "child"}
	root := &box{rects: []Rect{{X: 2, Y: 1, W: 4, H: 2}}}
	root.Add(child)
	rb := &resolverBackend{TestBackend: NewTestBackend(8, 4)}
	h := runApp(t, NewApp(root, WithBackend(rb), WithMinFrameInterval(0)), rb.TestBackend)
	return h, rb, child
}

func mouseEvents(p *probe) []MouseEvent {
	var out []MouseEvent
	for _, ev := range probeEvents(p) {
		if m, ok := ev.(MouseEvent); ok {
			out = append(out, m)
		}
	}
	return out
}

// A marked event is resolved on the loop goroutine, before routing: the producer's X and Y are
// ignored, the resolved position routes it, and Source is cleared before any component sees it.
func TestAPointerSourceIsResolvedAtDispatchBeforeRouting(t *testing.T) {
	h, rb, child := pointerApp(t)
	tok := &frameTok{1}
	// X, Y 0, 0 is outside the child: only the resolved (3.25, 2.75) reaches it.
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, Source: PointerSource{X: 3.25, Y: 2.75, Ref: tok}})
	waitFor(t, "the press", func() bool { return len(mouseEvents(child)) > 0 })
	got := mouseEvents(child)[0]
	if got.X != 1 || got.Y != 1 || got.SubX != 0.25 || got.SubY != 0.75 {
		t.Fatalf("delivered %+v; want local 1,1 with SubX 0.25, SubY 0.75 from the resolved source", got)
	}
	if got.Source != (PointerSource{}) {
		t.Fatalf("delivered with Source %+v; it must be cleared at dispatch", got.Source)
	}
	gids, calls := rb.resolved()
	if len(calls) != 1 || calls[0].Source.Ref != tok {
		t.Fatalf("resolver saw %+v; want the one marked event with its own Ref", calls)
	}
	if loop := h.loopGid(); gids[0] != loop {
		t.Fatalf("resolved on goroutine %d; want the loop's %d", gids[0], loop)
	}
}

// Click counting reads the resolved position: two presses resolved to the same place are a
// double click, though their producer X, Y differ.
func TestClickCountingUsesTheResolvedPosition(t *testing.T) {
	h, _, child := pointerApp(t)
	src := PointerSource{X: 3.5, Y: 1.5, Ref: &frameTok{1}}
	h.inject(
		MouseEvent{Kind: MousePress, Button: MouseLeft, X: 0, Y: 0, Source: src},
		MouseEvent{Kind: MouseRelease, Button: MouseLeft, X: 7, Y: 3, Source: src},
		MouseEvent{Kind: MousePress, Button: MouseLeft, X: 5, Y: 0, Source: src},
	)
	waitFor(t, "two presses", func() bool {
		n := 0
		for _, m := range mouseEvents(child) {
			if m.Kind == MousePress {
				n++
			}
		}
		return n == 2
	})
	var counts []int
	for _, m := range mouseEvents(child) {
		if m.Kind == MousePress {
			counts = append(counts, m.Count)
		}
	}
	if counts[0] != 1 || counts[1] != 2 {
		t.Fatalf("press counts %v; want 1, 2: both presses resolve to the same cell", counts)
	}
}

// An event without a Ref is the producer's as sent: the resolver is not called.
func TestAnUnmarkedEventIsNotResolved(t *testing.T) {
	h, rb, child := pointerApp(t)
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 3, Y: 2, SubX: 0.5, Source: PointerSource{X: 99, Y: 99}})
	waitFor(t, "the press", func() bool { return len(mouseEvents(child)) > 0 })
	if got := mouseEvents(child)[0]; got.X != 1 || got.Y != 1 || got.SubX != 0.5 {
		t.Fatalf("delivered %+v; want the producer's 3,2 as local 1,1", got)
	}
	if _, calls := rb.resolved(); len(calls) != 0 {
		t.Fatalf("resolver called for an unmarked event: %+v", calls)
	}
}

// A backend without the capability routes a marked event by the X, Y it was sent with.
func TestAMarkedEventWithoutAResolverRoutesAsSent(t *testing.T) {
	child := &probe{name: "child"}
	root := &box{rects: []Rect{{X: 2, Y: 1, W: 4, H: 2}}}
	root.Add(child)
	h := startApp(t, root, 8, 4)
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 3, Y: 2, Source: PointerSource{X: 99, Ref: &frameTok{1}}})
	waitFor(t, "the press", func() bool { return len(mouseEvents(child)) > 0 })
	got := mouseEvents(child)[0]
	if got.X != 1 || got.Y != 1 || got.Source != (PointerSource{}) {
		t.Fatalf("delivered %+v; want local 1,1 and a cleared Source", got)
	}
}

// With the App stalled, a Press, a thousand motions (coalesced by intake) and a Release
// each resolve from their own Source, however long they waited and however many came between.
func TestEventsQueuedThroughAStallResolveFromTheirOwnSources(t *testing.T) {
	h, rb, child := pointerApp(t)
	release := make(chan struct{})
	stalled := make(chan struct{})
	h.app.Update(func() { close(stalled); <-release })
	<-stalled
	tok := &frameTok{1}
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, Source: PointerSource{X: 3.25, Y: 1.5, Ref: tok}})
	for i := range 1000 {
		h.inject(MouseEvent{Kind: MouseMotion, Button: MouseLeft, Source: PointerSource{X: 2 + float32(i%4), Y: 1.5, Ref: tok}})
	}
	h.inject(MouseEvent{Kind: MouseRelease, Button: MouseLeft, Source: PointerSource{X: 4.75, Y: 2.5, Ref: tok}})
	close(release)
	waitFor(t, "the release", func() bool {
		for _, m := range mouseEvents(child) {
			if m.Kind == MouseRelease {
				return true
			}
		}
		return false
	})
	var press, rel *MouseEvent
	for _, m := range mouseEvents(child) {
		switch m.Kind {
		case MousePress:
			press = &m
		case MouseRelease:
			rel = &m
		}
	}
	if press == nil || press.X != 1 || press.Y != 0 || press.SubX != 0.25 {
		t.Fatalf("press %+v; want local 1,0 SubX 0.25, from its own source", press)
	}
	if rel.X != 2 || rel.Y != 1 || rel.SubX != 0.75 {
		t.Fatalf("release %+v; want local 2,1 SubX 0.75, from its own source", rel)
	}
	if _, calls := rb.resolved(); len(calls) > 1000 {
		t.Fatalf("%d events resolved; intake should have coalesced the motions", len(calls))
	}
}
