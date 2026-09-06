package tui

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/tui/style"
)

// App is the master runtime: it owns the backend, the component tree, the
// two-lane event queue, the demand-scheduled timer heap, the Bus, and the
// bounded task pool.
//
// ONE GOROUTINE — the caller of Run — owns all component state. Nothing else
// may touch it. Every other goroutine reaches the tree by POSTING AN EVENT,
// which the loop applies in its own time. This is what makes components safe
// to write without locks: a component method can assume no other goroutine is
// inside the tree while it runs. Break it and the damage is a data race in
// user code that never wrote a goroutine.
// REFERENCE: tui/doc.go
type App struct {
	cfg     appConfig
	root    Component
	backend Backend

	ran    atomic.Bool
	quit   chan struct{} // closed when Run exits; stops the intake pump
	runCtx context.Context

	// TWO LANES, and they are separate so that neither can starve the other.
	//
	// Lane A carries input arriving from the backend — keys, mouse, resize.
	// The App pumps it, rather than the backend pushing into the loop, so a
	// slow frame cannot block the terminal read. The channel is unbuffered and
	// the pump holds the pending queue, which is why a burst of input is
	// counted as drops here instead of growing without bound.
	input      chan Event
	inputDrops atomic.Uint64

	// Lane B carries events the program itself posts. Keeping them off lane A
	// means a program that posts heavily cannot delay input, and input arriving
	// faster than it can be handled cannot delay the program.
	queue programQueue

	// --- Everything below is owned by the loop goroutine. Read or write it
	// from anywhere else and the single-owner guarantee above is gone. ---

	nodes      map[NodeID]*node
	byComp     map[Component]*node
	rootNode   *node
	nextNodeID uint64

	inLayout  bool
	inRender  bool
	layingOut *node // the node whose Layout is executing (LayoutChild legality)

	focused    NodeID // 0 = none
	scopeStack []scopeEntry

	size         Size
	buf          *buffer
	rctx         *renderContext
	renderDirty  bool
	layoutDirty  bool
	framePending bool // a frame deadline sits in the timer heap
	lastFrame    time.Time

	// Multi-click synthesis state. Owned by the event-loop
	// goroutine, like the rest of App's dispatch state.
	lastPressAt     time.Time
	lastPressX      int
	lastPressY      int
	lastPressButton MouseButton
	lastPressCount  int
	lastPressTarget NodeID
	frames          uint64

	timers      timerHeap
	timer       *time.Timer
	timerC      <-chan time.Time // nil when nothing is scheduled (idle — G5)
	timerArms   int              // instrumentation
	nextTimerID uint64

	bus *Bus

	// Task pool: sem bounds RUNNING tasks (the ingestor's
	// bounded-background-work pattern, ingestor/writer.go:28).
	sem        chan struct{}
	nextTaskID atomic.Uint64
	async      asyncState
}

// NewApp builds the master runtime around a root component. It panics on
// nil root or missing backend — misconfiguration fails at construction
// (golib convention; precedent server.NewScaffold,
// server/scaffold.go:87-90).
func NewApp(root Component, opts ...AppOption) *App {
	if root == nil {
		panic("tui: NewApp: nil root component")
	}
	cfg := defaultAppConfig()
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if cfg.backend == nil {
		panic("tui: NewApp: WithBackend is required — the core package cannot construct a terminal driver (ADR-0005 §2.1)")
	}
	if cfg.theme == nil {
		t := style.DefaultTheme()
		cfg.theme = &t
	}

	a := &App{
		cfg:     cfg,
		root:    root,
		backend: cfg.backend,
		quit:    make(chan struct{}),
		input:   make(chan Event),
		nodes:   make(map[NodeID]*node),
		byComp:  make(map[Component]*node),
		sem:     make(chan struct{}, cfg.taskPoolSize),
	}
	a.queue.init(cfg.eventQueueLimit, cfg.logger)
	a.bus = newBus(a)
	a.async.ctxs = make(map[NodeID]context.Context)
	a.async.exclusive = make(map[exKey][]*exEntry)
	return a
}

// Bus returns the App's broadcast bus.
func (a *App) Bus() *Bus { return a.bus }

// Post enqueues ev for delivery on the loop goroutine. Safe from any
// goroutine, including from inside handlers on the loop itself. Never
// blocks; program-lane events are never dropped. Panics only when the app
// explicitly opts into WithEventQueueLimit and the ceiling is exceeded. The
// default is unlimited, so an app that never sets a limit never panics here.
func (a *App) Post(ev Event) {
	if ev == nil {
		panic("tui: App.Post: nil event")
	}
	a.queue.push(programItem{ev: ev})
}

