package decl

import (
	"errors"
	"fmt"
	"github.com/yongjohnlee80/golib/parse/qml"

	"github.com/yongjohnlee80/golib/parse"
)

// Restructurer is an OPTIONAL capability an [Adapter] may implement to let a
// reconcile change a node's children after construction.
//
// It is a separate interface rather than four more methods on Adapter because
// the two populations genuinely differ: a container can splice its children,
// and a widget that took both of them as required constructor arguments cannot.
// An adapter that does not implement this at all still reconciles — every
// structural change simply becomes a rebuild of the affected node, which is
// correct and merely lossy.
//
// Indices are positions in the parent's FINAL child order, and the engine only
// ever asks for one that is in range for the parent's child count at the moment
// of the call.
type Restructurer interface {
	// CanRestructure reports whether this node's children may change after
	// construction. It is consulted BEFORE anything is mutated, so a node that
	// says no is rebuilt instead of being half-edited and then refused.
	CanRestructure(node NodeID) bool

	// InsertChild places child among parent's children at index at.
	InsertChild(parent, child NodeID, at int) error

	// RemoveChild detaches child from parent. The engine destroys the child
	// afterwards; this call is only the structural half.
	RemoveChild(parent, child NodeID) error

	// MoveChild relocates child to index to WITHOUT rebuilding it. This is the
	// call that makes reload worth doing: everything the mounted child owns —
	// its state, its focus, its in-flight work — survives a move and does not
	// survive a remove and re-add.
	MoveChild(parent, child NodeID, to int) error
}

// PropertyKind says how a declared property can reach a node.
//
// The distinction that matters is between a property the type accepts only at
// construction and one the adapter does not recognise at all. Both are
// un-appliable, and a single boolean made them look identical — but the right
// response differs completely: one is rebuilt, and rebuilding the other is a
// pointless demolition, because a freshly built node would refuse it too.
type PropertyKind uint8

const (
	// PropUnknown means the adapter has no such property for this node's type.
	// It is a mistake in the schema, and a reconcile reports it WITHOUT
	// touching the tree — there is nothing a rebuild could achieve.
	PropUnknown PropertyKind = iota
	// PropRuntime means there is a setter: the value can be applied in place.
	PropRuntime
	// PropConstructorOnly means the type takes it at construction and offers no
	// setter, so changing it means building the node again.
	PropConstructorOnly
)

// String renders the kind for diagnostics.
func (k PropertyKind) String() string {
	switch k {
	case PropRuntime:
		return "runtime"
	case PropConstructorOnly:
		return "constructor-only"
	default:
		return "unknown"
	}
}

// Classifier is an OPTIONAL capability an [Adapter] may implement to say how a
// declared property can reach a node of a given TYPE.
//
// The engine cannot work this out. It sees what [Adapter.Create] reported
// consuming, and that inference has two holes. A builder reports a property
// consumed only when the schema DECLARED it, so a Split mounted without
// `orientation` consumes nothing and a later reload adding it looks ordinary.
// And nothing in the consumed-set distinguishes a constructor-only property
// from a typo — so a misspelled property name would demolish a working node to
// build one that refuses it just the same, while the diagnostic confidently
// blamed construction.
//
// The adapter owns the property tables, so the adapter is the only thing that
// can answer. An adapter that does not implement this still reconciles; it
// falls back to the consumed-set inference and keeps both holes.
//
// It is keyed by the schema TYPE NAME, not by a mounted node, and that is the
// whole point. The question "can a Button take a property called nosuch" is
// about Buttons, not about any particular one — and the nodes a reload most
// needs it for DO NOT EXIST YET: a node the schema adds, or the replacement for
// one whose type changed. An earlier version took a NodeID, used it only to
// look up that node's type, and was therefore unable to answer for exactly the
// cases where being wrong costs a working widget.
type Classifier interface {
	// ClassifyProperty reports how prop can reach a node of this schema type.
	// It must not have side effects: it is consulted during planning, before
	// anything is mutated.
	ClassifyProperty(typeName, prop string) PropertyKind
}

// ErrIncomplete reports source that stops mid-construct — the normal reading of
// a file an editor is part-way through writing.
//
// It is separate from a syntax error because the right response is different
// and a user can tell: incomplete means WAIT, invalid means the author made a
// mistake. Without the distinction every save would flash an error.
var ErrIncomplete = errors.New("decl: the source is incomplete")

// ErrNotMounted reports a reconcile against a tree that was never mounted.
var ErrNotMounted = errors.New("decl: the tree is not mounted")

// Rebuild records one node the reconciler could not patch in place, and why.
//
// It is reported rather than swallowed because a rebuild is exactly where a
// reload LOSES something — scroll offset, focus, a half-typed field — and a
// developer watching their screen reset deserves to be told which node did it
// and what about their edit caused it.
type Rebuild struct {
	// Type is the schema type of the rebuilt node.
	Type string
	// SchemaID is the `id:` it declared, empty when it declared none.
	SchemaID string
	// Pos is where the NEW schema declares it.
	Pos parse.Position
	// Reason is a sentence naming the specific edit that forced the rebuild.
	Reason string
}

// String renders the rebuild for a log line.
func (r Rebuild) String() string {
	s := r.Type
	if r.SchemaID != "" {
		s += "#" + r.SchemaID
	}
	if r.Pos.Line != 0 {
		s += " at " + r.Pos.String()
	}
	return s + ": " + r.Reason
}

