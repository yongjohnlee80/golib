package tui

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/tui/i18n"
	"github.com/yongjohnlee80/golib/tui/style"
)

// App is the master runtime coordinator: it owns the terminal backend seam,
// the retained component tree, the two-lane event queue, the demand-scheduled
// timer min-heap, the typed broadcast bus, and the bounded background task pool.
//
// # Single-Goroutine Ownership Model (Normative)
//
// Exactly ONE goroutine — the caller of App.Run — owns all component state.
// No other goroutine may inspect or modify the component tree.
//
// Every external goroutine reaches the tree by ENQUEUING an event (App.Post,
// App.Update, Bus.Publish), which the event loop drains and applies on its own
// schedule. This invariant allows component authors to write plain Go code without
// mutexes: a component method can assume that no concurrent thread is mutating
// its struct fields.
//
// # The Two-Lane Event Funnel
//
// Input events and program events are isolated into two independent lanes:
//   - Lane A (Input): Receives hardware terminal events (keys, mouse, resize).
//     An App intake pump reads from Backend.Events() into an unbuffered channel
//     with bounded overflow protection, preventing slow frames from blocking terminal reads.
//   - Lane B (Program): Carries user-posted closures (App.Update), bus deliveries,
//     and completed task results. Lane B never drops events and provides queue isolation from Lane A.
type App struct {
	cfg     appConfig
	root    Component
	backend Backend

	// frameImages are the images visible reporters asked for in this frame's paint, and placed what
	// the backend was last told to show, by id (graphics.go)
	frameImages []ImagePlacement
	placed      map[uint32]ImagePlacement

	ran  atomic.Bool
	quit chan struct{} // closed when Run exits; stops the intake pump
	// stopRun ends Run, as cancelling its context does: the too-small screen's default Quit.
	stopRun context.CancelFunc

	// minSize is the smallest screen the App lays out for (WithMinimumSize, SetMinimumSize), and
	// quitButton the too-small screen's Quit, in screen cells, while it shows (toosmall.go).
	minSize    Size
	quitButton Rect

	// natives are this frame's native placements, and styledDepth how many subtree-scoped
	// placements the paint is inside; nativeStyler is the backend's, for this frame (native.go).
	natives      []NativePlacement
	styledDepth  int
	nativeStyler NativeStyler

	// hovered is the node under the pointer (0: none), and pointer the pointer's last screen
	// cell, for re-checking when a capture ends (hover.go).
	hovered NodeID
	pointer Point

	done   chan struct{} // closed once Run has returned, its teardown finished (Done)
	runCtx context.Context

	// TWO LANES, and they are separate to provide queue isolation between
	// terminal input and program updates.
	//
	// **Lane A** carries input arriving from the backend — keys, mouse, resize.
	// The App pumps it, rather than the backend pushing into the loop, so a
	// slow frame cannot block the terminal read. The channel is unbuffered and
	// the pump holds the pending queue, which is why a burst of input is
	// counted as drops here instead of growing without bound.
	input      chan Event
	inputDrops atomic.Uint64

	// **Lane B** carries events the program itself posts. Keeping Lane B separate
	// prevents either lane from consuming the other's queue capacity; dispatch
	// remains serialized, so a large or slow batch can delay the next selection
	// from the other lane.
	queue programQueue

	// --- Everything below is owned by the loop goroutine. Read or write it
	// from anywhere else and the single-owner guarantee above is gone. ---

	nodes      map[NodeID]*node
	byComp     map[Component]*node
	rootNode   *node
	nextNodeID uint64

	inLayout bool
	inRender bool

	// handlerNode is the node whose HandleEvent OR HandleAction is executing,
	// or 0 outside input delivery. Both phases mark it, because capture may be
	// taken from either and is granted to a specific node.
	//
	// It is an IDENTITY rather than a flag: with a bare "some handler is
	// running" boolean, node A's handler could call a retained Context
	// belonging to node B and take the pointer in B's name.
	handlerNode NodeID

	// actionHandlerNode is the node whose HandleAction is executing, or 0
	// outside an action delivery. SEPARATE from handlerNode on purpose:
	// handlerNode deliberately covers HandleEvent as well, because pointer
	// capture may be taken from either phase, and reusing it to gate action
	// forwarding let the RAW lane forward with provenance it does not own.
	//
	// Raw delivery clears it for its duration, so a raw handler running inside
	// an action delivery cannot borrow the invocation still open above it.
	actionHandlerNode NodeID

	// handlerOrigin and handlerSource are the provenance of the action
	// invocation currently being delivered, kept so Context.ForwardAction can
	// pass it to a child without the handler supplying it. Provenance a
	// consumer can state is provenance a consumer can forge, so the only way to
	// carry it is to read it back from the runtime that recorded it.
	//
	// Meaningful only while handlerNode names an action delivery; both are
	// saved and restored around every delivery so a nested one cannot leave the
	// outer invocation described by the wrong input.
	handlerOrigin ActionOrigin
	handlerSource Event

	// initNode is the node whose Init is running, or 0 outside mounting. Like
	// handlerNode it is an identity, because the setter it gates publishes a
	// specific component's own default bindings.
	initNode NodeID

	// updateDepth counts nested App.Update callbacks currently executing. The
	// phases in which a capture may be ENDED are stated positively — a handler
	// or an Update — because the forbidden list was incomplete twice over:
	// Init and any callback reached outside an Update both slipped through a
	// Layout/Render-only check.
	updateDepth int

	layingOut *node // the node whose Layout is executing (LayoutChild legality)

	focused    NodeID // 0 = none
	scopeStack []scopeEntry

	// batchDepth counts the nested Context.BatchTreeMutation calls currently
	// executing, and batchRepair records that something inside one asked for a
	// focus repair. Focus is a single global, so batching is APP-WIDE rather
	// than per-node: two compositions mutating in one batch still share the one
	// focused node, and a per-node depth would let each conclude it was the
	// outermost and repair against the other's half-applied tree.
	//
	// Only focus repair and focusability revalidation are deferred. Capture
	// loss, OnUnmount hooks and lifetime-context cancellation stay synchronous:
	// they are safety-critical, and deferring them would strand resources for
	// the length of the batch.
	batchDepth  int
	batchRepair bool

	// commits are the AfterLayout records registered by the layout pass in
	// progress, drained by the commit phase that follows it. See commit.go.
	commits []commitRecord

	// pendingRepair records that a focus repair ran while its only candidates
	// had not been laid out, and must be retried once the frame has run. See
	// repairFocus's empty-scope branch.
	pendingRepair bool

	// pendingFocusInto is a FocusInto whose target had not been laid out yet
	// — a pane shown in the same turn — to apply after the next layout; 0 for
	// none. A focus change before then cancels it. See FocusInto.
	pendingFocusInto NodeID

	// Pointer capture. captureOwner is 0 when nobody holds the pointer.
	// captureFocus is the focused node sampled at acquisition, which is what
	// "focus left the owner's subtree" is measured against — the owner itself
	// need not be focusable, so its own focus state cannot serve.
	captureOwner NodeID
	captureKind  CaptureKind
	captureFocus NodeID

	// Gesture recognition state. gestureState is what the recogniser is
	// handed and returns; gestureArmed is the runtime's own record of
	// whether it has called SetArmed(true) without a matching false, which
	// is what lets it guarantee the target ends up disarmed.
	gestureState GestureState
	gestureArmed bool

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

	// Task pool: sem bounds how many tasks run at once. Each submission still
	// starts a goroutine, which waits for a slot before its task runs.
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
		panic("tui: NewApp: WithBackend is required — the core package cannot construct a terminal driver")
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
		done:    make(chan struct{}),
		input:   make(chan Event),
		nodes:   make(map[NodeID]*node),
		byComp:  make(map[Component]*node),
		sem:     make(chan struct{}, cfg.taskPoolSize),
		minSize: cfg.minimumSize,
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
		panic(errs.Fatal{Op: "tui: App.Post", Rule: "nil event"})
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
		panic(errs.Fatal{Op: "tui: App.Update", Rule: "nil func"})
	}
	a.queue.push(programItem{fn: fn, isUpdate: true})
}

