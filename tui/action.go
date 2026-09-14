package tui

import "github.com/yongjohnlee80/golib/errs"

// TYPED SEMANTIC ACTIONS.
//
// A widget that reads raw events has to re-derive intent from every input
// device it wants to support, and ends up with one state machine per device:
// arrow keys and pointer motion both mean "move the selection", but a widget
// written against KeyEvent and MouseEvent says so twice and can disagree with
// itself. An action names the INTENT once — menu.move, resize.step — and the
// widget implements it once, whatever produced it.
//
// Resolvers turn events into actions. Provenance is NOT theirs to state: the
// runtime records where an action came from, so a resolver cannot claim a
// pointer-derived action arrived from the keyboard and slip past a widget's
// pointer policy.
//
// The vocabulary lives here in core because routing has to name it. Concrete
// actions belong to the widgets that define them, so core never has to import
// them; ActivateAction is the deliberate exception, because the runtime itself
// produces it and cannot import the widget package to do so.

// ActionID is an action's stable published name, such as "menu.move". It is
// part of the public contract: consumers match on it, so a type may not change
// the string it returns once released.
type ActionID string

// Action is a named intent. Implementations are ordinary values carrying
// whatever payload the intent needs, and never carry input origin — provenance
// belongs to the invocation the runtime builds.
type Action interface {
	ActionID() ActionID
}

// ActionOrigin says what produced an action, so a widget can police input by
// source — accepting a keyboard resize while refusing a pointer one, say.
type ActionOrigin uint8

const (
	// OriginProgrammatic: the program called DoAction. There is no source event.
	OriginProgrammatic ActionOrigin = iota
	// OriginKey: resolved from keyboard input.
	OriginKey
	// OriginPointer: resolved from pointer input.
	OriginPointer
	// OriginUser: resolved from a consumer-defined UserEvent.
	OriginUser
)

// String names the origin for traces and test failures.
func (o ActionOrigin) String() string {
	switch o {
	case OriginProgrammatic:
		return "programmatic"
	case OriginKey:
		return "key"
	case OriginPointer:
		return "pointer"
	case OriginUser:
		return "user"
	}
	return "unknown"
}

// ActionInvocation is what a handler receives: the action together with the
// provenance the runtime observed.
//
// The runtime constructs it. A resolver returns only an Action, so no resolver
// — consumer-supplied ones included — can forge an Origin it did not have.
type ActionInvocation struct {
	// Action is the intent being dispatched.
	Action Action
	// Origin is where it came from, as the runtime observed it.
	Origin ActionOrigin
	// Source is the event that produced it, and is nil for OriginProgrammatic.
	Source Event
}

// ActionResolver turns an event into an action. Returning false means "not
// mine", and the runtime tries the next resolver in the chain.
//
// A resolver returns an ACTION ONLY, never an invocation: provenance is the
// runtime's to record.
type ActionResolver interface {
	Resolve(ev Event) (Action, bool)
}

// ActionResolverFunc adapts a plain function to ActionResolver, for the common
// case where a resolver needs no state.
type ActionResolverFunc func(ev Event) (Action, bool)

// Resolve calls f.
func (f ActionResolverFunc) Resolve(ev Event) (Action, bool) { return f(ev) }

// ActionHandler is the optional receiving seam for actions, probed by type
// assertion. A component that does not implement it receives no actions and
// behaves exactly as it did before actions existed, so nothing is added to
// Component and no existing component changes shape.
type ActionHandler interface {
	Component
	HandleAction(inv ActionInvocation) bool
}

// Activatable is the optional capability for "this control can be triggered",
// which is what a button, a menu item or a checkbox all share regardless of
// whether the trigger came from Enter, a click, or the program.
type Activatable interface {
	Component
	Activate(origin ActionOrigin) bool
}

// ActivateAction is the one concrete action core owns, because the runtime
// itself produces it and cannot import the widget package.
//
// It is EMPTY. Who triggered the activation is carried by the invocation's
// Origin, which the runtime sets, so no producer can assert a source it did
// not have.
type ActivateAction struct{}

// ActionID returns the stable published name of this action.
func (ActivateAction) ActionID() ActionID { return "control.activate" }

// UserEvent is the sanctioned carrier for consumer-defined input. Event is
// sealed by an unexported method so the runtime's type switches stay
// exhaustive; this type satisfies it from inside the package, giving consumers
// a way in that does not require opening the set.
type UserEvent struct {
	// Tag names the kind of user input, so a resolver can match on it.
	Tag string
	// Payload carries whatever the consumer needs; the runtime never reads it.
	Payload any
}

func (UserEvent) isEvent() {}

// resolverSet is one node's two-layer resolver chain.
//
// TWO layers, not one list, because a single setter cannot restore a default it
// has already overwritten: a consumer that adds one resolver would otherwise
// have to re-supply the widget's own, and would silently lose them on any
// upgrade that added another. Consumer entries are tried first, then defaults.
type resolverSet struct {
	consumer []ActionResolver
	defaults []ActionResolver
}

// chain returns the resolution order: consumer first, then defaults.
func (rs *resolverSet) chain() []ActionResolver {
	if len(rs.consumer) == 0 {
		return rs.defaults
	}
	if len(rs.defaults) == 0 {
		return rs.consumer
	}
	out := make([]ActionResolver, 0, len(rs.consumer)+len(rs.defaults))
	out = append(out, rs.consumer...)
	return append(out, rs.defaults...)
}