// Result summarises what a reconcile did.
type Result struct {
	// Created counts nodes constructed, including those inside rebuilt subtrees.
	Created int
	// Destroyed counts nodes released.
	Destroyed int
	// Moved counts identity-preserving relocations — the ones that kept state.
	Moved int
	// Applied counts property applications.
	Applied int
	// Rebuilt lists every node that could not be patched in place.
	Rebuilt []Rebuild
	// RootReplaced reports that the root node itself was rebuilt, so the
	// component the host mounted into its application is GONE and the new root
	// must be mounted in its place. A host that ignores this renders the old
	// tree forever.
	RootReplaced bool
}

// Reload parses src and reconciles the tree against it.
//
// The three outcomes are deliberately distinguishable, because a file watcher
// sees all three and must react differently to each:
//
//   - src stops mid-construct → [ErrIncomplete], and THE TREE IS UNTOUCHED. An
//     editor writing a file is observed part-way through; this is not an error
//     to show anyone, it is a reason to wait for the next event.
//   - src is complete but invalid → the parse error, and THE TREE IS UNTOUCHED.
//     The last good screen stays on display, so a typo does not blank it.
//   - src parses → the reconcile runs, and its Result says what changed.
//
// Both failure paths return before anything is mutated, which is what makes
// "keep the last good tree" true rather than aspirational.
func (t *Tree) Reload(src []byte) (Result, error) {
	spec, err := qml.QML{}.Parse(src)
	if err != nil {
		var se parse.SyntaxError
		if errors.As(err, &se) && se.Incomplete {
			return Result{}, SchemaError{Op: "reload", Pos: se.Pos,
				Err: fmt.Errorf("%w: %w", ErrIncomplete, err)}
		}
		return Result{}, SchemaError{Op: "reload", Err: err}
	}
	return t.Reconcile(spec)
}

// Reconcile patches the mounted tree to match spec.
//
// Nodes that keep their identity keep everything the toolkit hung on them. Only
// what genuinely changed is touched, and a node that CANNOT be patched is
// rebuilt and named in [Result.Rebuilt] rather than quietly losing its state.
//
// Identity is matched in a fixed order of precedence:
//
//  1. a declared schema `id` wins outright, wherever in its parent it moved to;
//  2. otherwise position among the remaining children, which is what containers
//     already do.
//
// There is deliberately no third rule reading a component's own key. A key
// belongs to a MOUNTED COMPONENT, and the new side of a reload is text: there
// is nothing to ask. Reading one would mean constructing the node first, which
// is the cost a reconcile exists to avoid. A schema author who wants an
// identity that survives reordering writes an `id`, and that is the only
// identity this engine can honestly offer.
//
// The work is planned in full BEFORE anything is mutated. Every handler is
// resolved — including those in subtrees the schema ADDS, whose identities are
// allocated during planning for exactly this reason — and every node's ability
// to restructure and to accept each changed property is cleared with the
// adapter first. So the ordinary mistakes, a mistyped handler name or a
// property the widget cannot set, are found while the screen is still intact.
//
// THREE THINGS CANNOT BE PRE-CHECKED:
//
//   - a CONSTRUCTOR, because classification proves a property EXISTS and only
//     the builder can say whether it accepts this VALUE. This one is made
//     harmless rather than merely reported: every replacement is BUILT BEFORE
//     anything it replaces is released, so a refusal discards the half-built
//     replacement and leaves the live tree standing;
//   - a SETTER, because the only way to learn that it refuses a VALUE is to
//     call it. Whether the property exists at all is settled during planning;
//     whether this particular value is acceptable is not;
//   - a STRUCTURAL operation. [Restructurer.CanRestructure] settles whether a
//     node accepts child changes at all, but Insert, Remove and Move each
//     return an error at the moment they run, after earlier structural work has
//     already landed.
//
// The last two are PARTIAL-MUTATION POINTS: a failure there leaves the tree
// partially reconciled and latches it, exactly as a failed [Tree.Mount] does,
// and the next Mount or Reconcile is refused until [Tree.Destroy] has cleared
// it. Pretending otherwise would mean claiming the adapter's setters and its
// container are reversible, and they are not.
//
// A reconcile that fails while still CONSTRUCTING has changed nothing, and is
// not latched. The difference is tracked rather than assumed: the tree records
// when it first touches something live, and a failure before that point is
// reported without declaring the tree partial.
//
// THE BOUNDARY IS PER-PARENT, NOT WHOLE-TREE, and the difference is visible.
// Replacements are built before anything is released within ONE parent's child
// list, but a node is patched — its own properties applied — before its
// children's replacements are constructed. So a property change on one node
// followed by a refused constructor DEEPER IN THE TREE leaves the applied value
// in place, and the tree does latch. Closing that would mean deferring every
// property application to a commit phase across the whole reconcile, which is a
// different design and not one this engine makes; saying so is the alternative
// to implying an atomicity it does not have.
func (t *Tree) Reconcile(spec qml.SpecTree) (Result, error) {
	res, err := t.reconcile(spec)
	return res, t.settle(err)
}

