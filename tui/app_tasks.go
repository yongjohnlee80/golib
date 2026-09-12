package tui

import (
	"context"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/logger"
)

// Task Scheduling and Async Lifecycle Architecture
//
// The TUI task runtime executes background work (I/O, network requests,
// database queries, compute) off the main loop goroutine while guaranteeing:
//  1. Non-Blocking Loop: App.Go / Context.Go never blocks the caller; semaphore
//     acquisition occurs inside the spun-up goroutine.
//  2. Automatic Cancellation on Unmount: The task context derives from the
//     component node's lifecycle context; unmounting a component immediately
//     cancels all its running tasks.
//  3. Direct Addressed Delivery: On completion, TaskResult is posted directly
//     to the owning node's HandleEvent on the loop goroutine (no bubbling).
//  4. Dead-Letter Accounting: If the owner unmounts before completion, the
//     delivery is dropped cleanly, counted in DeadLetters(), and logged without
//     crashing or stalling the loop.
//  5. Panic Isolation: Panicking tasks are recovered, wrapped in ErrTaskPanic
//     via errs.Recovered, logged with full stack traces, and delivered to the
//     owner as an ordinary TaskResult error.
//  6. Preemptive Exclusivity: TaskOption Exclusive(group) cancels any previous
//     in-flight task in that group before launching the new one (e.g. search-as-you-type).
//
//  ┌────────────────────────────────────────────────────────────────────────┐
//  │ Component (on Loop Goroutine)                                          │
//  │   id := ctx.Go(taskFunc, tui.Exclusive("search"))                      │
//  └───────────────────────┬────────────────────────────────────────────────┘
//                          │ (derive context from node unmount + App run)
//                          │ (cancel any in-flight task in "search" group)
//                          ▼
//  ┌────────────────────────────────────────────────────────────────────────┐
//  │ Background Task Goroutine (OFF Loop Goroutine)                         │
//  │                                                                        │
//  │   1. Acquire Bounded Pool Semaphore (a.sem <- struct{}{})             │
//  │      - If ctx cancelled while waiting in queue, abort immediately!    │
//  │                                                                        │
//  │   2. Execute task(tctx) with recover() sandbox                         │
//  │      - Optional: emit tui.TaskProgress via TaskInfo(ctx) & app.Post    │
//  │                                                                        │
//  │   3. Release Pool Semaphore (<-a.sem)                                  │
//  └───────────────────────┬────────────────────────────────────────────────┘
//                          │
//                          ▼
//  ┌────────────────────────────────────────────────────────────────────────┐
//  │ Program Lane (Lane B) -> App.Post(TaskResult{Owner, ID, Value, Err})   │
//  └───────────────────────┬────────────────────────────────────────────────┘
//                          │
//                          ▼
//  ┌────────────────────────────────────────────────────────────────────────┐
//  │ App.dispatch() on Loop Goroutine                                       │
//  │                                                                        │
//  │   Is Owner Node still mounted?                                         │
//  │     ├── YES ──> OwnerComponent.HandleEvent(TaskResult)                 │
//  │     └── NO  ──> Dead-letter (deadLetters++, warning log, drop)         │
//  └────────────────────────────────────────────────────────────────────────┘

// Task is one unit of background work. It runs OFF the loop goroutine and
// must treat ctx as its lifetime: ctx derives from the owner's unmount
// context AND the App's run context — whichever dies first. Task closures
// receive NO Surface, *Context, or tree handle: the compiler steers
// off-loop code toward Post.
//
// Example:
//
//	func fetchUser(userID int) tui.Task {
//	    return func(ctx context.Context) (any, error) {
//	        req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("/user/%d", userID), nil)
//	        if err != nil {
//	            return nil, err
//	        }
//	        resp, err := http.DefaultClient.Do(req)
//	        if err != nil {
//	            return nil, err
//	        }
//	        defer resp.Body.Close()
//	        var u User
//	        err = json.NewDecoder(resp.Body).Decode(&u)
//	        return u, err
//	    }
//	}
type Task func(ctx context.Context) (any, error)

// taskConfig is the option-set state of one Go call.
type taskConfig struct {
	group string
}

// TaskOption configures one App.Go call.
type TaskOption func(*taskConfig)

