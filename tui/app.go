package tui

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/tui/i18n"
	"github.com/yongjohnlee80/golib/tui/keymap"
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
	cfg     appConfig // as configured: NewApp's options, never written after NewApp
	live    appLive   // the settings that change at run time, starting from cfg's
	root    Component
	backend Backend
	caps    backendCaps // the backend's optional capabilities, asserted once at NewApp

	// THE APP'S PARTS, each a type in the file of its concern, its fields loop-owned unless the
	// type says otherwise.
	images  imagePlacements // the images shown (graphics.go)
	native  nativeState     // this frame's native placements (native.go)
	shape   shapeState      // this frame's pointer-shape regions (pointershape.go)
	faces   faceTable       // the font faces cells name (face.go)
	small   tooSmallState   // the smallest screen laid out for, and the too-small screen (toosmall.go)
	hover   hoverState      // the node under the pointer, and the tooltip (hover.go)
	phase   handlerPhase    // which phase runs, and whose handler (phase.go)
	focus   focusState      // the focused node, its scopes, batching and repair (focus.go)
	capture captureState    // pointer capture and the gesture recogniser's state (capture.go)
	presses pressCounter    // multi-click synthesis (app_routing.go)
	timing  timerState      // the timer heap (app_timers.go)
	clip    clipboardReads  // the clipboard reads waiting for an answer (clipboard.go)

	ran  atomic.Bool
	quit chan struct{} // closed when Run exits; stops the intake pump
	// stopRun ends Run, as cancelling its context does: the too-small screen's default Quit.
	stopRun context.CancelFunc

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

	// structural counts the tree changes in progress (mount, unmount); poisoned is that a panic
	// left one part made. A handler boundary contains no panic while either holds
	// (WithHandlerRecover): the tree may be half mounted.
	structural     int
	poisoned       bool
	stoppingCalled bool // loop-owned; retirement callbacks run at most once

	layingOut *node // the node whose Layout is executing (LayoutChild legality)

	// commits are the AfterLayout records registered by the layout pass in
	// progress, drained by the commit phase that follows it. See commit.go.
	commits []commitRecord

	size         Size
	buf          *buffer
	rctx         *renderContext
	renderDirty  bool
	layoutDirty  bool
	reveal       bool // a focused node waits to be brought into view by the Revealers around it (reveal.go)
	framePending bool // a frame deadline sits in the timer heap
	lastFrame    time.Time

	frames uint64

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
	}
	a.live = appLive{theme: cfg.theme, language: cfg.language, translations: cfg.translations, keymap: cfg.keymap, recognizer: cfg.recognizer}
	a.caps = capsOf(cfg.backend)
	a.small.minSize = cfg.minimumSize
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
		a.live.theme = th
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
	a.live.language = tag
	a.invalidate(true)
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
	a.SetTranslator(s)
}

// SetTranslator is [App.SetTranslations] for any [Translator]: catalogs that are not an
// [i18n.Set] (a remote catalog, a test's fake). Nil catalogs, a typed nil too, panic before
// anything is queued. Safe from any goroutine.
func (a *App) SetTranslator(t Translator) {
	if NilLike(t) {
		panic(errs.Fatal{Op: "tui: App.SetTranslator", Rule: "the catalogs must not be nil"})
	}
	a.Update(func() { a.ApplyTranslator(t) })
}

// ApplyTranslations is [App.SetTranslations] taking effect at once, for code already on the
// loop goroutine that changes the tree in the same turn — a reload that installs a new layout
// and its catalogs together, so no frame shows one without the other. A nil Set panics.
// Loop goroutine only.
func (a *App) ApplyTranslations(s *i18n.Set) {
	if s == nil {
		panic(errs.Fatal{Op: "tui: App.ApplyTranslations", Rule: "the catalogs must not be nil"})
	}
	a.ApplyTranslator(s)
}

// ApplyTranslator is [App.ApplyTranslations] for any [Translator]. Nil catalogs, a typed nil
// too, panic. Loop goroutine only.
func (a *App) ApplyTranslator(t Translator) {
	if NilLike(t) {
		panic(errs.Fatal{Op: "tui: App.ApplyTranslator", Rule: "the catalogs must not be nil"})
	}
	a.live.translations = t
	a.invalidate(true)
}