func (t *Tree) reconcile(spec qml.SpecTree) (_ Result, err error) {
	if t.ph != phaseIdle {
		return Result{}, SchemaError{Op: "reconcile", Err: fmt.Errorf("%w: %s", ErrPhase, t.ph)}
	}
	if spec.Root == nil {
		return Result{}, SchemaError{Op: "reconcile", Err: fmt.Errorf("%w: the schema has no root node", ErrPhase)}
	}
	if t.failed {
		return Result{}, SchemaError{Op: "reconcile", Err: fmt.Errorf(
			"%w: the previous operation failed and left a partial tree; call Destroy first", ErrPhase)}
	}
	if t.root == NoNode {
		return Result{}, SchemaError{Op: "reconcile", Err: ErrNotMounted}
	}

	// A module the new document is the first to import loads here, as it would
	// at Mount — which is what lets a large application bring in a dialog's
	// module when the document that needs it first appears. A reload that is
	// refused without touching the live tree unloads it again.
	// mutated is reset FIRST: the undo below reads it, and a refusal before
	// planning must not see the last reconcile's answer.
	t.mutated = false
	undo, err := t.loadImported(spec)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		if err != nil && !t.mutated {
			undo()
		}
	}()

	// The NEW document's imports, resolved before anything is planned and only
	// adopted once they are valid. A reload whose import line is wrong must
	// leave the last good screen exactly as it is, including the import set the
	// live tree resolved against — which a reload that assigned first and
	// checked afterwards would already have destroyed.
	imported, spec, err := t.vetDocument(spec)
	if err != nil {
		return Result{}, err
	}
	prevImports := t.imported
	t.imported = imported

	t.ph = phaseReconciling
	t.planned = map[*qml.SpecNode]plannedNode{}
	t.preEval = map[*qml.SpecNode][]qml.SpecProp{}
	t.mutated = false
	defer func() {
		// An untouched tree keeps the imports it was mounted with. Only a
		// reconcile that actually changed the live tree has made the new
		// document the one on screen.
		if !t.mutated && t.failed {
			t.imported = prevImports
		}
		t.ph = phaseIdle
		t.planned = nil
		t.preEval = nil
	}()

	// PLAN. Nothing below this line mutates the tree; the walk only reads it and
	// asks the adapter questions that have no side effects.
	plan, err := t.assess(t.root, spec.Root)
	if err != nil {
		return Result{}, err
	}

	// APPLY.
	var res Result
	if plan.rebuild {
		// The root has no parent to splice it into, so a root rebuild is the
		// whole tree. THE REPLACEMENT IS BUILT FIRST, while the live tree is
		// still standing: a constructor can refuse a VALUE that classification
		// already proved to be a real property — "diagonal" is a direction Flex
		// has no meaning for — and releasing first would take the working
		// screen down for a typo the new tree never got far enough to display.
		old := t.root
		id, err := t.mountNode(spec.Root, NoNode)
		if err != nil {
			// The live tree was never touched. Discard the half-built
			// replacement and leave everything exactly as it was.
			return Result{}, t.discardReplacements(err, id)
		}
		res.Rebuilt = append(res.Rebuilt, plan.rebuildRecord())
		res.RootReplaced = true
		res.Created = t.countBuilt(id)

		t.mutated = true
		if err := t.releaseSubtree(old, &res); err != nil {
			t.failed = true
			return res, err
		}
		t.root = id
		return res, nil
	}

	if _, err := t.patch(plan, NoNode, &res); err != nil {
		// Only a reconcile that actually CHANGED something leaves a partial
		// tree. One that failed while still constructing has nothing to latch.
		t.failed = t.mutated
		return res, err
	}
	return res, nil
}

// step is one node's plan: either patch it in place, or rebuild it.
type step struct {
	// old is the node this slot currently holds, or NoNode for a fresh subtree.
	old NodeID
	// spec is what the new schema says this slot should be.
	spec *qml.SpecNode

	// rebuild marks a slot that cannot be patched.
	rebuild bool
	// reason names the edit that forced the rebuild, for diagnostics.
	reason string

	// apply is the properties to set, in document order. Empty when nothing
	// changed, which is the common case and the point of reconciling.
	// Values here are TERMINAL: a binding has already been evaluated.
	apply []qml.SpecProp
	// effective is the node's full property list with bindings evaluated, which
	// is what the node records so the next reload compares like with like.
	effective []qml.SpecProp
	// bind are the node's binding registrations after this reconcile.
	bind []*binding
	// handlers are already RESOLVED, during planning, so the mutating pass
	// cannot fail on a name that does not exist in the host. They are only
	// meaningful when rebind is set.
	handlers map[string][]boundHandler
	// rebind marks that the bindings CHANGED. It is separate from handlers
	// being empty, because "the schema removed every handler" and "the
	// bindings are already correct" are different instructions that would
	// otherwise look identical — and confusing them silently unbinds a live
	// widget.
	rebind bool
	// order is the child plan in the NEW schema's order.
	order []*step
	// dropped are old children with no counterpart in the new schema.
	dropped []NodeID
	// restructure marks that the child list itself changes — an insert, a
	// removal, a reorder, or a child being swapped out by a rebuild.
	restructure bool
}

func (s *step) rebuildRecord() Rebuild {
	return Rebuild{Type: s.spec.Type, SchemaID: s.spec.ID, Pos: s.spec.Pos, Reason: s.reason}
}

// fresh returns the plan for a slot with nothing in it yet, having first
// planned the subtree that will fill it.
func (t *Tree) fresh(sn *qml.SpecNode) (*step, error) {
	if err := t.planSubtree(sn); err != nil {
		return nil, err
	}
	return &step{old: NoNode, spec: sn, rebuild: true, reason: "the schema adds this node"}, nil
}

// rebuildStep marks a node for rebuilding, planning the replacement subtree
// first so the mutating pass has nothing left to discover.
func (t *Tree) rebuildStep(s *step, reason string) (*step, error) {
	if err := t.planSubtree(s.spec); err != nil {
		return nil, err
	}
	s.rebuild = true
	s.reason = reason
	s.order, s.dropped, s.apply, s.handlers, s.rebind = nil, nil, nil, nil, false
	return s, nil
}

