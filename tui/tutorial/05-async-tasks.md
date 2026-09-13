# 5 — Async: Tasks, Fetching, and the Two-Lane Event Model

In `golib/tui`, **the loop goroutine owns every component and all UI state**. 
Layout, rendering, and event handlers execute strictly on this single thread, ensuring race-free, lock-free component code.

However, real-world terminal applications must perform I/O: fetching REST APIs, querying databases, executing shell commands, reading files, and listening to sockets.

> **THE GOLDEN RULE:**  
> **Never perform blocking I/O on the loop goroutine.**  
> If an event handler executes `http.Get(...)` or `time.Sleep(...)`, the entire event loop freezes. Key presses stop responding, window resizes stall, and frame rendering stops completely.

All background work must run asynchronously off the loop and funnel its results back onto **Lane B (the Program Lane)**.

---

## 1. The Two-Lane Event Architecture

Understanding *which lane* your work travels on is essential:

```
┌────────────────────────────────────────────────────────────────────────┐
│                              Outside World                             │
│     (Keyboard, Mouse, Terminal TTY)       (Background Workers, Tasks)  │
└───────────────────┬────────────────────────────────────┬───────────────┘
                    │ Raw Input                          │ Goroutine Work
                    ▼                                    ▼
        ┌──────────────────────┐             ┌──────────────────────┐
        │   Lane A: Input      │             │   Lane B: Program    │
        │   - backend.Events() │             │   - ctx.Go TaskResult│
        │   - Bounded queue    │             │   - ctx.Post events  │
        │   - Drops old motion │             │   - app.Update closures│
        │   - Latest-wins resize│            │   - Unbounded (default)│
        └───────────┬──────────┘             └───────────┬──────────┘
                    │                                    │
                    └───────────────┬────────────────────┘
                                    ▼
                     ┌──────────────────────────────┐
                     │    Single Event Loop (Run)   │
                     │    - Process Lane A Input    │
                     │    - Drain Lane B Program Q  │
                     │    - Render Coalesced Frame  │
                     └──────────────────────────────┘
```

- **Lane A (Input Lane):** Manages external terminal user input (keystrokes, mouse moves, terminal resizes). It is subject to drop-oldest overflow to keep the application responsive during heavy input floods.
- **Lane B (Program Lane):** Manages internal application signals: `ctx.Go` task completions, `ctx.Post` custom events, `Bus.Publish` notifications, and `app.Update` closures. By default, Lane B has an unlimited queue and events are never dropped. If an optional ceiling is configured via `WithEventQueueLimit(n)`, exceeding the limit triggers an immediate fail-loud panic (Lane B never silently drops events). Queue capacity is isolated from Lane A, though dispatch remains serialized on the single event loop goroutine, so draining a very large or slow program batch can delay processing from Lane A and indirectly fill its bounded intake queue.

---

## 2. Invoking Async Procedures with `ctx.Go`

The primary, sanctioned mechanism to perform background work is **`ctx.Go`**:

```go
func (c *Context) Go(task Task, opts ...TaskOption) TaskID
```

Where a `Task` is defined as:

```go
type Task func(ctx context.Context) (any, error)
```

### What `ctx.Go` guarantees:

