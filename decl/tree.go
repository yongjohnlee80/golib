package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/parse"
)

// DefaultMaxEmitDepth is the emission nesting cap a [Tree] uses when none is
// configured. It is defence against a long chain of handlers each legitimately
// emitting the next; a cycle is caught by identity long before this matters.
const DefaultMaxEmitDepth = 32

// phase records what the engine is in the middle of, so it can refuse work that
// would change the tree underneath an operation already walking it.
type phase uint8

const (
	phaseIdle phase = iota
	phaseMounting
	phaseEmitting
)

func (p phase) String() string {
	switch p {
	case phaseMounting:
		return "mounting"
	case phaseEmitting:
		return "emitting a signal"
	default:
		return "idle"
	}
}

// Tree is one instantiated schema.
//
// It holds identity, structure and handler bindings. It holds no values: the
// adapter's widgets are the only copy of a property, which is what keeps the
// engine from drifting out of step with what is on screen.
type Tree struct {
	adapter  Adapter
	maxDepth int

	nextID NodeID
	nodes  map[NodeID]*node
	// mounted is mount order. Teardown walks it backwards so a child is always
	// destroyed before its parent.
	mounted []NodeID
	root    NodeID

	ph phase
	// active is the stack of signals currently running, innermost last. It is
	// the cycle detector: a pair already on the stack cannot be entered again.
	active []signalKey
}

type node struct {
	id       NodeID
	typeName string
	// schemaID is the `id:` written in the schema, empty when absent. The
	// engine does not use it; it is kept so a consumer that reloads can match
	// an old node to a new one by what the author wrote.
	schemaID string
	pos      parse.Position
	parent   NodeID
	children []NodeID
	// handlers are per signal, in the order the schema declared them, which is
	// the order they run.
	handlers map[string][]boundHandler
}

type boundHandler struct {
	name string
	pos  parse.Position
	fn   func() error
}

type signalKey struct {
	node   NodeID
	signal string
}

// Option configures a [Tree].
type Option func(*Tree)

// WithMaxEmitDepth overrides [DefaultMaxEmitDepth].
func WithMaxEmitDepth(n int) Option {
	return func(t *Tree) {
		if n > 0 {
			t.maxDepth = n
		}
	}
}

// New returns a Tree that will drive a, which must not be nil.
//
// Construction validates rather than deferring the complaint to first use: a
// nil adapter is a programming mistake, not input, and the panic names it at
// the call site that made it.
func New(a Adapter, opts ...Option) *Tree {
	if a == nil {
		panic("decl.New: adapter is nil")
	}
	t := &Tree{
		adapter:  a,
		maxDepth: DefaultMaxEmitDepth,
		nodes:    make(map[NodeID]*node),
	}
	for _, o := range opts {
		o(t)
	}
	return t
}

// Root reports the root node, or NoNode before a successful Mount.
func (t *Tree) Root() NodeID { return t.root }

// Len reports how many nodes are mounted.
func (t *Tree) Len() int { return len(t.nodes) }

// TypeOf reports the schema type name a node was created from.
func (t *Tree) TypeOf(id NodeID) (string, bool) {
	n, ok := t.nodes[id]
	if !ok {
		return "", false
	}
	return n.typeName, true
}

// SchemaID reports the `id:` a node declared, and false when it declared none.
//
// The engine never consults this. It is recorded because the author wrote it,
// and a consumer matching a reloaded schema against a live tree needs the
// author's own name for a node rather than one the engine invented.
func (t *Tree) SchemaID(id NodeID) (string, bool) {
	n, ok := t.nodes[id]
	if !ok || n.schemaID == "" {
		return "", false
	}
	return n.schemaID, true
}

// Children reports a node's children in attach order.
func (t *Tree) Children(id NodeID) []NodeID {
	n, ok := t.nodes[id]
	if !ok {
		return nil
	}
	out := make([]NodeID, len(n.children))
	copy(out, n.children)
	return out
}