// planSubtree allocates identity and resolves every handler for a subtree that
// is about to be mounted fresh, WITHOUT touching the live tree.
//
// This is the half of "plan before you mutate" that a reconcile originally
// missed. A fresh node used to resolve its handlers inside mountNode, which
// runs after the node it replaces has already been detached and destroyed — so
// a typo in a NEW handler name tore down the working screen and latched the
// tree before reporting it. Identity is allocated here too, pre-order, so node
// numbers still read in schema order.
func (t *Tree) planSubtree(sn *qml.SpecNode) error {
	t.nextID++
	id := t.nextID
	p := plannedNode{id: id}

	// Properties are checked here too, against the type the node WILL be. This
	// is the case the classifier exists for: the node has no instance to ask
	// about, and discovering a misspelled property during the mount means
	// discovering it after the node it replaces has been destroyed.
	if classifier, ok := t.adapter.(Classifier); ok {
		for _, prop := range sn.Props {
			if t.isBinding(prop.Value) {
				continue // checkBindable classifies bindings by its own rules
			}
			if err := checkKind(classifier.ClassifyProperty(sn.Type, prop.Name), sn.Type, prop, id); err != nil {
				return err
			}
		}
	}
	// Bindings are validated and EVALUATED here, before anything is mutated, so
	// an unknown source or a failing value function leaves the tree intact.
	if err := t.checkBindable(sn.Type, sn.Props, id); err != nil {
		return err
	}
	// The evaluation is KEPT for the mount that follows. Discarding it made a
	// fresh binding invoke its ValueFunc twice per reload — invisible for a
	// pure function, and not something this seam is entitled to assume.
	_, effective, err := t.bindingsFor(id, sn.Props)
	if err != nil {
		return err
	}
	if t.preEval == nil {
		t.preEval = map[*qml.SpecNode][]qml.SpecProp{}
	}
	t.preEval[sn] = effective

	for _, h := range sn.Handlers {
		bh, err := t.compileHandler(id, sn.Type, h)
		if err != nil {
			return err
		}
		if p.handlers == nil {
			p.handlers = map[string][]boundHandler{}
		}
		p.handlers[h.Signal] = append(p.handlers[h.Signal], bh)
	}
	if p.handlers == nil {
		// A node with no handlers still needs an entry: its presence is what
		// tells mountNode to adopt the planned identity rather than allocate a
		// second one.
		p.handlers = map[string][]boundHandler{}
	}
	t.planned[sn] = p

	for _, child := range sn.Children {
		if err := t.planSubtree(child); err != nil {
			return err
		}
	}
	return nil
}

