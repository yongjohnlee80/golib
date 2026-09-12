package tui

// App.Go tests: addressed results on the loop goroutine, dead-lettering,
// exclusive preemption, staleness IDs, panic isolation, pool bound,
// TaskProgress streaming.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// taskEvents filters a probe's recorded events.
func taskResults(p *probe) []TaskResult {
	var out []TaskResult
	for _, ev := range p.recorded() {
		if r, ok := ev.(TaskResult); ok {
			out = append(out, r)
		}
	}
	return out
}

func taskProgresses(p *probe) []TaskProgress {
	var out []TaskProgress
	for _, ev := range p.recorded() {
		if r, ok := ev.(TaskProgress); ok {
			out = append(out, r)
		}
	}
	return out
}

// TestTaskResultAddressedToOwner: ctx.Go from component X
// delivers TaskResult{Owner: X.ID} to X's HandleEvent on the loop goroutine.
func TestTaskResultAddressedToOwner(t *testing.T) {
	t.Parallel()
	child := &probe{name: "child", pref: Size{W: 2, H: 1}}
	var taskID atomic.Uint64
	child.onInit = func(_ *probe, ctx *Context) {
		taskID.Store(uint64(ctx.Go(func(context.Context) (any, error) {
			return "payload", nil
		})))
	}
	root := NewFlex(Vertical)
	root.Add(child)
	h := startApp(t, root, 4, 2)
	loopGid := h.loopGid()

	waitFor(t, "task result delivery", func() bool { return len(taskResults(child)) == 1 })
	r := taskResults(child)[0]
	if r.Owner != child.nodeID() {
		t.Errorf("result Owner = %d, want %d", r.Owner, child.nodeID())
	}
	if r.ID != TaskID(taskID.Load()) {
		t.Errorf("result ID = %d, want %d", r.ID, taskID.Load())
	}
	if r.Value != "payload" || r.Err != nil {
		t.Errorf("result = %+v, want Value payload, nil Err", r)
	}
	if child.lastGid.Load() != loopGid {
		t.Errorf("result handled on goroutine %d, want loop %d", child.lastGid.Load(), loopGid)
	}
}

// TestDeadLetterAfterUnmount: unmounting the owner first
// dead-letters the result (count exposed), no method of the owner is
// invoked, and the in-flight task observes ctx.Err() != nil.
func TestDeadLetterAfterUnmount(t *testing.T) {
	t.Parallel()
	child := &probe{name: "child", pref: Size{W: 2, H: 1}}
	ctxErr := make(chan error, 1)
	taskRunning := make(chan struct{})
	child.onInit = func(_ *probe, ctx *Context) {
		ctx.Go(func(tctx context.Context) (any, error) {
			close(taskRunning)
			<-tctx.Done() // runs until unmount cancels it
			ctxErr <- tctx.Err()
			return nil, tctx.Err()
		})
	}
	root := NewFlex(Vertical)
	root.Add(child)
	h := startApp(t, root, 4, 2)

	select {
	case <-taskRunning: // the task must be IN FLIGHT before the unmount
	case <-time.After(3 * time.Second):
		t.Fatal("task never started")
	}
	h.onLoop(func() { root.Remove(child) })
	select {
	case err := <-ctxErr:
		if err == nil {
			t.Fatal("task ctx not cancelled by unmount")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight task never observed unmount cancellation")
	}
	waitFor(t, "dead-letter count", func() bool { return h.app.DeadLetters() == 1 })
	if got := len(taskResults(child)); got != 0 {
		t.Fatalf("unmounted owner received %d results, want 0", got)
	}
	countAfterUnmount := child.eventCount()
	h.sync()
	if child.eventCount() != countAfterUnmount {
		t.Fatal("a method of the unmounted component was invoked after unmount")
	}
}

// TestExclusivePreemptionAndStaleness: the first
// Exclusive("search") task observes cancellation; both results arrive;
// TaskIDs strictly increase; last-request-wins discards the stale one.
func TestExclusivePreemptionAndStaleness(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2)
	owner := root.nodeID()

	running := make(chan struct{}, 1)
	id1 := h.app.Go(owner, func(tctx context.Context) (any, error) {
		running <- struct{}{}
		<-tctx.Done() // preempted by the second task
		return "stale", tctx.Err()
	}, Exclusive("search"))
	select {
	case <-running:
	case <-time.After(3 * time.Second):
		t.Fatal("first task never started")
	}
	id2 := h.app.Go(owner, func(context.Context) (any, error) {
		return "fresh", nil
	}, Exclusive("search"))

	if id2 <= id1 {
		t.Fatalf("TaskIDs not strictly increasing: %d then %d", id1, id2)
	}
	waitFor(t, "both results", func() bool { return len(taskResults(root)) == 2 })

	latest := id2 // the app idiom: store the latest request's ID
	var kept []TaskResult
	for _, r := range taskResults(root) {
		if r.ID == id1 {
			if !errors.Is(r.Err, context.Canceled) {
				t.Errorf("preempted task Err = %v, want context.Canceled", r.Err)
			}
		}
		if r.ID >= latest {
			kept = append(kept, r)
		}
	}
	if len(kept) != 1 || kept[0].Value != "fresh" {
		t.Fatalf("staleness filter kept %+v, want exactly the fresh result", kept)
	}
}