// Mount instantiates spec, from the root down.
//
// Order is part of the contract, not an implementation detail. For each node
// the engine creates it, applies its properties in DOCUMENT ORDER, binds its
// handlers in document order, then mounts its children left to right and
// attaches each. A consumer can therefore read the order of effects straight
// off the schema file.
//
// A failure part-way leaves the nodes already created IN PLACE and returns the
// error. There is no rollback, and inventing one would be worse than saying so:
// the adapter has already run real constructors and real setters, and the
// engine cannot know which of those are safely undoable. Call [Tree.Destroy] to
// tear down what exists.
func (t *Tree) Mount(spec parse.SpecTree) error {
	if t.ph != phaseIdle {
		return SchemaError{Op: "mount", Err: fmt.Errorf("%w: %s", ErrPhase, t.ph)}
	}
	if spec.Root == nil {
		return SchemaError{Op: "mount", Err: fmt.Errorf("%w: the schema has no root node", ErrPhase)}
	}
	if t.root != NoNode {
		return SchemaError{Op: "mount", Err: fmt.Errorf("%w: this tree is already mounted", ErrPhase)}
	}

	t.ph = phaseMounting
	defer func() { t.ph = phaseIdle }()

	id, err := t.mountNode(spec.Root, NoNode)
	if err != nil {
		return err
	}
	t.root = id
	return nil
}

