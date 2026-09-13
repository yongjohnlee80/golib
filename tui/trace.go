package tui

import "fmt"

// Runtime Tracing Pipeline & Diagnostic Architecture
//
// The TUI runtime provides a zero-allocation-when-idle, structured event
// tracing pipeline designed for diagnosing interactive timing and focus bugs.
//
// In terminal UIs, the most difficult bugs are temporal and invisible from
// within components:
//   - "The modal dialog opened, but pressing Enter activated the background list."
//   - "A click landed on pane B, but focus redirected back to pane A."
//   - "A child unmounted while a pointer press was in flight."
//
// Runtime tracing records these decisions chronologically into a lightweight,
// structured stream of TraceEvent records delivered synchronously on the main
// loop goroutine.
//
//               ┌────────────────────────────────────────────────────────┐
//               │                Runtime Decision Sites                  │
//               │                                                        │
//               │   - Key Routing:     trace(TraceKey, consumer, ...)    │
//               │   - Pointer Focus:   trace(TraceFocus, target, ...)    │
//               │   - Modal Scopes:    trace(TraceScope, scope, "open")  │
//               │   - Focus Repair:    trace(TraceFocusRepair, ring[0])  │
//               │   - Tree Lifecycle:  trace(TraceMount/TraceUnmount)    │
//               └──────────────────────────┬─────────────────────────────┘
//                                          │
//                                          ▼
//                                  App.tracing() guard
//                                          │
//                    ┌─────────────────────┴─────────────────────┐
//                    ▼                                           ▼
//           [WithTrace enabled]                         [WithTrace disabled]
//                    │                                           │
//     - Resolve component type names                      Fast single-pointer nil check.
//       (ev.Comp, ev.PrevComp)                            Zero string allocations.
//     - Synchronous invoke: TraceFunc(ev)                 Zero CPU overhead in production.
//                    │
//                    ▼
//     ┌────────────────────────────────────────────────────────┐
//     │ Trace Consumer (Developer Tools / Diagnostics / Tests) │
//     │   - Ring buffer recorder                               │
//     │   - Live diagnostic HUD / terminal overlay             │
//     │   - Automated test assertions against focus transitions│
//     └────────────────────────────────────────────────────────┘
//
// Performance and Safety Invariants:
//  1. Zero Overhead When Disabled: Tracing is strictly opt-in via WithTrace.
//     When disabled (the default), call sites perform a single nil-pointer check
//     before computing or allocating arguments.
//  2. Component Name Resolution: While the runtime internally addresses nodes by
//     their monotonic NodeID, TraceEvent automatically populates human-readable
//     Go type names (Comp and PrevComp) so developers read "*widget.Button"
//     rather than "node 42".
//  3. Loop-Goroutine Execution: TraceFunc executes synchronously on the main loop.
//     Handlers must be non-blocking and fast (e.g., appending to a circular slice
//     or writing to a non-blocking channel).
//
// Usage Examples:
//
// Example 1: Capturing focus and key transitions during automated testing:
//
//	var traces []tui.TraceEvent
//	app := tui.NewApp(root,
//	    tui.WithBackend(tb),
//	    tui.WithTrace(func(ev tui.TraceEvent) {
//	        traces = append(traces, ev)
//	    }),
//	)
//	tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 'j'})
//	// Assert that the key was consumed by the expected component:
//	last := traces[len(traces)-1]
//	if last.Kind == tui.TraceKey && last.Comp != "*widget.List" {
//	    t.Fatalf("expected *widget.List to consume 'j', got %s", last.Comp)
//	}
//
// Example 2: Streaming runtime trace to a diagnostic log or ring buffer:
//
//	app := tui.NewApp(root,
//	    tui.WithBackend(backend),
//	    tui.WithTrace(func(ev tui.TraceEvent) {
//	        slog.Debug("tui trace",
//	            "kind", ev.Kind.String(),
//	            "node", ev.Node,
//	            "comp", ev.Comp,
//	            "prev", ev.PrevComp,
//	            "detail", ev.Detail,
//	        )
//	    }),
//	)

// TraceKind classifies a trace record.
type TraceKind uint8

const (
	// TraceFocus reports a focus move: Node gains it, Prev loses it.
	TraceFocus TraceKind = iota

	// TraceFocusRepair reports focus re-homed after its node died or
	// became invisible.
	TraceFocusRepair

	// TraceMount reports that a new component node was added to the tree.
	TraceMount

	// TraceUnmount reports that a component subtree was unmounted and its
	// lifecycle context cancelled.
	TraceUnmount

	// TraceKey reports a key event and the node that CONSUMED it (Node
	// is 0 when nothing did and the key fell through to global keymaps).
	TraceKey

	// TraceScope reports a focus trap (modal layer or popup overlay)
	// opening or closing.
	TraceScope
)