// SetTheme schedules an application-wide theme change on the loop goroutine.
// Every Surface resolves semantic style tokens through this one theme, so
// widgets need no theme reference and no notification callback of their own.
// The resolution generation is bumped to invalidate cached CellAttrs and the
// next frame repaints the whole tree without forcing a layout pass.
//
// Safe from any goroutine, including an event handler. Like Update, it always
// enqueues and returns; the change is visible after the program lane processes
// it. A nil theme is a configuration error and panics before anything is queued.
func (a *App) SetTheme(th *style.Theme) {
	if th == nil {
		panic(errs.Fatal{Op: "tui: App.SetTheme", Rule: "nil theme"})
	}
	a.Update(func() {
		a.cfg.theme = th
		if a.rctx != nil {
			a.rctx.theme = th
			a.rctx.themeGen++
		}
		a.renderDirty = true
	})
}

// SetLanguage schedules an application-wide language change on the loop goroutine: every
// widget showing a [Message] shows it in tag ("ko_KR", "es") from the next frame. Nothing is
// rebuilt and nothing republishes; the change marks layout dirty, and each widget looks its
// text up again as it is laid out, so labels that grow or shrink in the new language reflow
// in the same frame. Focus, scroll positions, typed text and open dialogs are untouched.
//
// Safe from any goroutine, including an event handler. Like Update, it always enqueues and
// returns. Code already on the loop goroutine that changes other state with it uses
// [App.ApplyLanguage], so that both show in the same frame.
func (a *App) SetLanguage(tag string) {
	a.Update(func() { a.ApplyLanguage(tag) })
}