1. **Off-Loop Execution:** Each task submission spawns a separate goroutine, while a concurrency semaphore limits concurrently executing tasks to 16 by default (configured via `WithTaskPoolSize`), preventing CPU starvation while keeping the UI responsive. Tasks run off-loop and must never mutate component state directly; return values are delivered back to the loop via `TaskResult`.
2. **Mount-Bound Cancellation:** The provided `context.Context` derives from `ctx.Ctx()` (the component's lifetime context). If the component unmounts while the request is in flight, **`ctx` is cancelled immediately**.
3. **Targeted Delivery (No Bubbling):** When the task finishes, the runtime packages the returned `(any, error)` into a `tui.TaskResult` event and pushes it onto **Lane B**. Unlike keyboard events, `TaskResult` is addressed **directly to the node that scheduled it**—it does not bubble or steal focus.
4. **Lifecycle Safety:** If the component node was removed from the tree before the task finishes, the runtime safely drops the result. It will never invoke `HandleEvent` on an unmounted node.

---

## 3. The Complete Pattern: Async HTTP Fetch Component

Below is a complete, working component demonstrating an asynchronous fetch:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// releasePayload is a typed result container for the fetch.
type releasePayload struct {
	version string
	body    string
}

type ReleaseViewer struct {
	ctx     *tui.Context
	loading bool
	err     error
	data    *releasePayload
}

func NewReleaseViewer() *ReleaseViewer {
	return &ReleaseViewer{}
}

// Init runs once when the component mounts onto the tree.
func (v *ReleaseViewer) Init(ctx *tui.Context) {
	v.ctx = ctx
	v.fetchRelease() // Trigger initial background load
}

// fetchRelease starts the async procedure off the loop goroutine.
func (v *ReleaseViewer) fetchRelease() {
	v.loading = true
	v.err = nil
	v.ctx.MarkDirty() // Request repaint to display "Loading..."

	// Schedule background task (execution bounded by the task concurrency semaphore):
	v.ctx.Go(func(tctx context.Context) (any, error) {
		req, err := http.NewRequestWithContext(tctx, "GET", "https://api.github.com/repos/golang/go/releases/latest", nil)
		if err != nil {
			return nil, err
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err // Returning an error wraps into TaskResult.Err
		}
		defer resp.Body.Close()

		raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if err != nil {
			return nil, err
		}

		// Return typed data to be matched in HandleEvent:
		return releasePayload{
			version: resp.Header.Get("X-Release-Version"),
			body:    string(raw),
		}, nil
	}, tui.Exclusive("fetch")) // Preempt any older in-flight request
}

// HandleEvent receives the TaskResult back on the loop goroutine!
func (v *ReleaseViewer) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.TaskResult:
		v.loading = false

		if e.Err != nil {
			// Check if task was aborted by component unmount or preemption:
			if e.Err == context.Canceled {
				return true
			}
			v.err = e.Err
			v.ctx.MarkDirty()
			return true
		}

		// Type-assert the payload:
		if payload, ok := e.Value.(releasePayload); ok {
			v.data = &payload
			v.ctx.MarkDirty()
			return true
		}

	case tui.KeyEvent:
		// Press 'r' to manually refresh data:
		if e.Kind == tui.KeyPress && e.Code == 'r' {
			v.fetchRelease()
			return true
		}
	}
	return false
}

func (v *ReleaseViewer) Layout(c tui.Constraints) tui.Size {
	return tui.Size{W: c.MaxW, H: c.MaxH}
}

