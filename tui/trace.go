package tui

import "fmt"

// Runtime tracing: an opt-in, structured record of the
// decisions the runtime makes on behalf of a program — focus moves,
// mounts, key routing, layer changes. Interactive bugs are timing bugs
// ("the modal opened but Enter did nothing"), and the state that
// explains them is runtime-owned and invisible from a component. A trace
// turns those into an evidence trail instead of a guessing game.
//
// Tracing is OFF unless WithTrace is set; the emit sites cost one nil
// check when it is off, so this is safe to leave in production builds.

// TraceKind classifies a trace record.
type TraceKind uint8

const (
	// TraceFocus reports a focus move: Node gains it, Prev loses it.
	TraceFocus TraceKind = iota
	// TraceFocusRepair reports focus re-homed after its node died or
	// became invisible.
	TraceFocusRepair
	// TraceMount / TraceUnmount report component tree lifecycle.
	TraceMount
	TraceUnmount
	// TraceKey reports a key event and the node that CONSUMED it (Node
	// is 0 when nothing did).
	TraceKey
	// TraceScope reports a focus trap (modal) opening or closing.
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
	Kind     TraceKind
	Node     NodeID
	Comp     string
	Prev     NodeID
	PrevComp string
	// Detail is kind-specific: the key for TraceKey, the reason for a
	// repair, "open"/"close" for a scope.
	Detail string
}

// TraceFunc receives trace events on the loop goroutine, in order. Keep
// it cheap and non-blocking: it runs inside the runtime's critical path.
type TraceFunc func(TraceEvent)

// WithTrace enables runtime tracing (see TraceFunc). Pass nil to disable.
func WithTrace(fn TraceFunc) AppOption {
	return func(c *appConfig) { c.trace = fn }
}

// tracing reports whether tracing is on (the guard at every emit site).
func (a *App) tracing() bool { return a.cfg.trace != nil }

// trace emits one record. Callers guard with tracing() when building the
// arguments costs anything.
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

// compName resolves a node's component type name ("" for none).
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

// describe renders a key for a trace line ("Ctrl-l", "Enter", "j").
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