// Exclusive assigns the task to a per-owner named group and CANCELS the
// contexts of all in-flight tasks in that (owner, group) before this one
// starts — lazygit's preemption semantics (pkg/tasks: Stop channel +
// monotonic staleness guard). Superseded tasks still emit
// their TaskResult (with ctx.Err()), which the monotonic ID check makes
// trivially ignorable.
//
// Example (search-as-you-type):
//
//	func (c *SearchComponent) HandleEvent(ev tui.Event) bool {
//	    switch e := ev.(type) {
//	    case tui.KeyEvent:
//	        if e.Kind == tui.KeyPress && e.Rune != 0 {
//	            c.query += string(e.Rune)
//	            // Any running search for the previous query is cancelled immediately.
//	            c.activeTask = c.ctx.Go(runSearch(c.query), tui.Exclusive("search-query"))
//	            return true
//	        }
//	    case tui.TaskResult:
//	        if e.ID == c.activeTask && e.Err == nil {
//	            c.results = e.Value.([]SearchResult)
//	            c.ctx.MarkDirty()
//	            return true
//	        }
//	    }
//	    return false
//	}
func Exclusive(group string) TaskOption {
	if group == "" {
		panic("tui: Exclusive: empty group name")
	}
	return func(tc *taskConfig) { tc.group = group }
}

// taskInfoKey keys the identity Go injects into every task context.
type taskInfoKey struct{}

// taskIdentity is the value TaskInfo extracts.
type taskIdentity struct {
	owner NodeID
	id    TaskID
}

// TaskInfo extracts the identity Go injected into the task's context — the
// handle a streaming task needs to address TaskProgress to itself.
//
// Example (streaming progress from background task):
//
//	func downloadFile(app *tui.App, url string) tui.Task {
//	    return func(ctx context.Context) (any, error) {
//	        owner, taskID, ok := tui.TaskInfo(ctx)
//	        if !ok {
//	            return nil, errors.New("missing task identity")
//	        }
//	        for step := 1; step <= 10; step++ {
//	            select {
//	            case <-ctx.Done():
//	                return nil, ctx.Err()
//	            case <-time.After(50 * time.Millisecond):
//	            }
//	            // Post incremental progress to owner node on Lane B
//	            app.Post(tui.TaskProgress{
//	                Owner: owner,
//	                ID:    taskID,
//	                Value: fmt.Sprintf("%d%% complete", step*10),
//	            })
//	        }
//	        return "done", nil
//	    }
//	}
func TaskInfo(ctx context.Context) (owner NodeID, id TaskID, ok bool) {
	ti, ok := ctx.Value(taskInfoKey{}).(taskIdentity)
	return ti.owner, ti.id, ok
}

// exKey identifies one per-owner exclusive group.
type exKey struct {
	owner NodeID
	group string
}

// exEntry is one in-flight member of an exclusive group.
type exEntry struct {
	id     TaskID
	cancel context.CancelFunc
}

// asyncState is the App's cross-goroutine task bookkeeping. It has its own
// lock because App.Go and task completion run off the loop goroutine;
// everything else on App is loop-owned.
type asyncState struct {
	mu        sync.Mutex
	ctxs      map[NodeID]context.Context // mounted node contexts, for task derivation
	exclusive map[exKey][]*exEntry

	wg          sync.WaitGroup
	inflight    atomic.Int64
	deadLetters atomic.Uint64
}

// canceledCtx is the base context for tasks whose owner is already
// unmounted at Go time: the task never runs and completes immediately with
// ctx.Err(); its result dead-letters at delivery.
var canceledCtx = func() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}()