// ApplyLanguage is [App.SetLanguage] taking effect at once: the frame being prepared lays out
// in tag. It is for code already on the loop goroutine — a handler, or inside Update — that
// changes other state in the same turn, such as a menu's check marks: an enqueued change can
// land a frame after state set directly, and that frame shows the two disagreeing. Loop
// goroutine only.
func (a *App) ApplyLanguage(tag string) {
	a.cfg.language = tag
	a.layoutDirty, a.renderDirty = true, true
}

// SetTranslations schedules replacing the catalogs every [Message] is looked up in, on the
// loop goroutine, and re-lays out the tree as [App.SetLanguage] does: how a reloaded catalog
// takes effect. A nil Set is a configuration error and panics before anything is queued.
//
// Safe from any goroutine; like Update it always enqueues and returns. On the loop goroutine,
// [App.ApplyTranslations] installs them in the same turn.
func (a *App) SetTranslations(s *i18n.Set) {
	if s == nil {
		panic(errs.Fatal{Op: "tui: App.SetTranslations", Rule: "the catalogs must not be nil"})
	}
	a.Update(func() { a.ApplyTranslations(s) })
}

// ApplyTranslations is [App.SetTranslations] taking effect at once, for code already on the
// loop goroutine that changes the tree in the same turn — a reload that installs a new layout
// and its catalogs together, so no frame shows one without the other. A nil Set panics.
// Loop goroutine only.
func (a *App) ApplyTranslations(s *i18n.Set) {
	if s == nil {
		panic(errs.Fatal{Op: "tui: App.ApplyTranslations", Rule: "the catalogs must not be nil"})
	}
	a.cfg.translations = s
	a.layoutDirty, a.renderDirty = true, true
}

// Language is the language widgets currently show a [Message] in. Call it on the loop
// goroutine, from a handler or inside Update.
func (a *App) Language() string { return a.cfg.language }

// Translate is m's text in the current language, exactly as a widget shows it (see
// [Context.Translate]): for text an application composes itself, such as a status line
// built around a file name. Text composed this way is fixed when it is built and does not
// follow a later [App.SetLanguage]; text that should is published as the [Message] itself.
// Call it on the loop goroutine, from a handler or inside Update.
func (a *App) Translate(m Message) string {
	s := a.cfg.translations
	text, ok := s.Lookup(a.cfg.language, m.ID)
	if !ok {
		return m.ID
	}
	return englishMnemonic(text, s, m.ID)
}

