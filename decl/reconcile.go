package decl

import (
	"errors"
	"fmt"

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
	spec, err := parse.QML{}.Parse(src)
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
// The work is planned in full BEFORE anything is mutated: every handler is
// resolved and every restructure is cleared with the adapter first, so the
// common failures happen while the tree is still untouched. What cannot be
// pre-checked is the adapter's own setters, since the only way to learn that a
// setter refuses a value is to call it. A failure there leaves the tree
// PARTIALLY reconciled and latches it, exactly as a failed [Tree.Mount] does:
// the next Mount or Reconcile is refused until [Tree.Destroy] has cleared it.
// Pretending otherwise would mean claiming the adapter's setters are
// reversible, and they are not.
func (t *Tree) Reconcile(spec parse.SpecTree) (Result, error) {
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

	t.ph = phaseReconciling
	defer func() { t.ph = phaseIdle }()

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
		// whole tree: release it and mount the new schema from scratch.
		res.Rebuilt = append(res.Rebuilt, plan.rebuildRecord())
		res.RootReplaced = true
		if err := t.releaseSubtree(t.root, &res); err != nil {
			t.failed = true
			return res, err
		}
		t.root = NoNode
		id, err := t.mountNode(spec.Root, NoNode)
		res.Created += t.countBuilt(id)
		if err != nil {
			t.failed = true
			return res, err
		}
		t.root = id
		return res, nil
	}

	if _, err := t.patch(plan, NoNode, &res); err != nil {
		t.failed = true
		return res, err
	}
	return res, nil
}