// SetKeymap schedules replacing the key bindings every widget matches keys against, on the loop
// goroutine: how an edited key-mapping scheme takes effect. A nil Map returns every command to its
// declared keys. Nothing is rebuilt; focus, typed text and open dialogs are untouched.
//
// Safe from any goroutine; like Update it always enqueues and returns. On the loop goroutine,
// [App.ApplyKeymap] installs it in the same turn.
func (a *App) SetKeymap(m *keymap.Map) {
	a.Update(func() { a.ApplyKeymap(m) })
}

// ApplyKeymap is [App.SetKeymap] taking effect at once, for code already on the loop goroutine
// that changes the tree in the same turn — a reload that installs a new layout and its key map
// together. A key's meaning changes, and so does the width of every accelerator and help line
// that prints one, so it dirties layout and render, as ApplyLanguage does. Loop goroutine only.
func (a *App) ApplyKeymap(m *keymap.Map) {
	a.live.keymap = m
	a.invalidate(true)
}

// Keymap is the key bindings in force; nil when none was given. Call it on the loop goroutine.
func (a *App) Keymap() *keymap.Map { return a.live.keymap }

// Language is the language widgets currently show a [Message] in. Call it on the loop
// goroutine, from a handler or inside Update.
func (a *App) Language() string { return a.live.language }