// Update enqueues fn to run on the loop goroutine. It ALWAYS enqueues and
// returns immediately — safe from any goroutine INCLUDING the loop itself,
// because lane B never blocks (the tview QueueUpdate self-deadlock is
// unrepresentable). Convention: code already running in a handler on the
// loop goroutine does NOT need Update — it owns the state and mutates
// directly; an fn enqueued from a handler runs in a later drain, before the
// next frame. It is never executed inline, and the App never inspects the
// caller's goroutine to decide: an fn always takes the same path, so its
// ordering relative to other posted work does not depend on who posted it.
func (a *App) Update(fn func()) {
	if fn == nil {
		panic("tui: App.Update: nil func")
	}
	a.queue.push(programItem{fn: fn})
}

// Run starts the backend synchronously (raw mode, alternate screen,
// capability probe; errors return before any goroutine starts,
// mirroring the scaffold's synchronous bind at server/scaffold.go:144-147),
// mounts root, runs the event loop ON THE CALLING GOROUTINE until ctx is
// cancelled or a terminal backend error occurs, then unmounts the tree,
// drains tasks, and stops the backend. The returned error is errors.Join of
// the loop error and teardown errors (never swallowed — the scaffold's
// rule, server/scaffold.go:157,166).
//
// PANIC CONTRACT: any panic on the loop goroutine (component handler,
// layout, render, or runtime bug) is recovered, the terminal is restored
// FIRST (backend.Stop always runs), and then the policy applies — default:
// repanic with the original value. A TUI that dies must never leave the
// terminal in raw mode with the alternate screen active.
func (a *App) Run(ctx context.Context) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !a.ran.CompareAndSwap(false, true) {
		return fmt.Errorf("tui: App.Run called more than once (%w)", errs.ErrPrecondition)
	}
	if err := a.backend.Start(ctx); err != nil { // synchronous acquisition
		return err
	}
	// Restore-before-repanic: this defer is registered first so it runs
	// LAST — after the quit close and run-context cancel below — and
	// backend.Stop ALWAYS executes before the panic propagates.
	defer func() {
		rec := recover()
		err = errors.Join(err, a.backend.Stop()) // restore ALWAYS
		if rec != nil {
			if a.cfg.panicPolicy == PanicRepanic {
				panic(rec) // after restore, original value
			}
			// A panicked ERROR is wrapped, not rendered. The convention
			// tells authors to panic with a value (errs.Fatal) so that a
			// caller can errors.As the fields back out; %v here would
			// flatten that value into text while ErrPanic kept answering
			// errors.Is, so the error would look healthy and the payload
			// would be gone. A non-error value has nothing to preserve and
			// still formats.
			if pe, ok := rec.(error); ok {
				err = errors.Join(err, fmt.Errorf("%w: %w", ErrPanic, pe))
			} else {
				err = errors.Join(err, fmt.Errorf("%w: %v", ErrPanic, rec))
			}
		}
	}()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()      // kills every node/task context even on the panic path
	defer close(a.quit) // stops the intake pump
	a.runCtx = runCtx

	sz, err := a.backend.Size()
	if err != nil {
		return err
	}
	a.size = sz
	a.buf = newBuffer(sz.W, sz.H)
	a.rctx = newRenderContext(a.cfg.theme, a.backend.Capabilities(), a.cfg.widthPolicy)

	a.mount(nil, a.root)
	a.layoutDirty, a.renderDirty = true, true
	a.maybeFrame() // first frame before any input

	// App-owned intake: pulls promptly from backend.Events(),
	// applies ALL lane-A policy, and closes a.input when Events() closes.
	go a.intake()

	return a.loop(ctx)
}

// widthPolicy reports the App's active grapheme width policy — the single
// source of the policy every Surface's resolution context carries
// (Surface.StringWidth reads rctx.policy, which is a copy of this). Fixed
// once per App (WithWidthPolicy). Reads rctx once Run has
// installed it; before that (Context methods can exist pre-Run) it falls
// back to the config value that will seed rctx — the same value.
func (a *App) widthPolicy() WidthPolicy {
	if a.rctx != nil {
		return a.rctx.policy
	}
	return a.cfg.widthPolicy
}

// loop is the scaffold's errc-vs-ctx.Done select, widened to four arms.
// REFERENCE: server/scaffold.go
func (a *App) loop(ctx context.Context) error {
	for {
		select {
		case ev, ok := <-a.input: // lane A: input, already through the pump
			if !ok { // backend Events() closed
				return errors.Join(a.backend.Err(), a.teardown())
			}
			a.dispatch(ev)

		case <-a.queue.wake: // lane B: the program posted work, or marked dirt
			a.drainProgramLane()
			a.maybeFrame()

		case <-a.timerC: // earliest deadline; nil, and so blocked, when idle
			a.fireDueTimers()

		case <-ctx.Done():
			return a.teardown()
		}
	}
}