func (v *ReleaseViewer) Render(s tui.Surface) {
	st := style.New()
	sz := s.Size()
	s.Fill(tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H}, " ", st)

	if v.loading {
		s.SetCell(2, 1, "Loading release details (fetching in background)...", st.Dim(true))
		return
	}

	if v.err != nil {
		errSt := st.Foreground(style.Red)
		s.SetCell(2, 1, fmt.Sprintf("Fetch failed: %v", v.err), errSt)
		s.SetCell(2, 2, "Press 'r' to retry", st.Dim(true))
		return
	}

	if v.data != nil {
		s.SetCell(2, 1, fmt.Sprintf("Latest Release: %s", v.data.version), st.Bold(true))
		s.SetCell(2, 3, "Press 'r' to refresh", st.Dim(true))
	}
}
```

---

## 4. Preempting Stale Work with `tui.Exclusive`

A notorious bug in asynchronous UIs is the **race on out-of-order responses**:
1. User types `"g"` into a search field $\rightarrow$ Query `"g"` fires.
2. User quickly types `"go"` $\rightarrow$ Query `"go"` fires.
3. Query `"go"` finishes in 30ms and populates the list.
4. Query `"g"` was slow, taking 300ms, and finishes *after* `"go"`.
5. The stale `"g"` results overwrite the newer `"go"` results!

### The Solution:
Pass **`tui.Exclusive(group string)`** when scheduling:

```go
v.ctx.Go(task, tui.Exclusive("search-query"))
```

When an exclusive task is scheduled:
- Any in-flight task on that node sharing the same group name is **immediately cancelled** (`tctx.Done()` fires).
- When the cancelled task exits, its `TaskResult` contains `context.Canceled`, which you can ignore.
- Only the most recent task's result is processed.

---

## 5. Streaming Long-Running Progress

For longer operations (e.g. database migrations, bulk downloads, or git clones), your task can stream progressive updates back to the UI before returning its final result.

Use **`tui.TaskInfo`** and **`ctx.Post`**:

```go
func (c *SyncComponent) startSync() {
	c.ctx.Go(func(tctx context.Context) (any, error) {
		owner, taskID, _ := tui.TaskInfo(tctx)

		for step := 1; step <= 10; step++ {
			select {
			case <-tctx.Done():
				return nil, tctx.Err()
			case <-time.After(500 * time.Millisecond):
			}

			// Post intermediate progress onto Lane B:
			c.ctx.Post(tui.TaskProgress{
				Owner:   owner,
				ID:      taskID,
				Stage:   fmt.Sprintf("Step %d of 10", step),
				Current: step,
				Total:   10,
			})
		}
		return "Sync complete!", nil
	})
}
```

In `HandleEvent`:
```go
func (c *SyncComponent) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.TaskProgress:
		c.stage = e.Stage
		c.progress = float64(e.Current) / float64(e.Total)
		c.ctx.MarkDirty()
		return true

	case tui.TaskResult:
		c.done = true
		c.ctx.MarkDirty()
		return true
	}
	return false
}
```

---

## 6. Periodic Polling with `ctx.Every`

If your UI monitors external data that changes over time, do not write a manual `for { sleep }` goroutine. Use **`ctx.Every`**:

```go
func (v *Dashboard) Init(ctx *tui.Context) {
	v.ctx = ctx
	v.refreshData()

	// Automatically posts a tui.TickEvent to this node every 5 seconds:
	ctx.Every(5 * time.Second)
}

func (v *Dashboard) HandleEvent(ev tui.Event) bool {
	switch ev.(type) {
	case tui.TickEvent:
		v.refreshData() // invokes ctx.Go(...)
		return true
	}
	return false
}
```

- **Unmount safety:** The timer automatically stops when the component unmounts (`OnUnmount`).
- **Fixed-delay semantics:** Timer intervals are fixed-delay, not fixed-rate: if the loop is delayed, ticks never backlog into a sudden burst of updates.

---

## 7. The BufferView Log Writer & Pre-Mount Relay

If you want background loggers or process outputs (like `exec.Cmd.Stdout`) to stream directly into a `widget.BufferView`, use `bufferView.Writer()`.

`bufferView.Writer()` is thread-safe, but has one important caveat:
> **Writes made before the BufferView is mounted reject with `(0, widget.ErrClosed)`.**

If callers ignore this error, startup logs are lost. To ensure startup logs are never lost before `app.Run` mounts the tree, use a small relay:

```go
type deferredWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
	dst io.Writer
}

func (d *deferredWriter) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dst != nil {
		return d.dst.Write(p)
	}
	return d.buf.Write(p) // Cache pre-mount log lines
}