// Translate is m's text in the current language, exactly as a widget shows it (see
// [Context.Translate]): for text an application composes itself, such as a status line
// built around a file name. Text composed this way is fixed when it is built and does not
// follow a later [App.SetLanguage]; text that should is published as the [Message] itself.
// Call it on the loop goroutine, from a handler or inside Update.
func (a *App) Translate(m Message) string {
	s := a.live.translations
	text, ok := s.Lookup(a.live.language, m.ID)
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
	a.sendFaces() // a face table interned before the backend started is its from the first frame
	// Restore-before-repanic: this defer is registered first so it runs
	// LAST — after the quit close and run-context cancel below — and
	// backend.Stop ALWAYS executes before the panic propagates.
	defer func() {
		rec := recover()
		var stack []byte
		if rec != nil {
			stack = debug.Stack() // the panic's frames are still below this one
		}
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
			// PanicError carries the stack for a crash report, and unwraps to a panicked error.
			err = errors.Join(err, errs.Recovered(ErrPanic, &PanicError{Value: rec, Stack: stack}, "tui: App.Run"))
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
	a.rctx = newRenderContext(a.live.theme, a.backend.Capabilities(), a.cfg.widthPolicy)

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

		case <-a.timing.timerC: // earliest deadline; nil, and so blocked, when idle
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
	a.phase.updateDepth++
	defer func() { a.phase.updateDepth-- }()
	defer a.contain("update", nil, nil)
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

	// Retire pending work while its owners still exist, but only after capture
	// loss has kept its shutdown reason. The named phase permits synchronous
	// Context mutations; queued Updates are not drained after this point.
	if !a.stoppingCalled {
		a.stoppingCalled = true
		for _, fn := range a.cfg.stopping {
			a.runUpdate(fn)
		}
	}
	// Clipboard reads still waiting are answered now: queued Updates are not drained after this,
	// so a backend's late answer would never reach them.
	a.closeClipboardReads()

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

// invalidate marks the next frame dirty, laid out first when layout is set, and wakes the loop:
// a change made while dispatching input gets its frame, since that lane renders nothing by itself.
func (a *App) invalidate(layout bool) {
	if layout {
		a.layoutDirty = true
	}
	a.renderDirty = true
	a.queue.wakeUp()
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

// settleLayout lays the tree out and commits it, then settles what only a placed tree can answer:
// a capture whose owner left the screen, and a focus that did, or that a FocusInto waits to move.
func (a *App) settleLayout() {
	a.focus.settleBeforeInput = false // this layout answers it
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

// settleBeforeInput lays out now, ahead of the frame, when the next layout moves where input is
// about to be routed: a component's SettleBeforeInput (a popup closing in its commit), or, for input
// that goes to the focused node, a FocusInto waiting for its target's layout. The keys typed after
// the one that showed a field reach the field, and those after the one that closed a popup reach
// what it leaves focused, however the loop orders the frame against them. The frame still paints at
// its own pace.
func (a *App) settleBeforeInput(focused bool) {
	pending := a.focus.settleBeforeInput || focused && a.focus.pendingFocusInto != 0
	if !pending || !a.layoutDirty || a.rootNode == nil || a.buf.size() != a.size || a.tooSmall() {
		return
	}
	a.settleLayout()
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
		a.settleLayout()
	}
	if !a.renderDirty {
		return
	}

	// v1 repaints the whole visible tree; the cell diff keeps the flush
	// minimal — the copy of the last frame IS the dirty tracking, so nothing
	// else has to remember what changed.
	a.beginFrame()
	a.renderTree()
	a.renderToolTip()
	a.presentFrame(true)
}

// beginFrame starts a frame on a blank grid, with no native view or image reported yet.
func (a *App) beginFrame() {
	a.buf.clear()
	a.beginNatives()
	a.shape.regions = a.shape.regions[:0]
	a.images.frameImages = a.images.frameImages[:0]
}

// presentFrame ends a frame, whatever painted it, the one way: the native views and images the
// paint reported (none removes every one), the cursor by its rule or hidden, and the one flush.
func (a *App) presentFrame(cursor bool) {
	a.renderDirty = false
	a.applyNatives()
	a.applyShapes()
	a.applyImages()
	if cursor {
		a.applyCursor()
	} else {
		a.backend.HideCursor()
	}
	a.flush()
	a.lastFrame = time.Now()
	a.frames++
}

// flush hands the frame's diff to the backend: with the buffer's size to one that asks for it
// (SizedFlusher), else through Flush.
func (a *App) flush() {
	diff := a.buf.diff()
	var err error
	if f := a.caps.sized; f != nil {
		err = f.FlushSized(a.buf.size(), diff)
	} else {
		err = a.backend.Flush(diff)
	}
	if err != nil {
		logger.Error(a.cfg.logger, err, map[string]any{"tui": "backend flush failed"})
	}
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
	if n := a.nodes[a.focus.focused]; n != nil && !n.visible() {
		// A FocusInto waiting for this layout says where the focus goes: a pane shown in the turn
		// the focused one was hidden (two views swapped). It is answered first; the repair, which
		// would hand the focus to the first focusable and so overtake it, runs only if the focus
		// is still not visible.
		if id := a.focus.pendingFocusInto; id != 0 {
			a.focus.pendingFocusInto = 0
			if p := a.nodes[id]; p != nil {
				a.focusInto(p.comp, false)
			}
		}
		if m := a.nodes[a.focus.focused]; m != nil && !m.visible() {
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
	if id := a.focus.pendingFocusInto; id != 0 {
		a.focus.pendingFocusInto = 0 // one layout buys one attempt
		if n := a.nodes[id]; n != nil {
			a.focusInto(n.comp, false) // never defers again
		}
	}
	if !a.focus.pendingRepair {
		return
	}
	a.focus.pendingRepair = false
	a.revalidateFocus()
}

// applyCursorColor gives the hardware cursor the focused component's colour (CursorColorer),
// resolved through the theme, or back its own, on a backend that can colour it.
func (a *App) applyCursorColor(comp Component) {
	setter := a.caps.cursorColor
	if setter == nil || a.rctx == nil {
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
func (a *App) applyCursor() {
	if n := a.nodes[a.focus.focused]; n != nil && n.visible() {
		if cr, ok := n.comp.(CursorReporter); ok {
			// a cursor outside what the node can show (scrolled out of a ScrollView) is hidden, not
			// drawn over whatever is there
			if x, y, ok := cr.Cursor(); ok {
				if at := (Point{X: x, Y: y}).Translate(n.absRect.X, n.absRect.Y); n.visRect.Contains(at.X, at.Y) {
					a.backend.SetCursor(at.X, at.Y)
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
	}
	a.backend.HideCursor()
}