// assess plans one matched pair, children first. It reads the tree and the
// adapter's read-only capabilities; it changes nothing.
//
// Children are planned before their parent because a child that must be rebuilt
// changes what the PARENT needs: swapping a child out is a structural edit, and
// a parent that cannot restructure must therefore be rebuilt too. Deciding the
// parent first would mean deciding it on incomplete information.
func (t *Tree) assess(oldID NodeID, sn *qml.SpecNode) (*step, error) {
	n := t.nodes[oldID]
	if n == nil {
		return nil, SchemaError{Op: "reconcile", Node: oldID, Pos: sn.Pos, Err: ErrNoSuchNode}
	}
	s := &step{old: oldID, spec: sn}

	if n.typeName != sn.Type {
		return t.rebuildStep(s, fmt.Sprintf("the type changed from %s to %s", n.typeName, sn.Type))
	}

	oldProps := propSequences(n.declared)
	newProps := propSequences(sn.Props)

	// classify says how a changed property can reach this node. Without the
	// capability the engine falls back to what Create reported consuming, which
	// cannot see a property that was ABSENT at construction and cannot tell a
	// constructor-only property from a typo.
	// A matched node's declarations are validated too: a reload can introduce a
	// binding, a duplicate, or a binding on a constructor-only property.
	if err := t.checkBindable(sn.Type, sn.Props, oldID); err != nil {
		return nil, err
	}

	classify := t.classifier(n)

	// UNKNOWN PROPERTIES FIRST, across every changed declaration, because an
	// error outranks a rebuild: if the schema names a property the adapter does
	// not have, demolishing the node achieves nothing — the replacement would
	// refuse it too — and the tree must be left exactly as it was.
	for _, p := range sn.Props {
		if sameSequence(oldProps[p.Name], newProps[p.Name]) {
			continue
		}
		if err := checkKind(classify(sn.Type, p.Name), sn.Type, p, oldID); err != nil {
			return nil, err
		}
	}
	// Then constructor-only changes, which DO call for a rebuild. Document
	// order, so the reason a reader gets is the first one in the file rather
	// than whichever the map yielded.
	for _, p := range sn.Props {
		if sameSequence(oldProps[p.Name], newProps[p.Name]) {
			continue
		}
		if classify(sn.Type, p.Name) == PropConstructorOnly {
			return t.rebuildStep(s, fmt.Sprintf(
				"%q is taken at construction and has no setter, so changing it cannot be applied", p.Name))
		}
	}
	// A property that disappears cannot be un-applied either: there is no
	// "unset" in the seam, and the engine holds no default to restore.
	// Iterating the node's own list rather than the map keeps the reported
	// reason stable across runs.
	for _, p := range n.declared {
		if _, still := newProps[p.Name]; !still {
			return t.rebuildStep(s, fmt.Sprintf(
				"%q was removed, and a property cannot be un-applied through a setter", p.Name))
		}
	}
	// A signal that appears for the first time has no emitter on the built
	// widget, and some widgets accept a callback only as a constructor option.
	for _, h := range sn.Handlers {
		if !n.wired[h.Signal] {
			return t.rebuildStep(s, fmt.Sprintf(
				"%q is a new signal, and a built widget cannot always be wired after construction", h.Signal))
		}
	}

	// Handlers are re-resolved during PLANNING so the mutating pass cannot fail
	// on an unknown host name. Rebinding is otherwise free: the emitter wired
	// into the widget calls Emit, which reads this map at call time, so
	// replacing its contents re-points a live widget with no toolkit work.
	//
	// Unchanged bindings are not re-resolved at all. Resolution is a lookup in
	// the host's table and reaching into the adapter for an answer we already
	// have would make "an unchanged file changes nothing" false — quietly, in
	// the one place a reader would not think to check.
	if !sameBindings(n.handlers, sn.Handlers) {
		s.rebind = true
		s.handlers = make(map[string][]boundHandler, len(sn.Handlers))
		for _, h := range sn.Handlers {
			bh, err := t.compileHandler(oldID, sn.Type, h)
			if err != nil {
				return nil, err
			}
			s.handlers[h.Signal] = append(s.handlers[h.Signal], bh)
		}
	}

	// Bindings are evaluated during PLANNING, so a failing value function or an
	// unknown source leaves the tree untouched and unlatched.
	//
	// An UNCHANGED binding is not re-evaluated: a reload is about the file, and
	// nothing in the file changed for it. Its current value is whatever the last
	// application left, which SetSource keeps current independently. Evaluating
	// it anyway would run the host's function once per node per reload for no
	// reason, and "an unchanged file changes nothing" would be true only of what
	// reaches the adapter.
	bs, effective, err := t.rebindChanged(n, oldID, sn.Props, oldProps, newProps)
	if err != nil {
		return nil, err
	}
	s.bind, s.effective = bs, effective

	// Only what actually changed is applied. A value that is the same is not
	// re-set, because a setter is not required to be idempotent: one of the
	// library's own assigns and invalidates unconditionally, so a "free" replay
	// would be a real repaint.
	//
	// Comparison is on the DECLARATIONS, so a binding whose expression is
	// unchanged does not re-fire even though its evaluation ran.
	for i, p := range sn.Props {
		if sameSequence(oldProps[p.Name], newProps[p.Name]) {
			continue
		}
		s.apply = append(s.apply, effective[i])
	}

	matched, dropped := t.matchChildren(n, sn)
	s.dropped = dropped
	s.restructure = len(dropped) > 0
	for i, m := range matched {
		if m == NoNode {
			f, err := t.fresh(sn.Children[i])
			if err != nil {
				return nil, err
			}
			s.order = append(s.order, f)
			s.restructure = true
			continue
		}
		cs, err := t.assess(m, sn.Children[i])
		if err != nil {
			return nil, err
		}
		if cs.rebuild {
			// The child is replaced by a different node, which is a change to
			// this node's child list.
			s.restructure = true
		}
		if i >= len(n.children) || n.children[i] != m {
			s.restructure = true
		}
		s.order = append(s.order, cs)
	}

	if s.restructure && !t.canRestructure(oldID) {
		return t.rebuildStep(s, "its children changed and this node cannot be restructured after construction")
	}
	return s, nil
}

// classifier returns how to judge a property, preferring the adapter's own
// answer and falling back to what Create reported consuming.
//
// The fallback is per-node and cannot see a property that was absent at
// construction, which is why it is a fallback: an adapter that implements
// [Classifier] answers from its tables instead.
func (t *Tree) classifier(n *node) func(typeName, prop string) PropertyKind {
	if c, ok := t.adapter.(Classifier); ok {
		return c.ClassifyProperty
	}
	return func(_, prop string) PropertyKind {
		if n != nil && n.consumed[prop] {
			return PropConstructorOnly
		}
		return PropRuntime
	}
}

// checkKind refuses a property the adapter cannot accept, and refuses a kind
// the engine does not recognise.
//
// An unrecognised numeric kind is rejected rather than allowed to fall through
// as runtime-settable. A default that treats an unknown answer as permission is
// how a future fourth kind would silently become "apply it and hope"; refusing
// makes adding one a compile-and-test problem instead of a field report.
func checkKind(k PropertyKind, typeName string, p qml.SpecProp, node NodeID) error {
	switch k {
	case PropRuntime, PropConstructorOnly:
		return nil
	case PropUnknown:
		return SchemaError{Op: "apply", Node: node, Detail: p.Name, Pos: p.Value.Pos,
			Err: fmt.Errorf("%w: type %q has no property %q", ErrAdapter, typeName, p.Name)}
	default:
		return SchemaError{Op: "apply", Node: node, Detail: p.Name, Pos: p.Value.Pos,
			Err: fmt.Errorf("%w: the adapter classified %q on type %q as %d, which this engine does not recognise",
				ErrAdapter, p.Name, typeName, uint8(k))}
	}
}