// SetRoot schedules replacing the root component on the loop goroutine: the
// old tree is unmounted — its node contexts cancelled, its tasks drained — and
// c is mounted in its place, laid out and painted on the next frame, with
// focus repaired as after any unmount. A declarative screen whose reload
// rebuilt its root is the case: the App outlives the tree it shows.
//
// Safe from any goroutine; like Update it always enqueues and returns. Before
// Run it replaces what Run will mount. A nil component panics before anything
// is queued.
func (a *App) SetRoot(c Component) {
	if c == nil {
		panic(errs.Fatal{Op: "tui: App.SetRoot", Rule: "nil component"})
	}
	a.Update(func() {
		if c == a.root && a.rootNode != nil {
			return
		}
		if a.rootNode != nil {
			a.unmountTree(a.rootNode)
		}
		a.root = c
		if a.runCtx != nil {
			a.mount(nil, c) // mounting marks layout, which repaints
		}
	})
}

// Done is closed once Run has returned, its teardown finished: what a
// component holding something beyond the tree's lifetime (a child process,
// say) waits on, since unmounting alone does not say the App is ending — a
// drawer that closes unmounts its content too. Any goroutine.
func (a *App) Done() <-chan struct{} { return a.done }

// Run starts the backend synchronously (raw mode, alternate screen,
// capability probe; errors return before the event loop and intake pump start,
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
	ctx, a.stopRun = context.WithCancel(ctx)
	defer a.stopRun()
	defer close(a.done)                          // registered first, so it runs last: after the teardown
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
			// errs.Recovered keeps a panicked ERROR recoverable instead of
			// rendering it into text. Written out by hand, the mistake is
			// invisible: ErrPanic keeps answering errors.Is, so the error
			// looks healthy while the payload is gone.
			err = errors.Join(err, errs.Recovered(ErrPanic, rec, "tui: App.Run"))
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

		case <-a.queue.wake: // lane B: the program posted work, or marked dirty
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
			if it.isUpdate {
				a.runUpdate(it.fn)
			} else {
				// Not an Update — a Bus delivery or other lane-B closure. It
				// runs on the loop goroutine but in no named phase, so the
				// phase-gated operations correctly refuse it.
				it.fn()
			}
			continue
		}
		a.dispatch(it.ev)
	}
}

// runUpdate executes one App.Update callback with the Update phase marked, so
// the operations legal "from an Update" can tell an actual Update callback from
// any other code that happens to be running on the loop goroutine.
//
// The depth is a COUNT, not a flag: an Update callback may run a nested one
// synchronously, and clearing outright on the inner return would leave the
// outer callback's remainder wrongly classified as no phase at all.
//
// A Bus delivery is not an Update merely because both travel lane B. Bus
// subscribers that need a phase-gated mutation must enqueue an Update.
func (a *App) runUpdate(fn func()) {
	a.updateDepth++
	defer func() { a.updateDepth-- }()
	fn()
}