func (d *deferredWriter) Attach(dst io.Writer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.buf.Len() > 0 {
		_, _ = dst.Write(d.buf.Bytes())
		d.buf.Reset()
	}
	d.dst = dst
}
```

Pass `deferredWriter` to your application logger at startup, then in your root component's `Init`:
```go
func (r *Root) Init(ctx *tui.Context) {
	r.logRelay.Attach(r.logBufferView.Writer())
}
```

---

## 8. Production Best Practices & Anti-Patterns

Writing asynchronous code in a single-owner UI architecture is simple once you internalize five battle-tested patterns:

### Pattern 1: Never Mutate Component State Inside the Task Closure

The single most common mistake for newcomers is modifying fields or calling `ctx` methods from within `ctx.Go`:

```go
// ❌ WRONG: Data race and loop invariant violation!
ctx.Go(func(tctx context.Context) (any, error) {
    items, err := client.FetchItems(tctx)
    v.items = items       // 💥 DATA RACE: Mutating struct from a worker goroutine!
    v.ctx.MarkDirty()     // 💥 DATA RACE: Calling loop-bound context methods off-loop!
    return nil, nil
})
```

```go
// ✅ CORRECT: The task is a pure, isolated reader/fetcher.
ctx.Go(func(tctx context.Context) (any, error) {
    return client.FetchItems(tctx) // Pure fetch, returns data
})

// All state mutation happens on the loop in HandleEvent:
func (v *View) HandleEvent(ev tui.Event) bool {
    if res, ok := ev.(tui.TaskResult); ok {
        v.items = res.Value.([]Item) // Safe: executed strictly on the loop goroutine
        v.ctx.MarkDirty()
        return true
    }
    return false
}
```

---

### Pattern 2: Discriminate Multiple Tasks with Typed Result Structs

If a component manages multiple distinct asynchronous actions (e.g. searching, loading user profile, saving a form), avoid generic types like `string` or `[]byte`. Wrap each result in a unique typed struct:

```go
type searchResult struct{ items []Item }
type profileResult struct{ user User }
type saveResult struct{ updated time.Time }

func (c *Dashboard) HandleEvent(ev tui.Event) bool {
    if res, ok := ev.(tui.TaskResult); ok {
        if res.Err != nil && !errors.Is(res.Err, context.Canceled) {
            c.err = res.Err
            c.ctx.MarkDirty()
            return true
        }

        // Clean, type-safe discrimination across multiple concurrent tasks:
        switch data := res.Value.(type) {
        case searchResult:
            c.searchItems = data.items
        case profileResult:
            c.profile = data.user
        case saveResult:
            c.lastSaved = data.updated
        }

        c.ctx.MarkDirty()
        return true
    }
    return false
}
```

---

### Pattern 3: Non-Overlapping Polling (Preventing Request Pile-Ups)

When pairing `ctx.Every` with `ctx.Go`, slow network responses or server stalls can cause ticks to arrive faster than requests complete. If unmanaged, dozens of duplicate worker requests pile up.

Use an `inFlight` boolean guard or `tui.Exclusive("poll")`:

```go
type Monitor struct {
    ctx      *tui.Context
    inFlight bool
    metrics  Metrics
}

func (m *Monitor) Init(ctx *tui.Context) {
    m.ctx = ctx
    m.poll()
    ctx.Every(2 * time.Second) // Tick every 2s
}

func (m *Monitor) poll() {
    if m.inFlight {
        return // Skip poll if previous request is still running
    }
    m.inFlight = true

    m.ctx.Go(func(tctx context.Context) (any, error) {
        return fetchMetrics(tctx)
    }, tui.Exclusive("poll"))
}