// drainProgramLane processes one lane-B batch snapshot: queued closures,
// posted events, bus deliveries, task results.
func (a *App) drainProgramLane() {
	for _, it := range a.queue.drain() {
		if it.fn != nil {
			it.fn()
			continue
		}
		a.dispatch(it.ev)
	}
}

// teardown is the registry-drain shape (server/registry.go:155-198),
// terminal edition.
func (a *App) teardown() error {
	// T1 — unmount the tree, children first: every node context cancels,
	// which signals every in-flight task.
	if a.rootNode != nil {
		a.unmountTree(a.rootNode)
	}

	// T2 — wait for the task pool bounded by WithTaskDrainTimeout; tasks
	// still running at the deadline are abandoned (their goroutines keep
	// their cancelled ctx; results are dead-lettered) and counted —
	// exactly the registry's deadline-force-close report
	// (server/registry.go:184-194).
	done := make(chan struct{})
	go func() {
		a.async.wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(a.cfg.taskDrainTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		if n := a.async.inflight.Load(); n > 0 {
			return errs.Wrap(errs.ErrTimeout, "tui: task drain deadline: %d task(s) abandoned", n)
		}
	}
	// T3 — return; the deferred backend.Stop() restores the terminal.
	return nil
}

// maybeFrame renders when dirt exists, honoring the min-frame-interval CAP:
// dirty marks arriving faster than the interval coalesce into one frame per
// interval via a deadline in the timer heap — a cap, not a ticker: no dirt,
// no frame, no wakeup.
func (a *App) maybeFrame() {
	if !a.renderDirty && !a.layoutDirty {
		return
	}
	if a.framePending {
		return // a frame deadline is already scheduled
	}
	now := time.Now()
	if wait := a.cfg.minFrameInterval - now.Sub(a.lastFrame); wait > 0 {
		a.framePending = true
		a.scheduleFrame(now.Add(wait))
		return
	}
	a.renderFrame()
}

// renderFrame runs the frame pipeline against the cell buffer and its diff:
// (re)layout if dirty, repaint the visible tree into the cell buffer, apply
// the cursor rule, diff, and hand the backend ONE Flush (the one-write
// rule).
func (a *App) renderFrame() {
	if a.rootNode == nil {
		return
	}
	if a.buf.size() != a.size {
		// Never diff across a size change: resize invalidates the last
		// buffer entirely — cells no longer correspond to the same positions —
		// and forces a layout pass.
		a.buf.resize(a.size.W, a.size.H)
		a.layoutDirty = true
	}
	if a.layoutDirty {
		a.layoutTree()
		a.layoutDirty = false
		a.renderDirty = true // geometry changed; repaint
		a.repairInvisibleFocus()
	}
	if !a.renderDirty {
		return
	}

	// v1 repaints the whole visible tree; the cell diff keeps the flush
	// minimal — the copy of the last frame IS the dirty tracking, so nothing
	// else has to remember what changed.
	for i := range a.buf.curr {
		a.buf.curr[i] = blankCell
	}
	a.renderTree()
	a.renderDirty = false

	a.applyCursor()
	if err := a.backend.Flush(a.buf.diff()); err != nil {
		logger.Error(a.cfg.logger, err, map[string]any{"tui": "backend flush failed"})
	}
	a.lastFrame = time.Now()
	a.frames++
}

// repairInvisibleFocus enforces the no-invisible-focus rule: when a layout
// pass leaves the focused node without a current
// measure/place (a Split zoomed it away, a Tabs switch unhosted it, any
// future hider), focus is re-homed exactly like a dead focus — the
// existing unmount-time repair already picks the first focusable in the
// innermost surviving scope, or none. No component can keep receiving
// keys invisibly.
func (a *App) repairInvisibleFocus() {
	if n := a.nodes[a.focused]; n != nil && !n.visible() {
		a.repairFocus()
	}
}

// applyCursor implements the IME real-cursor rule: a
// focused CursorReporter parks the hardware cursor at the absolute
// translation of its reported position; anything else hides it. Cursor state
// is latched on the backend and emitted by the Flush that follows.
func (a *App) applyCursor() {
	if n := a.nodes[a.focused]; n != nil && n.visible() {
		if cr, ok := n.comp.(CursorReporter); ok {
			if x, y, ok := cr.Cursor(); ok {
				a.backend.SetCursor(n.absRect.X+x, n.absRect.Y+y)
				if cs, ok := n.comp.(CursorShaper); ok {
					a.backend.SetCursorShape(cs.CursorShape())
				} else {
					a.backend.SetCursorShape(CursorShapeDefault)
				}
				a.backend.ShowCursor()
				return
			}
		}
	}
	a.backend.HideCursor()
}