// rebindChanged evaluates only the bindings whose DECLARATION changed, and
// carries the rest forward with the value they already hold.
func (t *Tree) rebindChanged(n *node, id NodeID, props []qml.SpecProp,
	oldProps, newProps map[string][]qml.SpecValue) ([]*binding, []qml.SpecProp, error) {

	effective := make([]qml.SpecProp, len(props))
	copy(effective, props)
	var out []*binding

	for i, p := range props {
		if !needsResolution(p.Value) {
			continue
		}
		if prev, ok := t.bindingFor(id, p.Name); ok && t.isBinding(p.Value) &&
			sameSequence(oldProps[p.Name], newProps[p.Name]) {
			// Unchanged: keep the registration, and with it the applied-value
			// cache that keeps the next source tick quiet.
			out = append(out, prev)
			if prev.cached {
				effective[i].Value = prev.applied
			}
			continue
		}
		res, err := t.evalValue(ctxBinding, p.Value, id, nil)
		if err != nil {
			return nil, nil, SchemaError{Op: "bind", Node: id, Detail: p.Name, Pos: p.Value.Pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
		effective[i].Value = res.value
		if !res.tracked {
			continue
		}
		out = append(out, &binding{
			node: id, prop: p.Name, expr: p.Value, pos: p.Value.Pos, deps: res.deps,
		})
	}
	return out, effective, nil
}

// rebind replaces a node's binding registrations when its declarations changed,
// and leaves them alone when they did not.
//
// Leaving them alone is what makes "an unchanged file changes nothing" true for
// bindings: a re-registered binding would lose its applied-value cache and the
// next source tick would reach the setter with a value the widget already has.
func (t *Tree) rebind(s *step) error {
	if sameDeclarations(t.nodes[s.old].declared, s.spec.Props) {
		return nil
	}
	// s.bind carries the unchanged registrations THROUGH, so re-installing the
	// set does not reset a binding that kept its declaration — and with it, the
	// cache that keeps the next tick quiet.
	t.dropBindings(s.old)
	t.registerBindings(s.bind)
	return nil
}

// sameDeclarations reports whether two property lists were WRITTEN the same.
func sameDeclarations(a, b []qml.SpecProp) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || !sameValue(a[i].Value, b[i].Value) {
			return false
		}
	}
	return true
}

// matchChildren pairs the node's current children with the new schema's, by the
// precedence in [Tree.Reconcile]. The result is parallel to sn.Children, with
// NoNode where a new child has no counterpart; dropped holds the leftovers.
func (t *Tree) matchChildren(n *node, sn *qml.SpecNode) (matched []NodeID, dropped []NodeID) {
	matched = make([]NodeID, len(sn.Children))
	used := make(map[NodeID]bool, len(n.children))

	// 1. A DECLARED id wins outright, wherever it moved to.
	byID := map[string]NodeID{}
	for _, c := range n.children {
		if cn := t.nodes[c]; cn != nil && cn.schemaID != "" {
			// First declaration wins if a schema repeated an id; matching the
			// last would make the pairing depend on iteration order.
			if _, dup := byID[cn.schemaID]; !dup {
				byID[cn.schemaID] = c
			}
		}
	}
	for i, child := range sn.Children {
		if child.ID == "" {
			continue
		}
		if old, ok := byID[child.ID]; ok && !used[old] {
			matched[i] = old
			used[old] = true
		}
	}

	// 2. Position among the remaining children, excluding old nodes that were
	// NAMED.
	//
	// The exclusion is one-sided on purpose, because the two directions are not
	// the same claim. A node the author named is not handed to whatever happens
	// to sit in its position now: it already had an identity, and reusing it as
	// something the file no longer calls by that name would carry state across
	// two different things. But a NEW node declaring an id may adopt an
	// anonymous old one — the old node claimed no identity, so nothing is
	// contradicted, and the alternative would reset a node's state for the
	// ordinary edit of giving it a name.
	var rest []NodeID
	for _, c := range n.children {
		if used[c] {
			continue
		}
		if cn := t.nodes[c]; cn != nil && cn.schemaID != "" {
			continue
		}
		rest = append(rest, c)
	}
	next := 0
	for i := range sn.Children {
		if matched[i] != NoNode {
			continue
		}
		if next < len(rest) {
			matched[i] = rest[next]
			used[rest[next]] = true
			next++
		}
	}
	for _, c := range n.children {
		if !used[c] {
			dropped = append(dropped, c)
		}
	}
	return matched, dropped
}

// canRestructure asks the adapter whether a node's children may change. An
// adapter with no such capability answers no for everything, which turns every
// structural edit into a rebuild — lossy, but never wrong.
func (t *Tree) canRestructure(id NodeID) bool {
	r, ok := t.adapter.(Restructurer)
	return ok && r.CanRestructure(id)
}