// checkResolvers rejects nil entries at the setter rather than at dispatch. A
// nil resolver reaching the chain would panic in the middle of routing an
// event, naming neither the component that supplied it nor the call that did.
func checkResolvers(op string, r []ActionResolver) {
	for i, x := range r {
		if x == nil {
			panic(errs.Fatal{
				Op:     "tui: Context." + op,
				Rule:   "nil resolver",
				Detail: "entry " + itoa(i),
			})
		}
	}
}

// SetDefaultActionResolvers replaces this component's DEFAULT resolver layer —
// the widget's own bindings, the ones a consumer's entries are tried ahead of.
//
// Legal only while this component's own Init is running, and a panic otherwise.
// The layer is a widget's published behaviour, so it is fixed once the
// component is live; a phase rule states that plainly, where merely making the
// setter hard to reach would only make the same mutation obscure.
func (c *Context) SetDefaultActionResolvers(r ...ActionResolver) {
	a := c.app
	if a.initNode != c.node.id {
		panic(errs.Fatal{
			Op:   "tui: Context.SetDefaultActionResolvers",
			Rule: "legal only while this component's own Init is running",
		})
	}
	checkResolvers("SetDefaultActionResolvers", r)
	c.node.resolvers.defaults = append([]ActionResolver(nil), r...)
}

// SetActionResolvers replaces the CONSUMER resolver layer, which is tried
// before the component's defaults. Calling it with no arguments clears that
// layer and leaves the defaults intact.
//
// Loop-goroutine only, like every other Context method.
func (c *Context) SetActionResolvers(r ...ActionResolver) {
	checkResolvers("SetActionResolvers", r)
	c.node.resolvers.consumer = append([]ActionResolver(nil), r...)
}

// ActionResolvers returns the resolution order — consumer entries first, then
// defaults — as a fresh slice, so a caller inspecting the chain cannot mutate
// the node's own layers by writing through it.
func (c *Context) ActionResolvers() []ActionResolver {
	return append([]ActionResolver(nil), c.node.resolvers.chain()...)
}

// DoAction dispatches a to this node directly, skipping resolution entirely,
// and reports whether it was handled. Origin is OriginProgrammatic and Source
// is nil, because no input produced it.
//
// It does not bubble. The caller named the node it meant.
func (c *Context) DoAction(a Action) bool {
	if a == nil {
		panic(errs.Fatal{Op: "tui: Context.DoAction", Rule: "nil action"})
	}
	return c.app.dispatchAction(c.node, ActionInvocation{
		Action: a,
		Origin: OriginProgrammatic,
	})
}

// dispatchAction is the ONE activation rule, used by every producer: key
// resolvers, pointer resolvers, UserEvents, DoAction, and later the gesture
// recogniser.
//
// The Activatable fallback exists because activation must not depend on which
// producer asked for it. An earlier design reached Activate only from a
// recogniser, so a key binding that resolved to ActivateAction fell through to
// raw event handling and activated nothing — the same intent worked by click
// and silently did nothing by keyboard.
func (a *App) dispatchAction(n *node, inv ActionInvocation) bool {
	if n == nil || !n.mounted {
		return false
	}
	if h, ok := n.comp.(ActionHandler); ok {
		if a.deliverAction(n, h, inv) {
			a.trace(TraceEvent{Kind: TraceAction, Node: n.id,
				Detail: "handled: " + string(inv.Action.ActionID()) +
					" (" + inv.Origin.String() + ")"})
			return true
		}
	}
	if _, isActivate := inv.Action.(ActivateAction); isActivate {
		if act, ok := n.comp.(Activatable); ok && act.Activate(inv.Origin) {
			a.trace(TraceEvent{Kind: TraceAction, Node: n.id,
				Detail: "activated (" + inv.Origin.String() + ")"})
			return true
		}
	}
	return false
}

// deliverAction is the ONE place HandleAction is entered, mirroring deliverTo
// for events so that "this node's action handler is running" is exactly the set
// of moments a component can observe.
//
// It marks the same handlerNode as event delivery. Capture acquisition is legal
// from both phases and is granted to a specific node, so both must record WHICH
// node — a shared marker that only said "some handler" would let one component
// take the pointer in another's name from inside an action handler, which is
// the hole already closed on the event path.
func (a *App) deliverAction(n *node, h ActionHandler, inv ActionInvocation) bool {
	prev := a.handlerNode
	a.handlerNode = n.id
	defer func() { a.handlerNode = prev }()
	return h.HandleAction(inv)
}

// resolveFor runs n's resolver chain against ev and returns the first match.
// First match wins: once a resolver claims the event, later ones are not tried,
// even if the action it produced goes unhandled.
func (a *App) resolveFor(n *node, ev Event) (Action, bool) {
	for _, r := range n.resolvers.chain() {
		if act, ok := r.Resolve(ev); ok {
			if act == nil { // a resolver claiming a match must produce an action
				panic(errs.Fatal{
					Op:   "tui: ActionResolver.Resolve",
					Rule: "returned (nil, true); a resolver that matches must return an action",
				})
			}
			return act, true
		}
	}
	return nil, false
}

// originFor maps a source event to the provenance the runtime will record.
// This is the only place origin is decided, which is what makes it unforgeable
// by a resolver.
func originFor(ev Event) ActionOrigin {
	switch ev.(type) {
	case KeyEvent, PasteEvent:
		return OriginKey
	case MouseEvent:
		return OriginPointer
	case UserEvent:
		return OriginUser
	}
	return OriginProgrammatic
}

// itoa renders a small non-negative int without pulling strconv into this
// file's import set for one panic detail.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