// Go schedules task on the App's bounded pool and returns immediately.
//
// Concurrency and Scheduling Semantics:
//   - Non-Blocking: Never blocks the caller; bounded semaphore acquisition
//     happens inside the spawned goroutine so UI latency is never impacted.
//   - Lifetime Bound: The task context derives from the owner node's unmount
//     context AND the App's lifetime context. Unmounting the node or closing
//     the App immediately cancels the task.
//   - Direct Delivery: On completion, TaskResult{Owner, ID, Value, Err} is
//     posted onto Lane B and delivered directly to the owner's HandleEvent
//     on the main loop goroutine (bypassing bubbling).
//   - Dead-Letter Handling: If the owner has unmounted before delivery, the
//     event is dropped cleanly, counted in DeadLetters(), and logged at warning level.
//   - Thread-Safety: Safe to call from any goroutine, though UI components
//     typically call the convenient Context.Go method.
func (a *App) Go(owner NodeID, task Task, opts ...TaskOption) TaskID {
	if task == nil {
		panic(errs.Fatal{Op: "tui: App.Go", Rule: "nil task"})
	}
	var tc taskConfig
	for _, o := range opts {
		if o != nil {
			o(&tc)
		}
	}

	// Monotonic per App; with never-reused NodeIDs the (owner, id) pair is
	// globally unambiguous for the App's lifetime.
	id := TaskID(a.nextTaskID.Add(1))

	a.async.mu.Lock()
	base, mounted := a.async.ctxs[owner]
	if !mounted {
		base = canceledCtx
	}
	cctx, cancel := context.WithCancel(base)
	tctx := context.WithValue(cctx, taskInfoKey{}, taskIdentity{owner: owner, id: id})
	if tc.group != "" {
		k := exKey{owner: owner, group: tc.group}
		// Preempt every in-flight member of the (owner, group) before this one
		// starts.
		for _, e := range a.async.exclusive[k] {
			e.cancel()
		}
		a.async.exclusive[k] = append(a.async.exclusive[k], &exEntry{id: id, cancel: cancel})
	}
	a.async.inflight.Add(1)
	a.async.wg.Add(1)
	a.async.mu.Unlock()

	go a.runTask(tctx, cancel, owner, id, tc.group, task)
	return id
}

// runTask is the per-task goroutine: acquire the pool semaphore INSIDE the
// goroutine (the deliberate inversion of the ingestor's caller-blocking
// acquire, ingestor/writer.go:54-56 vs its acquire-or-fallback select at
// ingestor/writer.go:66-75 — a UI thread must never block, so Go bounds
// RUNNING tasks, not calls), run recover-protected (the scaffold's
// per-connection isolation, server/scaffold.go:205-212), and post the
// addressed TaskResult.
func (a *App) runTask(tctx context.Context, cancel context.CancelFunc, owner NodeID, id TaskID, group string, task Task) {
	defer a.async.wg.Done()
	defer a.async.inflight.Add(-1)
	defer cancel()
	defer a.removeExclusive(owner, group, id)

	select {
	case a.sem <- struct{}{}:
		// A preemption/unmount racing the acquire must still win: a task
		// cancelled while queued never runs.
		if err := tctx.Err(); err != nil {
			<-a.sem
			a.Post(TaskResult{Owner: owner, ID: id, Err: err})
			return
		}
	case <-tctx.Done():
		a.Post(TaskResult{Owner: owner, ID: id, Err: tctx.Err()})
		return
	}

	var value any
	var err error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				// One crashing task never kills the app; the owner finds
				// out through the same channel as any failure
				// (errors.Is(res.Err, tui.ErrTaskPanic)).
				// errs.Recovered keeps a panicked ERROR recoverable rather
				// than rendering it into text, which is the loss ErrTaskPanic
				// would otherwise hide by continuing to answer errors.Is.
				err = errs.Recovered(ErrTaskPanic, rec, "tui: task %d on node %d",
					uint64(id), uint64(owner))
				logger.Error(a.cfg.logger, err, map[string]any{
					"tui": "task panic", "owner": uint64(owner), "task": uint64(id),
					"stack": string(debug.Stack()),
				})
			}
		}()
		value, err = task(tctx)
	}()
	<-a.sem

	a.Post(TaskResult{Owner: owner, ID: id, Value: value, Err: err})
}

// removeExclusive drops a completed task from its exclusive group.
func (a *App) removeExclusive(owner NodeID, group string, id TaskID) {
	if group == "" {
		return
	}
	k := exKey{owner: owner, group: group}
	a.async.mu.Lock()
	defer a.async.mu.Unlock()
	entries := a.async.exclusive[k]
	for i, e := range entries {
		if e.id == id {
			entries = append(entries[:i], entries[i+1:]...)
			break
		}
	}
	if len(entries) == 0 {
		delete(a.async.exclusive, k)
		return
	}
	a.async.exclusive[k] = entries
}

// DeadLetters reports how many addressed TaskResult/TaskProgress deliveries
// were dropped because their owner had unmounted. Dead-lettering is silent by design
// (unmount races are normal, not errors); the count and WithLogger are its
// only observers.
func (a *App) DeadLetters() uint64 { return a.async.deadLetters.Load() }