func (t *Tree) mountNode(sn *parse.SpecNode, parent NodeID) (NodeID, error) {
	t.nextID++
	id := t.nextID

	n := &node{
		id:       id,
		typeName: sn.Type,
		schemaID: sn.ID,
		pos:      sn.Pos,
		parent:   parent,
		handlers: make(map[string][]boundHandler),
	}
	t.nodes[id] = n
	t.mounted = append(t.mounted, id)

	if err := t.adapter.Create(id, sn.Type, sn.Pos); err != nil {
		return id, SchemaError{Op: "create", Node: id, Detail: sn.Type, Pos: sn.Pos,
			Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
	}

	for _, p := range sn.Props {
		app := Application{Node: id, Prop: p.Name, Value: p.Value, Origin: FromSchema}
		if err := t.adapter.Apply(app); err != nil {
			return id, SchemaError{Op: "apply", Node: id, Detail: p.Name, Pos: p.Value.Pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
	}

	for _, h := range sn.Handlers {
		fn, err := t.adapter.ResolveHandler(id, h.Signal, h.Name, h.Pos)
		if err != nil {
			return id, SchemaError{Op: "bind", Node: id, Detail: h.Signal + " -> " + h.Name,
				Pos: h.Pos, Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
		if fn == nil {
			return id, SchemaError{Op: "bind", Node: id, Detail: h.Signal + " -> " + h.Name,
				Pos: h.Pos, Err: fmt.Errorf("%w: resolved to a nil function", ErrAdapter)}
		}
		n.handlers[h.Signal] = append(n.handlers[h.Signal], boundHandler{name: h.Name, pos: h.Pos, fn: fn})
	}

	for _, child := range sn.Children {
		cid, err := t.mountNode(child, id)
		if err != nil {
			return id, err
		}
		n.children = append(n.children, cid)
		if err := t.adapter.Attach(id, cid); err != nil {
			return id, SchemaError{Op: "attach", Node: cid, Pos: child.Pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
	}

	return id, nil
}

// SetProp applies a value the host program chose, as opposed to one the schema
// declared.
//
// It is legal during a signal emission. A handler setting a property is the
// ordinary case this whole design exists to serve, and refusing it would make
// the emission contract useless.
func (t *Tree) SetProp(id NodeID, prop string, v parse.SpecValue) error {
	if _, ok := t.nodes[id]; !ok {
		return SchemaError{Op: "set", Node: id, Detail: prop, Err: ErrNoSuchNode}
	}
	app := Application{Node: id, Prop: prop, Value: v, Origin: FromHost}
	if err := t.adapter.Apply(app); err != nil {
		return SchemaError{Op: "set", Node: id, Detail: prop, Pos: v.Pos,
			Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
	}
	return nil
}

// Emit runs the handlers bound to a node's signal, synchronously, in the order
// the schema declared them.
//
// Synchronous is deliberate. A queued emission would make the order in which
// effects land depend on where the queue happened to be drained, and a schema
// author has no way to see that. Running here means the effects of a handler
// are complete when Emit returns.
//
// The rules, all of which a caller can rely on:
//
//   - Handlers run in schema document order.
//   - A signal that re-enters itself while still running is REFUSED with
//     [ErrSignalCycle], immediately, before the second entry does anything.
//   - A chain of distinct signals deeper than the cap is refused with
//     [ErrEmitDepth]. That is a backstop for long acyclic chains, not the cycle
//     mechanism.
//   - A handler that returns an error STOPS the emission. Later handlers for
//     that signal DO NOT RUN.
//   - Property writes a handler made before erroring STAY COMMITTED. There is
//     no rollback. Claiming otherwise would mean pretending the adapter's
//     setters are reversible, and they are not.
//   - Mounting is refused while a signal is running.
//
// Emitting a signal nothing is bound to is a no-op and not an error: a schema
// that simply does not care about a widget's signal is ordinary.
func (t *Tree) Emit(id NodeID, signal string) error {
	n, ok := t.nodes[id]
	if !ok {
		return SchemaError{Op: "emit", Node: id, Detail: signal, Err: ErrNoSuchNode}
	}

	key := signalKey{node: id, signal: signal}
	for _, k := range t.active {
		if k == key {
			return SchemaError{Op: "emit", Node: id, Detail: signal, Pos: n.pos,
				Err: fmt.Errorf("%w: %s on node %d is already running", ErrSignalCycle, signal, id)}
		}
	}
	if len(t.active) >= t.maxDepth {
		return SchemaError{Op: "emit", Node: id, Detail: signal, Pos: n.pos,
			Err: fmt.Errorf("%w: %d signals deep", ErrEmitDepth, len(t.active))}
	}

	handlers := n.handlers[signal]
	if len(handlers) == 0 {
		return nil
	}

	prev := t.ph
	t.ph = phaseEmitting
	t.active = append(t.active, key)
	defer func() {
		t.active = t.active[:len(t.active)-1]
		t.ph = prev
	}()

	for _, h := range handlers {
		if err := h.fn(); err != nil {
			return SchemaError{Op: "emit", Node: id, Detail: signal + " -> " + h.name,
				Pos: h.pos, Err: err}
		}
	}
	return nil
}

// HandlerNames reports the handler names bound to a node's signal, in run
// order. It exists so a consumer can show what a schema wired up without the
// engine having to expose the functions themselves.
func (t *Tree) HandlerNames(id NodeID, signal string) []string {
	n, ok := t.nodes[id]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(n.handlers[signal]))
	for _, h := range n.handlers[signal] {
		out = append(out, h.name)
	}
	return out
}

// Destroy tears the tree down, releasing nodes in REVERSE mount order so a
// child is always released before its parent.
//
// Every node is offered to the adapter even if an earlier release failed, and
// the first error is returned once the walk is complete. Stopping at the first
// failure would strand every remaining node, which is a worse outcome than
// reporting one error late.
func (t *Tree) Destroy() error {
	if t.ph == phaseEmitting {
		return SchemaError{Op: "destroy", Err: fmt.Errorf("%w: %s", ErrPhase, t.ph)}
	}
	var first error
	for i := len(t.mounted) - 1; i >= 0; i-- {
		id := t.mounted[i]
		if err := t.adapter.Destroy(id); err != nil && first == nil {
			first = SchemaError{Op: "destroy", Node: id,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
	}
	t.nodes = make(map[NodeID]*node)
	t.mounted = nil
	t.root = NoNode
	return first
}