func (m *Monitor) HandleEvent(ev tui.Event) bool {
    switch e := ev.(type) {
    case tui.TickEvent:
        m.poll()
        return true

    case tui.TaskResult:
        m.inFlight = false // Reset in-flight guard
        if e.Err == nil {
            m.metrics = e.Value.(Metrics)
            m.ctx.MarkDirty()
        }
        return true
    }
    return false
}
```

---

### Pattern 4: Always Filter `context.Canceled`

When a user closes a modal, navigates away, or triggers preemption via `tui.Exclusive`, the task's context is cancelled. This is **normal expected control flow**, not an application failure:

```go
func (v *View) HandleEvent(ev tui.Event) bool {
    if res, ok := ev.(tui.TaskResult); ok {
        v.loading = false

        if res.Err != nil {
            // Silently ignore cancellations — user navigated away or preempted the query:
            if errors.Is(res.Err, context.Canceled) {
                return true
            }

            // Real network/server failure: display to user
            v.err = res.Err
            v.ctx.MarkDirty()
            return true
        }

        v.data = res.Value.(Data)
        v.ctx.MarkDirty()
        return true
    }
    return false
}
```

---

### Pattern 5: Panic Resilience with `tui.ErrTaskPanic`

If a background task panics (e.g. from an unexpected nil pointer in third-party library code), the `golib/tui` task runner catches the panic, wraps it into `tui.ErrTaskPanic` inside `TaskResult.Err`, and dispatches it back to the component. The terminal state is **not** restored because the application does not crash—the UI event loop continues running normally. (Full terminal restoration is reserved for unhandled loop-level panics or explicit application exit).

Always verify `res.Err == nil` and check that `res.Value != nil` before type-asserting `res.Value`. Note that a task returning `(nil, nil)` yields `res.Value == nil` with `res.Err == nil`:

```go
if res.Err != nil {
    if errors.Is(res.Err, tui.ErrTaskPanic) {
        logger.Error(nil, res.Err, map[string]any{"task": "crashed"})
    }
    v.err = res.Err
    v.ctx.MarkDirty()
    return true
}

// Check for nil and safely assert res.Value:
if payload, ok := res.Value.(Payload); ok {
    v.data = payload
}
v.ctx.MarkDirty()
return true
```

---

## 6. External Background Goroutines: Communicating via `App.Update`

Some background systems are long-running daemons that live independently of any single component's lifecycle:
- An incoming webhook HTTP server
- A persistent WebSocket connection
- A database change-data-capture (CDC) listener
- An OS signal listener

These external goroutines **must never touch component state or invoke widget methods directly**. They communicate with the UI through two sanctioned channels:

### Approach A: Posting Closures with `App.Update`

`App.Update(fn func())` accepts a closure and queues it onto Lane B:

```go
// In an external background goroutine (e.g. WebSocket listener):
go func() {
    for msg := range wsConn.Messages() {
        app.Update(func() {
            // This runs safely ON THE EVENT LOOP goroutine!
            model.AppendMessage(msg)
            appContext.MarkDirty()
        })
    }
}()
```

**Guarantees of `App.Update`:**
1. **Thread-Safe**: Safe to call concurrently from any number of external goroutines.
2. **Never Executed Inline**: It is always enqueued onto Lane B and drained by the event loop.
3. **Queue Capacity Isolation**: Uses Lane B's independent queue capacity, preventing external work from consuming Lane A intake slots. Note that because loop dispatch is serialized, draining large program batches can still delay Lane A handling and indirectly cause intake queue overflow.

### Approach B: Publishing to the `Bus`

If multiple components need to react to external messages, publish a typed event instead:

```go
go func() {
    for ev := range fileWatcher.Events() {
        bus.Publish(FileChangedEvent{Path: ev.Path})
    }
}()
```

Subscribers receive the event on the loop goroutine via `tui.SubscribeScoped`.

---

## Summary: When to Use Which Async Mechanism

| Mechanism | Purpose | Goroutine | Delivery |
| :--- | :--- | :--- | :--- |
| **`ctx.Go`** | Running background I/O (HTTP fetch, DB query, exec) | Per-submission goroutine (semaphore-limited execution) | Addressed `TaskResult` to component's `HandleEvent` |
| **`tui.Exclusive`** | Preempting obsolete in-flight requests (e.g. typing) | (Cancellation option for `ctx.Go`) | Cancels earlier task in same group |
| **`ctx.Post`** | External events or progress from outside goroutines | External / Task | Dispatched on Lane B into event routing |
| **`ctx.Every` / `After`** | Recurring timers and polling | Runtime Loop | Addressed `TickEvent` to component's `HandleEvent` |
| **`app.Update`** | Direct escape-hatch mutation closure | Any Goroutine | Executes closure directly on loop goroutine |

Next: [Floats and modals (Chapter 6)](06-floats-and-modals.md).