// TestTaskPanicIsolation: a panicking task produces
// errors.Is(res.Err, ErrTaskPanic), the app keeps processing events, and
// the recovered stack appears via WithLogger.
func TestTaskPanicIsolation(t *testing.T) {
	t.Parallel()
	var lc logCapture
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2, WithLogger(lc.logger()))

	h.app.Go(root.nodeID(), func(context.Context) (any, error) {
		panic("task exploded")
	})
	waitFor(t, "panicked task result", func() bool { return len(taskResults(root)) == 1 })
	r := taskResults(root)[0]
	if !errors.Is(r.Err, ErrTaskPanic) {
		t.Fatalf("result Err = %v, want errors.Is(_, ErrTaskPanic)", r.Err)
	}
	if !lc.has("task panic") {
		t.Fatal("recovered task panic (with stack) was not logged via WithLogger")
	}
	// The app keeps processing events.
	before := root.eventCount()
	h.inject(keyEv('k'))
	waitFor(t, "post-panic event processing", func() bool { return root.eventCount() > before })
}

// TestTaskPoolBound: with WithTaskPoolSize(2), 10 queued
// tasks never exceed 2 running concurrently; cancelling a queued task
// prevents it from ever starting.
func TestTaskPoolBound(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2, WithTaskPoolSize(2))
	owner := root.nodeID()

	var running, highWater atomic.Int64
	for i := 0; i < 10; i++ {
		h.app.Go(owner, func(context.Context) (any, error) {
			n := running.Add(1)
			for { // atomic max
				hw := highWater.Load()
				if n <= hw || highWater.CompareAndSwap(hw, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			running.Add(-1)
			return nil, nil
		})
	}
	waitFor(t, "all pool tasks done", func() bool { return len(taskResults(root)) == 10 })
	if hw := highWater.Load(); hw > 2 {
		t.Fatalf("running high water = %d, want <= 2", hw)
	}
}

// TestQueuedTaskCancelledNeverRuns: the second half of the pool bound — a
// task cancelled while waiting for the pool semaphore never runs.
func TestQueuedTaskCancelledNeverRuns(t *testing.T) {
	t.Parallel()
	child := &probe{name: "child", pref: Size{W: 2, H: 1}}
	root := NewFlex(Vertical)
	root.Add(child)
	h := startApp(t, root, 4, 2, WithTaskPoolSize(1))

	block := make(chan struct{})
	defer close(block)
	holderRunning := make(chan struct{})
	owner := child.nodeID()
	h.app.Go(owner, func(context.Context) (any, error) {
		close(holderRunning)
		<-block // holds the single pool slot
		return "holder", nil
	})
	<-holderRunning

	var ran atomic.Bool
	h.app.Go(owner, func(context.Context) (any, error) {
		ran.Store(true)
		return "queued", nil
	})
	// Unmount the owner: the queued task's ctx dies while it waits.
	h.onLoop(func() { root.Remove(child) })
	waitFor(t, "queued task dead-lettered", func() bool { return h.app.DeadLetters() >= 1 })
	if ran.Load() {
		t.Fatal("queued task ran despite cancellation before acquiring the pool")
	}
}

// TestTaskProgressStreaming: a task streams TaskProgress
// via TaskInfo + Post; the final TaskResult still arrives.
func TestTaskProgressStreaming(t *testing.T) {
	t.Parallel()
	child := &probe{name: "child", pref: Size{W: 2, H: 1}}
	child.onInit = func(_ *probe, ctx *Context) {
		a := ctx.App()
		ctx.Go(func(tctx context.Context) (any, error) {
			owner, id, ok := TaskInfo(tctx)
			if !ok {
				t.Error("TaskInfo not present in task ctx")
				return nil, nil
			}
			for i := 0; i < 3; i++ {
				a.Post(TaskProgress{Owner: owner, ID: id, Value: i})
			}
			return "summary", nil
		})
	}
	root := NewFlex(Vertical)
	root.Add(child)
	startApp(t, root, 4, 2)

	waitFor(t, "progress + result", func() bool {
		return len(taskProgresses(child)) == 3 && len(taskResults(child)) == 1
	})
	for i, p := range taskProgresses(child) {
		if p.Value != i || p.Owner != child.nodeID() {
			t.Fatalf("progress[%d] = %+v, want ordered value %d addressed to owner", i, p, i)
		}
	}
	if r := taskResults(child)[0]; r.Value != "summary" {
		t.Fatalf("final result = %+v, want summary", r)
	}
}

// TestGoUnknownOwnerDeadLetters: Go with a never-mounted owner completes
// with a cancelled ctx and dead-letters without running.
func TestGoUnknownOwnerDeadLetters(t *testing.T) {
	t.Parallel()
	root := &probe{name: "root", pref: Size{W: 4, H: 2}}
	h := startApp(t, root, 4, 2)
	var ran atomic.Bool
	h.app.Go(NodeID(999999), func(context.Context) (any, error) {
		ran.Store(true)
		return nil, nil
	})
	waitFor(t, "dead-lettered unknown owner", func() bool { return h.app.DeadLetters() == 1 })
	if ran.Load() {
		t.Fatal("task with unmounted owner ran; its ctx must be pre-cancelled")
	}
}