// String renders the kind for log lines.
func (k TraceKind) String() string {
	switch k {
	case TraceFocus:
		return "focus"
	case TraceFocusRepair:
		return "focus-repair"
	case TraceMount:
		return "mount"
	case TraceUnmount:
		return "unmount"
	case TraceKey:
		return "key"
	case TraceScope:
		return "scope"
	}
	return "?"
}

// TraceEvent is one runtime observation. Comp/PrevComp carry the Go type
// names of the components involved, which is what a reader actually
// needs — a bare NodeID says nothing.
type TraceEvent struct {
	// Kind identifies the runtime transition category.
	Kind TraceKind

	// Node is the primary target node ID (e.g. node gaining focus, mounting,
	// or consuming a key).
	Node NodeID

	// Comp is the formatted Go type name of Node (e.g. "*widget.TextInput").
	// Automatically populated if omitted at the emit site.
	Comp string

	// Prev is the secondary or previous node ID (e.g. node losing focus).
	Prev NodeID

	// PrevComp is the formatted Go type name of Prev. Automatically populated
	// if omitted at the emit site.
	PrevComp string

	// Detail provides kind-specific contextual metadata:
	//   - TraceKey: the string representation of the key ("Ctrl-c", "Enter", "Tab").
	//   - TraceScope: "open" or "close".
	//   - TraceFocusRepair: human-readable reason for repair ("node unmounted", "node hidden").
	//   - Pointer press aborts: explanation why a click was skipped.
	Detail string
}

// TraceFunc receives trace events on the loop goroutine, in chronological order.
//
// Implementations MUST be non-blocking and fast: TraceFunc executes synchronously
// on the main loop's critical path.
type TraceFunc func(TraceEvent)

// WithTrace configures runtime event tracing on the application.
//
// Pass nil to disable tracing. When enabled, every focus shift, modal trap,
// key consumption, and tree unmount fires fn synchronously.
func WithTrace(fn TraceFunc) AppOption {
	return func(c *appConfig) { c.trace = fn }
}

// tracing reports whether runtime tracing is active.
//
// Every internal trace emit site calls this before preparing arguments to
// guarantee zero allocation and zero CPU overhead when tracing is disabled.
func (a *App) tracing() bool { return a.cfg.trace != nil }

// trace formats and delivers one TraceEvent to the registered TraceFunc.
//
// Automatically infers and populates human-readable Go component type names
// (ev.Comp and ev.PrevComp) from the live node registry if they were omitted
// at the emit site.
func (a *App) trace(ev TraceEvent) {
	if a.cfg.trace == nil {
		return
	}
	if ev.Comp == "" {
		ev.Comp = a.compName(ev.Node)
	}
	if ev.PrevComp == "" {
		ev.PrevComp = a.compName(ev.Prev)
	}
	a.cfg.trace(ev)
}

// compName returns the formatted Go type name ("*widget.Button") of the component
// mounted at id, or "<unmounted>" if the node is no longer present in the registry.
func (a *App) compName(id NodeID) string {
	if id == 0 {
		return ""
	}
	n := a.nodes[id]
	if n == nil {
		return "<unmounted>"
	}
	return fmt.Sprintf("%T", n.comp)
}

// describe renders a human-readable string representation of a KeyEvent
// for trace lines (e.g. "Ctrl-l", "Alt-Enter", "Shift-Tab", "Esc", "j").
func (k KeyEvent) describe() string {
	var b []byte
	if k.Mods&ModCtrl != 0 {
		b = append(b, "Ctrl-"...)
	}
	if k.Mods&ModAlt != 0 {
		b = append(b, "Alt-"...)
	}
	if k.Mods&ModShift != 0 {
		b = append(b, "Shift-"...)
	}
	switch k.Code {
	case KeyEnter:
		return string(b) + "Enter"
	case KeyEscape:
		return string(b) + "Esc"
	case KeyTab:
		return string(b) + "Tab"
	case KeyUp:
		return string(b) + "Up"
	case KeyDown:
		return string(b) + "Down"
	case KeyLeft:
		return string(b) + "Left"
	case KeyRight:
		return string(b) + "Right"
	}
	if k.Text != "" {
		return string(b) + k.Text
	}
	return string(b) + string(k.Code)
}
