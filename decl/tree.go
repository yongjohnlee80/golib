package decl

import (
	"errors"
	"fmt"
	"github.com/yongjohnlee80/golib/parse/qml"
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
	phasePropagating
)

func (p phase) String() string {
	switch p {
	case phasePropagating:
		return "propagating a source change"
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
	planned map[*qml.SpecNode]plannedNode
	// mutated records that a reconcile has actually CHANGED the live tree, as
	// opposed to having only constructed replacements that were then discarded.
	// It is what lets a reconcile that failed while still building report the
	// failure without latching a tree it never touched.
	mutated bool

	// sources are the host-declared reactive values, by name. They are declared
	// before Mount so a schema's references can be checked against a known set.
	sources map[string]qml.SpecValue
	// funcs are the host-declared value functions a Call may name.
	funcs map[string]ValueFunc
	// consts are the qualified names the adapter defines — `Tui.Horizontal`.
	// They are collected once from the adapter, because they do not change.
	consts map[string]qml.SpecValue
	// injected is the typed registry: every name a schema may reach, paired with
	// the host's declaration of WHAT IT IS. It is the one scope the resolver
	// consults, which is what gives "where did this name come from?" exactly one
	// answer. See [Tree.Inject].
	injected map[string]Injected
	// modules are the importable namespaces, by name. They are distinct from
	// injected namespaces: a module must be IMPORTED before its names resolve,
	// which is what stops an `import` line from being decoration.
	modules map[string]Module
	// offered are modules the host made importable without loading; loads are
	// the ones a document's import has loaded, with what each load wrote.
	offered map[string]offer
	loads   map[string]*loaded
	// stale are loaded modules ClearComponentCache marked: the next document
	// that imports one loads it afresh. refreshing makes that reconcile compare
	// values rather than declarations, until one succeeds.
	stale      map[string]bool
	refreshing bool
	// components are the component types loaded modules brought, by name.
	components map[string]component
	// imported is what the document's import lines brought into scope. It is
	// replaced on every Mount and Reconcile, because imports belong to the
	// DOCUMENT rather than to the tree, and a reload that drops an import must
	// stop resolving through it.
	imported imports
	// bindings are every bound property in the tree, in DOCUMENT ORDER — the
	// only order a schema author can see, and therefore the only defensible
	// fan-out order when a propagation stops part-way.
	bindings []*binding
	// preEval holds each node's properties with its bindings already evaluated,
	// computed by a whole-tree pass BEFORE any node is allocated. Without it a
	// binding failure deep in a schema would leave the nodes above it allocated
	// and the tree latched, when nothing had been built at all.
	preEval map[*qml.SpecNode][]qml.SpecProp
	// failed records that a Mount did not complete. A tree in that state holds
	// a partial graph, so the next Mount must be refused rather than allowed to
	// graft a second graph onto the wreckage — which is exactly what happens
	// when the guard keys on "is there a root" and a failed mount never set one.
	failed bool
	// active is the stack of signals currently running, innermost last. It is
	// the cycle detector: a pair already on the stack cannot be entered again.
	active []activeEmission
	// deferred are the signals raised while the tree was in the middle of an
	// operation, waiting for it to commit. See Emit.
	deferred []deferredSignal

	// sched puts work on the goroutine that owns this tree. Provider callbacks
	// arrive from wherever the host's data lives, which is not that goroutine.
	sched func(func())
	// providers are the live subscriptions, in subscription order.
	providers []*provider
	// onProviderError is where a delivery's failure goes. A delivery has no
	// caller to return an error to.
	onProviderError func(error)
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
	props []qml.SpecProp
	// declared is what the schema WROTE, expressions and all. props holds the
	// evaluated terminals; a reload compares declarations, because a binding
	// whose expression is unchanged must not re-fire just because a source
	// moved underneath it.
	declared []qml.SpecProp
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
	// key is the structural fingerprint of the SOURCE this was compiled from.
	// A reconcile compares it against the incoming schema to decide whether a
	// handler changed — a question the compiled function itself cannot answer,
	// and which an earlier design asked of a handler NAME that a body has no
	// single one of.
	key  string
	name string
	pos  parse.Position
	fn   func(args []qml.SpecValue) error
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
	// An adapter's qualified vocabulary is fixed, so it is read once here
	// rather than consulted on every resolution.
	if c, ok := a.(Constants); ok {
		t.consts = c.Constants()
	}
	// An adapter's modules go in through the SAME registration as a host's, so
	// every rule about names and exports applies to both. Copying them straight
	// into the map let an adapter publish two modules exporting one name, which
	// nothing downstream could then tell apart.
	//
	// It PANICS rather than returning an error because an adapter is code, not
	// input: a schema is a document a user wrote and its mistakes are reported,
	// while an adapter exporting one name from two modules is a programming
	// mistake in the same class as the nil adapter above — and New has no
	// caller who could do anything useful with an error about it.
	if m, ok := a.(Modules); ok {
		for _, mod := range m.Modules() {
			if err := t.registerModule("adapter modules", mod); err != nil {
				panic("decl.New: " + err.Error())
			}
		}
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

// NodeByID finds the node a document declared with `id: name`.
//
// It is how a HOST reaches a widget the schema built — the editor whose mode
// the status line reports, the dialog an Exit command opens. QML's `id` exists
// so that things can be referred to; without this a host could only walk the
// tree comparing SchemaIDs, which is the same lookup done badly in every
// caller.
//
// It reports false for an id no mounted node declares.
func (t *Tree) NodeByID(schemaID string) (NodeID, bool) {
	if schemaID == "" {
		return NoNode, false
	}
	for id, n := range t.nodes {
		if n.schemaID == schemaID {
			return id, true
		}
	}
	return NoNode, false
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
func (t *Tree) Mount(spec qml.SpecTree) error {
	return t.settle(t.mount(spec))
}

func (t *Tree) mount(spec qml.SpecTree) (err error) {
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

	// OFFERED MODULES the document imports are loaded before it is vetted,
	// since vetting resolves imports against loaded modules. A mount refused
	// before anything was built unloads them again: the tree is left exactly as
	// it was, and a corrected document can be mounted over it.
	undo, err := t.loadImported(spec)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil && !t.failed {
			undo()
		}
	}()

	// IMPORTS FIRST. Every qualified name below is resolved against what this
	// document imported, so the import set has to exist before the first name is
	// looked up — and an import of something no host provides is a better thing
	// to be told than "unbound name" at each of the twenty places that use it.
	// The whole document is judged — imports, ids, attached properties —
	// before a single node is planned, by the same vetting Reconcile uses.
	imported, spec, err := t.vetDocument(spec)
	if err != nil {
		return err
	}
	t.imported = imported

	// Bindings are validated and evaluated across the WHOLE schema first. A
	// failure here has allocated nothing, built nothing and latched nothing —
	// the tree is exactly as it was, and mountable again once the schema is
	// corrected.
	t.preEval = map[*qml.SpecNode][]qml.SpecProp{}
	defer func() { t.preEval = nil }()
	if err := t.preEvaluate(spec.Root); err != nil {
		return err
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

func (t *Tree) mountNode(sn *qml.SpecNode, parent NodeID) (NodeID, error) {
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
		declared: sn.Props,
		consumed: map[string]bool{},
		wired:    map[string]bool{},
	}
	t.nodes[id] = n

	// Bindings are validated and evaluated BEFORE anything is built, so a
	// schema mistake is found while the tree is still intact. The adapter never
	// sees an expression: Construction and every Application carry terminals.
	effective, preEvaluated := t.preEval[sn]
	if !preEvaluated {
		// No pre-pass ran (a reconcile mounts subtrees it has already planned
		// through its own path), so validate and evaluate here.
		if err := t.checkBindable(sn.Type, sn.Props, id); err != nil {
			return id, err
		}
		var err error
		if _, effective, err = t.bindingsFor(id, sn.Props); err != nil {
			return id, err
		}
	}
	bs, err := t.bindingsOn(id, sn.Props)
	if err != nil {
		return id, err
	}
	n.props = effective
	n.declared = sn.Props

	// Handlers are resolved before construction, because a widget may only
	// accept its callback as a constructor option and never expose a setter.
	if pre != nil {
		n.handlers = pre
	}
	for _, h := range sn.Handlers {
		if pre != nil {
			break // already resolved, during planning
		}
		bh, err := t.compileHandler(id, sn.Type, h)
		if err != nil {
			return id, err
		}
		n.handlers[h.Signal] = append(n.handlers[h.Signal], bh)
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
	var emitters map[string]func(args ...qml.SpecValue) error
	if len(n.handlers) > 0 {
		emitters = make(map[string]func(args ...qml.SpecValue) error, len(n.handlers))
		for signal := range n.handlers {
			emitters[signal] = func(args ...qml.SpecValue) error { return t.Emit(id, signal, args...) }
			n.wired[signal] = true
		}
	}

	consumed, err := t.adapter.Create(Construction{
		Node:     id,
		Type:     sn.Type,
		Pos:      sn.Pos,
		Props:    effective,
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
	// The node is built, so its bindings become live. Registering after Create
	// means a construction failure leaves none behind.
	t.registerBindings(bs)

	// Whatever construction did not claim is applied in document order. The
	// engine does not re-apply a consumed property: some have no setter, and
	// some setters assign and invalidate unconditionally, so a replay is either
	// impossible or a second visible effect rather than a free no-op.
	for _, p := range effective {
		if claimed[p.Name] {
			// A builder that claimed a BOUND property leaves no Apply to
			// succeed, so the terminal it was handed IS the applied value and
			// seeds the cache here instead. Without this the first tick could
			// not tell an unchanged value from a new one.
			t.noteApplied(id, p.Name, p.Value)
			continue
		}
		origin := FromSchema
		if _, bound := t.bindingFor(id, p.Name); bound {
			origin = FromBinding
		}
		app := Application{Node: id, Prop: p.Name, Value: p.Value, Origin: origin}
		if err := t.adapter.Apply(app); err != nil {
			return id, SchemaError{Op: "apply", Node: id, Detail: p.Name, Pos: p.Value.Pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
		t.noteApplied(id, p.Name, p.Value)
	}

	return id, nil
}

// SetProp applies a value the host program chose, as opposed to one the schema
// declared.
//
// It is legal during a signal emission. A handler setting a property is the
// ordinary case this whole design exists to serve, and refusing it would make
// the emission contract useless.
func (t *Tree) SetProp(id NodeID, prop string, v qml.SpecValue) error {
	if t.ph == phaseMounting || t.ph == phaseDestroying || t.ph == phaseReconciling ||
		t.ph == phasePropagating {
		return SchemaError{Op: "set", Node: id, Detail: prop,
			Err: fmt.Errorf("%w: %s", ErrPhase, t.ph)}
	}
	// A bound property has ONE writer. A host value the next source tick
	// silently reverts would leave a value whose origin cannot be determined
	// from the screen, and there is no moment a caller could reason about as
	// "the binding takes over again".
	if b, bound := t.bindingFor(id, prop); bound {
		return SchemaError{Op: "set", Node: id, Detail: prop, Pos: b.pos, Err: fmt.Errorf(
			"%w: %q is bound by the schema, which is its only writer", ErrPhase, prop)}
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
//
// args are the signal's parameters, in the order the adapter declared them;
// a handler reaches them by name, as QML's do.
func (t *Tree) Emit(id NodeID, signal string, args ...qml.SpecValue) error {
	// A signal raised while the tree is in the MIDDLE of an operation is
	// DEFERRED until that operation has committed, not refused and not run.
	//
	// Running it now is what the phase rule exists to prevent: a handler would
	// mutate the tree between two entries of one fan-out, or underneath the
	// walk that is rebuilding it. But refusing it — which is what this did —
	// loses a signal that genuinely happened. Widgets fire during these
	// operations for real reasons: a setter the engine applies can change a
	// widget's state and the widget reports it, as an editor does when a bound
	// keyset switches it out of Normal mode. The status line then has to hear.
	//
	// So it is queued and delivered once the tree is consistent again.
	if t.deferring() {
		t.deferSignal(signalKey{node: id, signal: signal}, args)
		return nil
	}
	// A tree being destroyed has nothing left for a handler to act on, and a
	// signal deferred past the teardown would be delivered to nodes that no
	// longer exist. Refused.
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
		if err := h.fn(args); err != nil {
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
	case phaseEmitting, phaseMounting, phaseDestroying, phaseReconciling, phasePropagating:
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
	// Bindings AND sources go with the tree. ADR-decl-0001b R2 makes Destroy
	// the lifetime boundary for both, and the alternative is a half-reset
	// engine: DeclareSource is refused after Mount, so a tree re-mounted with
	// sources carried over could never have its set corrected.
	t.bindings = nil
	t.sources = nil
	// The typed registry goes with them, and so do the functions it seeded. The
	// registry is the authority on what a name IS, so a function surviving a
	// Destroy that forgot its entry would be callable by a schema the resolver
	// can no longer answer for — two stores disagreeing, which is the exact
	// failure routing every declaration through Inject exists to prevent.
	t.unloadAll()
	t.injected = nil
	t.funcs = nil
	// Subscriptions end with the tree they fed. A provider still delivering
	// into a destroyed tree would find no sources and no nodes, and its errors
	// are joined with the adapter's rather than replacing them: a teardown that
	// reported only the first thing to go wrong hides the rest.
	errs = append(errs, t.unsubscribeAll())
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