// patch executes the plan for a node that KEEPS its identity.
//
// Rebuilds are not handled here. A rebuilt node has to be detached from its
// parent before it is released — the adapter forgets a component in Destroy, so
// detaching afterwards would have nothing left to detach — and only the parent
// can detach it. [Tree.patchChildren] therefore owns every rebuild except the
// root's, which has no parent and is handled by [Tree.Reconcile].
func (t *Tree) patch(s *step, parent NodeID, res *Result) (NodeID, error) {
	if s.rebuild {
		return NoNode, SchemaError{Op: "reconcile", Node: s.old, Pos: s.spec.Pos,
			Err: fmt.Errorf("%w: a rebuild reached patch, which cannot detach it from its parent", ErrPhase)}
	}

	n := t.nodes[s.old]
	if s.rebind {
		n.handlers = s.handlers
		if n.handlers == nil {
			n.handlers = map[string][]boundHandler{}
		}
	}
	// Changed bindings are re-extracted and evaluated during PLANNING (assess
	// -> s.apply carries the new declarations), so an evaluation failure has
	// already been reported with the tree intact. An UNCHANGED binding keeps
	// its registration and its cache, so a reload does not re-fire it.
	if err := t.rebind(s); err != nil {
		return NoNode, err
	}
	for _, p := range s.apply {
		origin := FromSchema
		if _, bound := t.bindingFor(s.old, p.Name); bound {
			origin = FromBinding
		}
		app := Application{Node: s.old, Prop: p.Name, Value: p.Value, Origin: origin}
		// Marked BEFORE the call: a setter that fails part-way has still
		// changed something, and assuming otherwise is how a partial tree gets
		// declared clean.
		t.mutated = true
		if err := t.adapter.Apply(app); err != nil {
			return NoNode, SchemaError{Op: "apply", Node: s.old, Detail: p.Name, Pos: p.Value.Pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
		res.Applied++
		t.noteApplied(s.old, p.Name, p.Value)
	}
	n.props = s.effective
	n.declared = s.spec.Props
	n.pos = s.spec.Pos
	// The node now reflects the NEW schema, including the name it is known by.
	// Leaving this stale costs nothing until the NEXT reload, which would look
	// for the node under a name the tree no longer records and rebuild
	// something it was holding perfectly well.
	n.schemaID = s.spec.ID

	want, err := t.patchChildren(s, res)
	if err != nil {
		return NoNode, err
	}
	n.children = want
	return s.old, nil
}

// patchChildren settles one node's child list and returns it in its new order.
//
// The sequence is fixed by what the adapter can still see at each point:
//
//  1. DETACH everything that is leaving, while its component still exists;
//  2. RELEASE those subtrees, which is when the adapter forgets them;
//  3. build or patch each slot, so every child has its final identity;
//  4. ATTACH the new arrivals at their final index;
//  5. MOVE the survivors into place — the step that keeps their state.
//
// Doing 2 before 1 is the tempting order and it is wrong: Destroy forgets the
// component, so the detach that follows has nothing to work with.
func (t *Tree) patchChildren(s *step, res *Result) ([]NodeID, error) {
	n := t.nodes[s.old]

	// Departing children are those the new schema drops outright, plus those a
	// rebuild replaces with a different node.
	leaving := make(map[NodeID]bool, len(s.dropped))
	for _, id := range s.dropped {
		leaving[id] = true
	}
	for _, cs := range s.order {
		if cs.rebuild && cs.old != NoNode {
			leaving[cs.old] = true
		}
	}

	var r Restructurer
	if s.restructure {
		var ok bool
		if r, ok = t.adapter.(Restructurer); !ok {
			// assess only sets restructure on a node that cleared
			// canRestructure, which requires this capability.
			return nil, SchemaError{Op: "reconcile", Node: s.old, Pos: s.spec.Pos,
				Err: fmt.Errorf("%w: the adapter stopped being a Restructurer mid-reconcile", ErrAdapter)}
		}
	}

	cur := append([]NodeID(nil), n.children...)
	want := make([]NodeID, len(s.order))

	// 0. CONSTRUCT EVERY REPLACEMENT FIRST, before anything is released.
	//
	// Classification proves a property EXISTS; only the constructor can say
	// whether it accepts this VALUE, and only by being called. Building first
	// means a refusal costs nothing: the live children are still attached, and
	// the half-built replacements are discarded.
	// Accounting is BATCH-LOCAL until the whole batch stands. Recording each
	// success into the Result as it happens is accurate right up until a later
	// sibling refuses, at which point every replacement is discarded and the
	// Result still names constructions that were thrown away. A Result is a
	// report of what HAPPENED; a discarded build did not happen.
	//
	// An earlier version fixed exactly this for a single node and left it for
	// the batch, which is the same defect one scope out.
	var fresh []NodeID
	var batchCreated int
	var batchRebuilt []Rebuild
	for i, cs := range s.order {
		if !cs.rebuild {
			continue
		}
		id, err := t.mountNode(cs.spec, s.old)
		if err != nil {
			fresh = append(fresh, id)
			return nil, t.discardReplacements(err, fresh...)
		}
		fresh = append(fresh, id)
		batchCreated += t.countBuilt(id)
		want[i] = id
		if cs.old != NoNode {
			batchRebuilt = append(batchRebuilt, cs.rebuildRecord())
		}
	}
	res.Created += batchCreated
	res.Rebuilt = append(res.Rebuilt, batchRebuilt...)

	// 1 and 2, in the tree's own child order so the trace is deterministic.
	var departed []NodeID
	for _, id := range cur {
		if !leaving[id] {
			continue
		}
		t.mutated = true
		if err := r.RemoveChild(s.old, id); err != nil {
			return nil, SchemaError{Op: "reconcile", Node: id, Pos: s.spec.Pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
		departed = append(departed, id)
	}
	cur = filterIDs(cur, func(id NodeID) bool { return !leaving[id] })
	for _, id := range departed {
		if err := t.releaseSubtree(id, res); err != nil {
			return nil, err
		}
	}

	// 3. The children that KEPT their identity, which may have work of their own.
	for i, cs := range s.order {
		if cs.rebuild {
			continue
		}
		id, err := t.patch(cs, s.old, res)
		if err != nil {
			return nil, err
		}
		want[i] = id
	}

	if !s.restructure {
		return want, nil
	}

	// 4. Ascending order means every earlier arrival is already in place, so
	// the index a child asks for is the index it gets.
	present := make(map[NodeID]bool, len(cur))
	for _, id := range cur {
		present[id] = true
	}
	for i, id := range want {
		if present[id] {
			continue
		}
		at := i
		if at > len(cur) {
			at = len(cur)
		}
		t.mutated = true
		if err := r.InsertChild(s.old, id, at); err != nil {
			return nil, SchemaError{Op: "reconcile", Node: id, Pos: s.spec.Pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
		cur = insertID(cur, at, id)
		present[id] = true
	}

	// 5. The adapter's CURRENT order is tracked as we go rather than computed
	// from the plan, because every move shifts the positions of everything
	// after it: an index taken from the final order is wrong as soon as the
	// first one lands.
	for i := 0; i < len(want) && i < len(cur); i++ {
		if cur[i] == want[i] {
			continue
		}
		j := indexOfID(cur, want[i])
		if j < 0 {
			continue
		}
		t.mutated = true
		if err := r.MoveChild(s.old, want[i], i); err != nil {
			return nil, SchemaError{Op: "reconcile", Node: want[i], Pos: s.spec.Pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
		cur = insertID(removeAt(cur, j), i, want[i])
		res.Moved++
	}
	return want, nil
}

// discardReplacements releases subtrees that were built and then abandoned,
// and returns cause joined with every cleanup failure.
//
// The failures are KEPT rather than dropped. Adapter.Destroy is allowed to
// fail, and this is the one place where a failure has nowhere else to surface:
// the node is forgotten immediately afterwards, so no later Destroy can retry
// it or report it. Discarding the error here would leave a resource the adapter
// still holds, with no record anywhere that it was never released — which is
// exactly the silent-loss shape this package refused for handler errors.
//
// Every subtree is offered even if an earlier one refuses, because stopping at
// the first would strand the rest with no record either.
func (t *Tree) discardReplacements(cause error, ids ...NodeID) error {
	errs := []error{cause}
	var sink Result
	for _, id := range ids {
		if err := t.releaseSubtree(id, &sink); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// releaseSubtree destroys a node and everything under it, children first, and
// forgets them. Failures are joined rather than stopping the walk, so one
// stubborn node cannot strand the rest.
func (t *Tree) releaseSubtree(id NodeID, res *Result) error {
	var errs []error
	for _, n := range t.subtreePostOrder(id) {
		if nd := t.nodes[n]; nd != nil && nd.built {
			if err := t.adapter.Destroy(n); err != nil {
				errs = append(errs, SchemaError{Op: "destroy", Node: n,
					Err: fmt.Errorf("%w: %w", ErrAdapter, err)})
			}
			res.Destroyed++
		}
		// A node that goes drops its bindings with it; one that keeps its
		// identity keeps them.
		t.dropBindings(n)
		delete(t.nodes, n)
	}
	return errors.Join(errs...)
}

// subtreePostOrder lists a subtree with every child before its parent.
func (t *Tree) subtreePostOrder(id NodeID) []NodeID {
	var out []NodeID
	seen := map[NodeID]bool{}
	var visit func(NodeID)
	visit = func(cur NodeID) {
		if seen[cur] {
			return
		}
		seen[cur] = true
		if n := t.nodes[cur]; n != nil {
			for _, c := range n.children {
				visit(c)
			}
		}
		out = append(out, cur)
	}
	visit(id)
	return out
}

// countBuilt reports how many nodes of a freshly mounted subtree the adapter
// actually built, which is what "created" should mean after a partial failure.
func (t *Tree) countBuilt(id NodeID) int {
	n := 0
	for _, c := range t.subtreePostOrder(id) {
		if nd := t.nodes[c]; nd != nil && nd.built {
			n++
		}
	}
	return n
}

// propSequences groups a property list by name, keeping document order.
//
// Duplicates are kept as a sequence rather than collapsed to the last value,
// because [Tree.Mount] applies every declaration in order. Comparing only the
// effective value would call a schema unchanged when the number of times a
// setter runs had changed.
func propSequences(props []qml.SpecProp) map[string][]qml.SpecValue {
	out := make(map[string][]qml.SpecValue, len(props))
	for _, p := range props {
		out[p.Name] = append(out[p.Name], p.Value)
	}
	return out
}

// sameBindings reports whether a node's live bindings already are what the
// schema asks for: the same signals, carrying the same handler BODIES in the
// same run order.
func sameBindings(cur map[string][]boundHandler, want []qml.SpecHandler) bool {
	n := 0
	for _, hs := range cur {
		n += len(hs)
	}
	if n != len(want) {
		return false
	}
	// want is in document order and cur was appended per signal in that same
	// order, so walking want and consuming each signal's list in step compares
	// the two orders rather than just the two sets.
	seen := make(map[string]int, len(cur))
	for _, h := range want {
		hs := cur[h.Signal]
		i := seen[h.Signal]
		if i >= len(hs) || hs[i].key != handlerKey(h) {
			return false
		}
		seen[h.Signal] = i + 1
	}
	return true
}

func sameSequence(a, b []qml.SpecValue) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameValue(a[i], b[i]) {
			return false
		}
	}
	return true
}

// sameValue compares two values by what they MEAN, not by where they were
// written. Position is deliberately excluded: adding a line above a property
// moves it, and re-running every setter in a file because of that would make a
// reload lose state for an edit that changed nothing.
func sameValue(a, b qml.SpecValue) bool {
	if a.Kind != b.Kind || a.Raw != b.Raw || len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if !sameValue(a.Args[i], b.Args[i]) {
			return false
		}
	}
	return true
}

func filterIDs(ids []NodeID, keep func(NodeID) bool) []NodeID {
	out := ids[:0:0]
	for _, id := range ids {
		if keep(id) {
			out = append(out, id)
		}
	}
	return out
}

func insertID(ids []NodeID, at int, id NodeID) []NodeID {
	if at > len(ids) {
		at = len(ids)
	}
	out := make([]NodeID, 0, len(ids)+1)
	out = append(out, ids[:at]...)
	out = append(out, id)
	return append(out, ids[at:]...)
}

func removeAt(ids []NodeID, i int) []NodeID {
	out := make([]NodeID, 0, len(ids))
	out = append(out, ids[:i]...)
	return append(out, ids[i+1:]...)
}

func indexOfID(ids []NodeID, id NodeID) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}
