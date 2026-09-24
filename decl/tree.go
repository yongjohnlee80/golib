package decl

import (
	"errors"
	"fmt"
	"sort"

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
	phaseDestroying
	phaseReconciling
)

func (p phase) String() string {
	switch p {
	case phaseReconciling:
		return "reconciling"
	case phaseMounting:
		return "mounting"
	case phaseEmitting:
		return "emitting a signal"
	case phaseDestroying:
		return "tearing down"
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
	root   NodeID

	ph phase
	// planned holds the identities and bindings allocated during a reconcile's
	// PLANNING pass for subtrees that will be mounted fresh. It exists so a
	// handler name that does not resolve is discovered while the tree is still
	// intact: without it, a typo in a NEW node is only found after the node it
	// replaces has already been detached and destroyed.
	planned map[*parse.SpecNode]plannedNode
	// mutated records that a reconcile has actually CHANGED the live tree, as
	// opposed to having only constructed replacements that were then discarded.
	// It is what lets a reconcile that failed while still building report the
	// failure without latching a tree it never touched.
	mutated bool
	// failed records that a Mount did not complete. A tree in that state holds
	// a partial graph, so the next Mount must be refused rather than allowed to
	// graft a second graph onto the wreckage — which is exactly what happens
	// when the guard keys on "is there a root" and a failed mount never set one.
	failed bool
	// active is the stack of signals currently running, innermost last. It is
	// the cycle detector: a pair already on the stack cannot be entered again.
	active []activeEmission
}

// plannedNode is one fresh node's pre-allocated identity and pre-resolved
// bindings, carried from the planning pass into the mount that follows.
type plannedNode struct {
	id       NodeID
	handlers map[string][]boundHandler
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
	// built records that the adapter's Create returned successfully. A node
	// that never finished construction is not offered to Destroy.
	built bool
	// handlers are per signal, in the order the schema declared them, which is
	// the order they run.
	handlers map[string][]boundHandler

	// props is the declared property set this node currently reflects, in
	// document order. A reconcile needs it to answer "what changed", which is a
	// question about the PREVIOUS schema — and the previous schema is gone by
	// the time the new one arrives.
	props []parse.SpecProp
	// consumed names the properties the adapter took at construction. They are
	// recorded because a consumed property is, by definition, one the adapter
	// could not be asked to set later: consuming it is how an adapter says
	// "there is no setter for this". A reconcile that changes one therefore has
	// no path other than rebuilding the node.
	consumed map[string]bool
	// wired is the set of signals that had an emitter at construction. A signal
	// appearing for the first time in a reloaded schema cannot be attached to an
	// already-built widget — some accept a callback only as a constructor
	// option — so it, too, forces a rebuild.
	wired map[string]bool
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

// activeEmission is one live emission on the stack. It carries the position the
// emission STARTED at, so a cycle can name both ends: where the signal first
// ran, and where it tried to run again. One position names only the collision
// and leaves the reader to find the other half themselves.
type activeEmission struct {
	key signalKey
	pos parse.Position
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

// Mount instantiates spec.
//
// Order is part of the contract, not an implementation detail. For each node,
// in this order:
//
//  1. its handlers are RESOLVED, before anything is built — a widget may accept
//     its callback only as a constructor argument;
//  2. its CHILDREN are mounted, left to right, by this same sequence;
//  3. the node itself is CREATED, receiving its declared properties, its
//     already-built children, and one emitter per distinct signal;
//  4. every property the adapter did NOT report consuming is applied, in
//     document order.
//
// So construction runs bottom-up while identity is allocated top-down: node IDs
// read in schema order even though the building starts at the leaves. Both
// halves matter — a container may require its children as constructor arguments
// with no way to add them later, and a diagnostic that numbered nodes backwards
// would be needlessly hard to match against the file.
//
// A failure part-way leaves what was already built IN PLACE and returns the
// error. There is no rollback, and inventing one would be worse than saying so:
// the adapter has run real constructors and real setters, and the engine cannot
// know which of those are safely undoable. The tree REMEMBERS the failure and
// refuses a further Mount until [Tree.Destroy] has cleared it, so a second
// schema cannot be grafted onto a partial one.
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
	if t.failed {
		return SchemaError{Op: "mount", Err: fmt.Errorf(
			"%w: the previous mount failed and left a partial tree; call Destroy first", ErrPhase)}
	}

	t.ph = phaseMounting
	defer func() { t.ph = phaseIdle }()

	id, err := t.mountNode(spec.Root, NoNode)
	if err != nil {
		t.failed = true
		return err
	}
	t.root = id
	return nil
}

func (t *Tree) mountNode(sn *parse.SpecNode, parent NodeID) (NodeID, error) {
	// The ID is allocated BEFORE the children are built, so identities still
	// read in schema order even though construction runs bottom-up. A reader
	// comparing a diagnostic against the file should not have to think in
	// reverse.
	//
	// A reconcile allocates both the identity and the bindings during its
	// planning pass and leaves them here, so this mount adopts them rather than
	// reaching into the adapter again. Mount has no planning pass and falls
	// through to allocating its own.
	var id NodeID
	var pre map[string][]boundHandler
	if p, ok := t.planned[sn]; ok {
		id, pre = p.id, p.handlers
	} else {
		t.nextID++
		id = t.nextID
	}

	n := &node{
		id:       id,
		typeName: sn.Type,
		schemaID: sn.ID,
		pos:      sn.Pos,
		parent:   parent,
		handlers: make(map[string][]boundHandler),
		props:    sn.Props,
		consumed: map[string]bool{},
		wired:    map[string]bool{},
	}
	t.nodes[id] = n

	// Handlers are resolved before construction, because a widget may only
	// accept its callback as a constructor option and never expose a setter.
	if pre != nil {
		n.handlers = pre
	}
	for _, h := range sn.Handlers {
		if pre != nil {
			break // already resolved, during planning
		}
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

	// CHILDREN FIRST. A constructor that requires its children — a split needs
	// two, and has no method to supply them later — can only be called once
	// they exist.
	for _, child := range sn.Children {
		cid, err := t.mountNode(child, id)
		if err != nil {
			return id, err
		}
		n.children = append(n.children, cid)
	}

	// One emitter per DISTINCT signal. Three handlers on one signal share a
	// single entry, so one widget event runs the list once.
	var emitters map[string]func() error
	if len(n.handlers) > 0 {
		emitters = make(map[string]func() error, len(n.handlers))
		for signal := range n.handlers {
			emitters[signal] = func() error { return t.Emit(id, signal) }
			n.wired[signal] = true
		}
	}

	consumed, err := t.adapter.Create(Construction{
		Node:     id,
		Type:     sn.Type,
		Pos:      sn.Pos,
		Props:    sn.Props,
		Children: n.children,
		Emitters: emitters,
	})
	if err != nil {
		return id, SchemaError{Op: "create", Node: id, Detail: sn.Type, Pos: sn.Pos,
			Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
	}
	// Only now is the node considered built, which is what makes teardown of a
	// partial tree correct: a node whose Create never returned is not offered
	// to Destroy.
	n.built = true

	claimed := n.consumed
	for _, name := range consumed {
		claimed[name] = true
	}

	// Whatever construction did not claim is applied in document order. The
	// engine does not re-apply a consumed property: some have no setter, and
	// some setters assign and invalidate unconditionally, so a replay is either
	// impossible or a second visible effect rather than a free no-op.
	for _, p := range sn.Props {
		if claimed[p.Name] {
			continue
		}
		app := Application{Node: id, Prop: p.Name, Value: p.Value, Origin: FromSchema}
		if err := t.adapter.Apply(app); err != nil {
			return id, SchemaError{Op: "apply", Node: id, Detail: p.Name, Pos: p.Value.Pos,
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
	if t.ph == phaseMounting || t.ph == phaseDestroying || t.ph == phaseReconciling {
		return SchemaError{Op: "set", Node: id, Detail: prop,
			Err: fmt.Errorf("%w: %s", ErrPhase, t.ph)}
	}
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
	if t.ph == phaseReconciling {
		// A handler running mid-reconcile would mutate the tree underneath the
		// walk that is rebuilding it. Widgets do fire during a reconcile — a
		// container relaying a removal, say — so this is a real path, not a
		// defensive impossibility.
		return SchemaError{Op: "emit", Node: id, Detail: signal,
			Err: fmt.Errorf("%w: %s", ErrPhase, t.ph)}
	}
	if t.ph == phaseDestroying {
		return SchemaError{Op: "emit", Node: id, Detail: signal,
			Err: fmt.Errorf("%w: %s", ErrPhase, t.ph)}
	}
	n, ok := t.nodes[id]
	if !ok {
		return SchemaError{Op: "emit", Node: id, Detail: signal, Err: ErrNoSuchNode}
	}

	key := signalKey{node: id, signal: signal}
	for _, live := range t.active {
		if live.key == key {
			return SchemaError{Op: "emit", Node: id, Detail: signal, Pos: n.pos,
				Err: fmt.Errorf("%w: %s on node %d is already running, started at %s; "+
					"re-entered from %s", ErrSignalCycle, signal, id, live.pos, n.pos)}
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
	t.active = append(t.active, activeEmission{key: key, pos: n.pos})
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

// Destroy tears the tree down, releasing every node CHILDREN BEFORE PARENTS.
//
// The order is computed from the tree's own topology, not by reversing the
// order things were created. Those were the same thing while parents were built
// first; they stopped being the same the moment construction went bottom-up, and
// reversing creation order now would release every parent before its children —
// precisely the bug the rule exists to prevent. Topology is the thing that was
// ever actually meant.
//
// Partial trees are handled by the same walk: a node whose construction never
// completed is skipped, because the adapter never finished building it and has
// nothing to release.
//
// Every node is offered even if an earlier release failed, and the failures are
// joined into one error. Stopping at the first would strand every remaining
// node, and reporting only the first would throw away evidence that costs
// nothing to keep.
func (t *Tree) Destroy() error {
	switch t.ph {
	case phaseEmitting, phaseMounting, phaseDestroying, phaseReconciling:
		return SchemaError{Op: "destroy", Err: fmt.Errorf("%w: %s", ErrPhase, t.ph)}
	}

	prev := t.ph
	t.ph = phaseDestroying
	defer func() { t.ph = prev }()

	var errs []error
	for _, id := range t.teardownOrder() {
		n := t.nodes[id]
		if n == nil || !n.built {
			continue
		}
		if err := t.adapter.Destroy(id); err != nil {
			errs = append(errs, SchemaError{Op: "destroy", Node: id,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)})
		}
	}

	t.nodes = make(map[NodeID]*node)
	t.root = NoNode
	t.failed = false
	return errors.Join(errs...)
}

// teardownOrder returns every live node, deepest first, so a child always
// precedes its parent. Roots are every node whose parent is absent, which
// covers a partial tree whose top was never linked to anything.
func (t *Tree) teardownOrder() []NodeID {
	parented := make(map[NodeID]bool, len(t.nodes))
	for _, n := range t.nodes {
		for _, c := range n.children {
			parented[c] = true
		}
	}
	roots := make([]NodeID, 0, 1)
	for id := range t.nodes {
		if !parented[id] {
			roots = append(roots, id)
		}
	}
	// Deterministic: IDs are allocated in schema order, so sorting gives the
	// same walk every run, which is what makes an adapter's trace assertable.
	sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })

	var out []NodeID
	var visit func(NodeID)
	seen := make(map[NodeID]bool, len(t.nodes))
	visit = func(id NodeID) {
		if seen[id] {
			return
		}
		seen[id] = true
		if n := t.nodes[id]; n != nil {
			for _, c := range n.children {
				visit(c)
			}
		}
		out = append(out, id) // post-order: children land before their parent
	}
	for _, r := range roots {
		visit(r)
	}
	return out
}