// teardown is the registry-drain shape (server/registry.go:155-198),
// terminal edition.
func (a *App) teardown() error {
	// T0 — end any capture as a SHUTDOWN, before the unmount below would end
	// it as an unmount. Both are true, but the owner is being told why its
	// gesture died, and "the application is stopping" is the reason that
	// distinguishes an orderly exit from a widget being torn out of a live UI.
	a.loseCapture(CaptureLostShutdown)

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
	if a.tooSmall() {
		// Nothing is laid out: layoutDirty stays set, so the first frame at a large enough
		// size lays the tree out afresh.
		a.renderTooSmall()
		return
	}
	if a.layoutDirty {
		// Layout and commit alternate until geometry settles; see commit.go.
		// The frame must not paint a layout its own commit has invalidated.
		a.layoutAndCommit()
		a.renderDirty = true // geometry changed; repaint
		// Visibility is only knowable once the pass has placed everything, so
		// this is the earliest honest moment to end a capture whose owner is
		// no longer on screen.
		//
		// It runs BEFORE the focus repair below on purpose. An owner that went
		// invisible usually loses focus in the same pass, and both losses are
		// genuinely true; checking visibility first means the owner is told the
		// reason that actually explains its gesture ending rather than the
		// knock-on one.
		a.captureCheckVisible()
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
	a.beginNatives()
	a.renderTree()
	a.renderDirty = false

	a.applyNatives()
	a.applyImages()
	a.applyCursor()
	if err := a.backend.Flush(a.buf.diff()); err != nil {
		logger.Error(a.cfg.logger, err, map[string]any{"tui": "backend flush failed"})
	}
	a.lastFrame = time.Now()
	a.frames++
}

// repairInvisibleFocus enforces the no-invisible-focus invariant: when a layout
// pass leaves the currently focused node without a valid measure or placement
// (for instance, when a Split zooms and conceals a pane, or a Tabs switch unhosts
// a child page), focus is immediately re-homed.
//
// Repair selects the first focusable component in the innermost surviving focus scope,
// or clears focus if no candidate is available. This guarantees that no component
// can receive keyboard input invisibly without being displayed on screen.
func (a *App) repairInvisibleFocus() {
	if n := a.nodes[a.focused]; n != nil && !n.visible() {
		// A FocusInto waiting for this layout says where the focus goes: a pane shown in the turn
		// the focused one was hidden (two views swapped). It is answered first; the repair, which
		// would hand the focus to the first focusable and so overtake it, runs only if the focus
		// is still not visible.
		if id := a.pendingFocusInto; id != 0 {
			a.pendingFocusInto = 0
			if p := a.nodes[id]; p != nil {
				a.focusInto(p.comp, false)
			}
		}
		if m := a.nodes[a.focused]; m != nil && !m.visible() {
			a.repairFocus()
		}
	}
	a.retryDeferredFocusRepair()
}

// retryDeferredFocusRepair re-runs a repair that could not be satisfied before
// this layout.
//
// Focusability requires a measure and a placement, so a repair triggered by a
// mutation that MOUNTED new nodes runs against candidates that do not yet
// qualify — a dialog replacing its buttons repairs into an empty scope and
// leaves focus nowhere, even though the buttons appear on the very next frame.
// Running the repair again after layout is what distinguishes "this scope has no
// focusable" from "its focusables did not exist yet".
//
// It runs the same whole-scope revalidation as the immediate path, rather than a
// bare repair, because the deferral has two causes and only one of them leaves
// focus nowhere: a scope whose candidates were unborn, and a scope whose owner
// nominated a control that had not been laid out while focus remained valid
// elsewhere. A bare repair would fix the first and silently skip the second.
//
// Self-limiting: a scope that is genuinely empty, or a nominee that never
// becomes visible, produces no repaint and therefore no further frame to retry
// in. The flag is cleared before the retry, so one layout buys one attempt.
func (a *App) retryDeferredFocusRepair() {
	if id := a.pendingFocusInto; id != 0 {
		a.pendingFocusInto = 0 // one layout buys one attempt
		if n := a.nodes[id]; n != nil {
			a.focusInto(n.comp, false) // never defers again
		}
	}
	if !a.pendingRepair {
		return
	}
	a.pendingRepair = false
	a.revalidateFocus()
}

// applyCursor implements the real hardware cursor positioning invariant.
//
// If the focused node implements CursorReporter and reports an active insertion point,
// the runtime translates the local surface coordinates through the laid-out Rect
// chain to absolute screen coordinates and sets the hardware cursor position.
// If the component also implements CursorShaper, its desired cursor shape (block,
// underline, or bar) is latched onto the backend.
//
// If no focused node reports an active cursor, the hardware cursor is hidden.
// This hardware cursor anchoring is required so operating system IME composition
// windows anchor properly above or below the active text input cell.
// applyCursorColor gives the hardware cursor the focused component's colour (CursorColorer),
// resolved through the theme, or back its own, on a backend that can colour it.
func (a *App) applyCursorColor(comp Component) {
	setter, ok := a.backend.(CursorColorSetter)
	if !ok || a.rctx == nil {
		return
	}
	if cc, ok := comp.(CursorColorer); ok {
		if c, ok := cc.CursorColor(); ok {
			setter.SetCursorColor(a.rctx.resolve(style.New().Foreground(c)).FG, true)
			return
		}
	}
	setter.SetCursorColor(CellColor{}, false)
}

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
				a.applyCursorColor(n.comp)
				a.backend.ShowCursor()
				return
			}
		}
	}
	a.backend.HideCursor()
}
