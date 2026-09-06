package tui

// Tree mechanics tests: lifecycle order, mount/unmount cascade, LIFO hooks,
// NodeID monotonicity, the comparability identity contract.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLifecycleOrder: exactly Init → Layout → Render on
// mount; remount assigns a fresh, strictly larger NodeID and a fresh Init.
func TestLifecycleOrder(t *testing.T) {
	t.Parallel()
	log := &callLog{}
	child := &probe{name: "c", pref: Size{W: 2, H: 1}, log: log}
	root := NewFlex(Vertical)
	root.Add(child)
	h := startApp(t, root, 4, 2)

	waitFor(t, "first render", func() bool { return child.renders.Load() >= 1 })
	got := log.get()
	want := []string{"c.init", "c.layout", "c.render"}
	if len(got) < 3 {
		t.Fatalf("lifecycle = %v, want at least %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("lifecycle = %v, want prefix %v", got, want)
		}
	}

	firstID := child.nodeID()
	h.onLoop(func() { root.Remove(child) })
	h.onLoop(func() { root.Add(child) }) // remount the same Go value
	waitFor(t, "re-init", func() bool { return child.inits.Load() == 2 })
	if second := child.nodeID(); second <= firstID {
		t.Fatalf("remount NodeID = %d, want strictly larger than %d (never reused)", second, firstID)
	}
}

// TestUnmountCascade: unmounting a depth-3 subtree cancels
// every descendant's Ctx(), runs OnUnmount hooks LIFO, children before
// parents, and a TaskResult posted afterward for a dead ID dead-letters.
func TestUnmountCascade(t *testing.T) {
	t.Parallel()
	log := &callLog{}
	leaf := &probe{name: "leaf", pref: Size{W: 2, H: 1}}
	mid := NewFlex(Vertical)
	top := NewFlex(Vertical)
	mid.Add(leaf)
	top.Add(mid)
	root := NewFlex(Vertical)
	root.Add(top)

	var ctxTop, ctxMid, ctxLeaf context.Context
	cancelled := make(chan string, 3)
	leaf.onInit = func(_ *probe, ctx *Context) {
		ctxLeaf = ctx.Ctx()
		context.AfterFunc(ctxLeaf, func() { cancelled <- "leaf" })
		ctx.OnUnmount(func() { log.add("leaf.hook1") })
		ctx.OnUnmount(func() { log.add("leaf.hook2") })
	}
	h := startApp(t, root, 4, 4)

	var leafID NodeID
	h.onLoop(func() {
		leafID = h.app.byComp[leaf].id
		ctxMid = h.app.byComp[Component(mid)].cctx
		ctxTop = h.app.byComp[Component(top)].cctx
		context.AfterFunc(ctxMid, func() { cancelled <- "mid" })
		context.AfterFunc(ctxTop, func() { cancelled <- "top" })
		// Parent-level hooks to observe children-before-parents ordering.
		h.app.byComp[Component(mid)].hooks = append(h.app.byComp[Component(mid)].hooks,
			func() { log.add("mid.hook") })
	})

	h.onLoop(func() { root.Remove(top) })

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		select {
		case s := <-cancelled:
			seen[s] = true
		case <-time.After(3 * time.Second):
			t.Fatalf("descendant contexts not all cancelled; saw %v", seen)
		}
	}
	// Hook order: leaf hooks LIFO (hook2 then hook1), children before
	// parents (leaf before mid). The Flex's own OnUnmount(ctx=nil) hooks
	// from Init interleave harmlessly — filter ours.
	var hooks []string
	for _, e := range log.get() {
		if strings.Contains(e, "hook") {
			hooks = append(hooks, e)
		}
	}
	want := []string{"leaf.hook2", "leaf.hook1", "mid.hook"}
	if len(hooks) != 3 || hooks[0] != want[0] || hooks[1] != want[1] || hooks[2] != want[2] {
		t.Fatalf("hook order = %v, want %v (LIFO, children before parents)", hooks, want)
	}

	// A result addressed to the dead ID is dropped and counted.
	before := h.app.DeadLetters()
	h.app.Post(TaskResult{Owner: leafID, ID: 42})
	waitFor(t, "dead-letter for dead ID", func() bool { return h.app.DeadLetters() == before+1 })
	if leaf.eventCount() != 0 {
		t.Fatalf("dead component received %d events, want 0", leaf.eventCount())
	}
}

// valueComp is a NON-comparable value component (slice field, value
// receivers) — the misuse case Mount must refuse.
type valueComp struct{ data []int }

func (valueComp) Init(*Context)             {}
func (valueComp) Layout(c Constraints) Size { return c.Constrain(Size{}) }
func (valueComp) Render(Surface)            {}
func (valueComp) HandleEvent(Event) bool    { return false }

// TestMountComparabilityPanic: mounting a component whose
// dynamic type is not comparable panics at Mount with the targeted message;
// the terminal is restored before the panic propagates.
func TestMountComparabilityPanic(t *testing.T) {
	t.Parallel()
	tb := NewTestBackend(4, 2)
	app := NewApp(valueComp{data: []int{1}}, WithBackend(tb))
	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected the comparability panic")
		}
		msg := panicText(rec)
		if !strings.Contains(msg, "is not comparable; use a pointer component") {
			t.Fatalf("panic = %q, want the comparability message", rec)
		}
		if err := tb.Inject(keyEv('x')); err == nil {
			t.Fatal("backend not stopped before the mount panic propagated")
		}
	}()
	_ = app.Run(context.Background())
}

// TestMountSameValueTwicePanics: mounting the same pointer
// twice simultaneously panics.
func TestMountSameValueTwicePanics(t *testing.T) {
	t.Parallel()
	child := &probe{name: "c", pref: Size{W: 2, H: 1}}
	root := NewFlex(Vertical)
	root.Add(child)
	root.Add(child) // same pointer twice
	tb := NewTestBackend(4, 2)
	app := NewApp(root, WithBackend(tb))
	defer func() {
		rec := recover()
		msg := panicText(rec)
		if rec == nil || !strings.Contains(msg, "already mounted") {
			t.Fatalf("panic = %v, want 'already mounted'", rec)
		}
	}()
	_ = app.Run(context.Background())
}

// TestTreeMutationInsideLayoutPanics: Mount/Unmount are illegal inside
// Layout.
func TestTreeMutationInsideLayoutPanics(t *testing.T) {
	t.Parallel()
	extra := &probe{name: "extra"}
	bad := &probe{name: "bad", pref: Size{W: 2, H: 1}}
	bad.onLayout = func(p *probe, c Constraints) Size {
		p.ctx.Mount(extra) // illegal
		return c.Constrain(p.pref)
	}
	tb := NewTestBackend(4, 2)
	app := NewApp(bad, WithBackend(tb), WithPanicPolicy(PanicReturn))
	err := app.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "inside Layout/Render is illegal") {
		t.Fatalf("Run err = %v, want the Layout-mutation panic surfaced via PanicReturn", err)
	}
}