// step is one node's plan: either patch it in place, or rebuild it.
type step struct {
	// old is the node this slot currently holds, or NoNode for a fresh subtree.
	old NodeID
	// spec is what the new schema says this slot should be.
	spec *parse.SpecNode

	// rebuild marks a slot that cannot be patched.
	rebuild bool
	// reason names the edit that forced the rebuild, for diagnostics.
	reason string

	// apply is the properties to set, in document order. Empty when nothing
	// changed, which is the common case and the point of reconciling.
	apply []parse.SpecProp
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

// fresh returns the plan for a slot with nothing in it yet.
func fresh(sn *parse.SpecNode) *step {
	return &step{old: NoNode, spec: sn, rebuild: true, reason: "the schema adds this node"}
}

// assess plans one matched pair, children first. It reads the tree and the
// adapter's read-only capabilities; it changes nothing.
//
// Children are planned before their parent because a child that must be rebuilt
// changes what the PARENT needs: swapping a child out is a structural edit, and
// a parent that cannot restructure must therefore be rebuilt too. Deciding the
// parent first would mean deciding it on incomplete information.
func (t *Tree) assess(oldID NodeID, sn *parse.SpecNode) (*step, error) {
	n := t.nodes[oldID]
	if n == nil {
		return nil, SchemaError{Op: "reconcile", Node: oldID, Pos: sn.Pos, Err: ErrNoSuchNode}
	}
	s := &step{old: oldID, spec: sn}

	if n.typeName != sn.Type {
		s.rebuild = true
		s.reason = fmt.Sprintf("the type changed from %s to %s", n.typeName, sn.Type)
		return s, nil
	}

	// A property the adapter CONSUMED at construction is one it told us has no
	// setter. Changing or deleting it has no path through Apply, so the only
	// honest response is to build the node again.
	oldProps := propSequences(n.props)
	newProps := propSequences(sn.Props)
	for name := range n.consumed {
		if !sameSequence(oldProps[name], newProps[name]) {
			s.rebuild = true
			s.reason = fmt.Sprintf(
				"%q was taken at construction and has no setter, so changing it cannot be applied", name)
			return s, nil
		}
	}
	// A property that disappears from the schema cannot be un-applied either:
	// there is no "unset" in the seam, and the engine holds no default to
	// restore. Rebuilding is the only way to make the screen match the file.
	for name := range oldProps {
		if _, still := newProps[name]; !still {
			s.rebuild = true
			s.reason = fmt.Sprintf(
				"%q was removed, and a property cannot be un-applied through a setter", name)
			return s, nil
		}
	}
	// A signal that appears for the first time has no emitter on the built
	// widget, and some widgets accept a callback only as a constructor option.
	for _, h := range sn.Handlers {
		if !n.wired[h.Signal] {
			s.rebuild = true
			s.reason = fmt.Sprintf(
				"%q is a new signal, and a built widget cannot always be wired after construction", h.Signal)
			return s, nil
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
			fn, err := t.adapter.ResolveHandler(oldID, h.Signal, h.Name, h.Pos)
			if err != nil {
				return nil, SchemaError{Op: "bind", Node: oldID, Detail: h.Signal + " -> " + h.Name,
					Pos: h.Pos, Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
			}
			if fn == nil {
				return nil, SchemaError{Op: "bind", Node: oldID, Detail: h.Signal + " -> " + h.Name,
					Pos: h.Pos, Err: fmt.Errorf("%w: resolved to a nil function", ErrAdapter)}
			}
			s.handlers[h.Signal] = append(s.handlers[h.Signal], boundHandler{name: h.Name, pos: h.Pos, fn: fn})
		}
	}

	// Only what actually changed is applied. A value that is the same is not
	// re-set, because a setter is not required to be idempotent: one of the
	// library's own assigns and invalidates unconditionally, so a "free" replay
	// would be a real repaint.
	for _, p := range sn.Props {
		if n.consumed[p.Name] {
			continue
		}
		if sameSequence(oldProps[p.Name], newProps[p.Name]) {
			continue
		}
		s.apply = append(s.apply, p)
	}

	matched, dropped := t.matchChildren(n, sn)
	s.dropped = dropped
	s.restructure = len(dropped) > 0
	for i, m := range matched {
		if m == NoNode {
			s.order = append(s.order, fresh(sn.Children[i]))
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
		s.rebuild = true
		s.reason = "its children changed and this node cannot be restructured after construction"
		s.order, s.dropped, s.apply, s.handlers = nil, nil, nil, nil
	}
	return s, nil
}

// matchChildren pairs the node's current children with the new schema's, by the
// precedence in [Tree.Reconcile]. The result is parallel to sn.Children, with
// NoNode where a new child has no counterpart; dropped holds the leftovers.
func (t *Tree) matchChildren(n *node, sn *parse.SpecNode) (matched []NodeID, dropped []NodeID) {
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
	for _, p := range s.apply {
		app := Application{Node: s.old, Prop: p.Name, Value: p.Value, Origin: FromSchema}
		if err := t.adapter.Apply(app); err != nil {
			return NoNode, SchemaError{Op: "apply", Node: s.old, Detail: p.Name, Pos: p.Value.Pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
		res.Applied++
	}
	n.props = s.spec.Props
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
			res.Rebuilt = append(res.Rebuilt, cs.rebuildRecord())
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

	// 1 and 2, in the tree's own child order so the trace is deterministic.
	var departed []NodeID
	for _, id := range cur {
		if !leaving[id] {
			continue
		}
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

	// 3.
	want := make([]NodeID, len(s.order))
	for i, cs := range s.order {
		if cs.rebuild {
			id, err := t.mountNode(cs.spec, s.old)
			res.Created += t.countBuilt(id)
			if err != nil {
				return nil, err
			}
			want[i] = id
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
		if err := r.MoveChild(s.old, want[i], i); err != nil {
			return nil, SchemaError{Op: "reconcile", Node: want[i], Pos: s.spec.Pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
		cur = insertID(removeAt(cur, j), i, want[i])
		res.Moved++
	}
	return want, nil
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
func propSequences(props []parse.SpecProp) map[string][]parse.SpecValue {
	out := make(map[string][]parse.SpecValue, len(props))
	for _, p := range props {
		out[p.Name] = append(out[p.Name], p.Value)
	}
	return out
}

// sameBindings reports whether a node's live bindings already are what the
// schema asks for: the same signals, carrying the same handler names in the
// same run order.
func sameBindings(cur map[string][]boundHandler, want []parse.SpecHandler) bool {
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
		if i >= len(hs) || hs[i].name != h.Name {
			return false
		}
		seen[h.Signal] = i + 1
	}
	return true
}

func sameSequence(a, b []parse.SpecValue) bool {
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
func sameValue(a, b parse.SpecValue) bool {
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
