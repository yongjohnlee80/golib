package decl

import (
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/parse"
)

// ValueFunc derives a value from arguments that are ALREADY EVALUATED terminals.
//
// It receives no [Tree], and that is deliberate rather than an omission: a
// function with no handle on the engine cannot start a second propagation
// through the seam at all. The phase refusal in [Tree.SetSource] catches a
// function that obtained a Tree some other way, but the seam should not be the
// only thing standing between a host and a nested propagation.
//
// The result must be terminal. A function returns a value, not an expression.
type ValueFunc func(args []parse.SpecValue) (parse.SpecValue, error)

// Sentinel errors for the reactive layer.
var (
	// ErrNoSuchSource reports a Ref, or a SetSource, naming a source that was
	// never declared.
	ErrNoSuchSource = errors.New("decl: no such source")

	// ErrNoSuchFunc reports a Call naming a function that was never declared.
	ErrNoSuchFunc = errors.New("decl: no such value function")

	// ErrNotResolvable reports a reference this engine cannot resolve — today,
	// a member chain. It is separate from ErrNoSuchSource because the answer
	// differs: one is a name that could be declared, the other is a SHAPE this
	// engine does not evaluate.
	ErrNotResolvable = errors.New("decl: reference cannot be resolved")

	// ErrNotTerminal reports an expression where a value belongs — a source set
	// to a Ref, or a function returning one.
	ErrNotTerminal = errors.New("decl: value is not terminal")

	// ErrDuplicateDecl reports a source or function declared twice.
	ErrDuplicateDecl = errors.New("decl: already declared")

	// ErrBindingUnsupported reports a schema that binds against an adapter with
	// no [Classifier]. Literal-only schemas are unaffected.
	ErrBindingUnsupported = errors.New("decl: this adapter cannot classify properties, so it cannot take bindings")

	// ErrDuplicateBinding reports a property declared more than once where at
	// least one declaration is a binding.
	ErrDuplicateBinding = errors.New("decl: a property declared more than once cannot also be bound")
)

// PropagationResult summarises one source update.
//
// The counts are PINNED ON FAILURE rather than zeroed: propagation is not
// atomic, and a host that cannot see how far it got cannot tell "nothing needed
// doing" from "three things changed and then one failed".
type PropagationResult struct {
	// Recomputed counts bindings whose evaluation ran to completion. A binding
	// whose evaluation failed is NOT counted, because it did not produce a value.
	Recomputed int
	// Applied counts the values that actually reached a setter.
	Applied int
	// Quiet counts bindings that recomputed to a value they already had, so no
	// setter was called. This is the number that should be large.
	Quiet int
}

// isTerminal reports whether v is a value rather than an expression.
//
// Terminal kinds are the ones that mean something without evaluation. Ref and
// Call are the two that do not, and letting either be a SOURCE value is what
// would make sources depend on sources — the thing that keeps this graph
// bipartite is that they cannot.
func isTerminal(v parse.SpecValue) bool {
	switch v.Kind {
	case parse.SpecValueString, parse.SpecValueNumber,
		parse.SpecValueBool, parse.SpecValueToken:
		return true
	default:
		return false
	}
}

// Constants is an OPTIONAL capability an [Adapter] may implement to expose its
// own vocabulary as qualified names.
//
// QML writes an enum as `Qt.Horizontal`; the toolkit equivalent is
// `tui.Horizontal`. These are CONSTANTS, not bindings: a qualified name is
// resolved once at planning and never changes, so it creates no dependency and
// no entry in the graph. That is the whole reason a qualified name can be
// supported now while a reference to another object's PROPERTY cannot.
type Constants interface {
	// Constants returns the qualified names this adapter defines, keyed by the
	// full dotted spelling, with terminal values.
	Constants() map[string]parse.SpecValue
}

// needsResolution reports whether a declared value has to be resolved before
// the adapter can see it.
//
// It is deliberately wider than [isBinding]: a qualified name is resolved but
// NOT tracked. Conflating the two is how `tui.Vertical` once reached a builder
// as an unresolved reference — the value was correctly judged "not a binding"
// and therefore never evaluated at all.
func needsResolution(v parse.SpecValue) bool {
	switch v.Kind {
	case parse.SpecValueRef, parse.SpecValueCall, parse.SpecValueExpr:
		// SpecValueExpr is here so it is REFUSED rather than forwarded. A kind
		// this list forgets is not rejected — it sails past resolution and
		// reaches a setter as an un-evaluated tree, which is how `tui.Vertical`
		// once arrived at a builder as a bare reference.
		return true
	default:
		return false
	}
}

// Tracking is decided by [Tree.isBinding] in resolve.go, from the INJECTED KIND
// rather than the shape of a name. The predicate lived here as a pure function
// keyed on path length, which made `Theme.surface` permanently un-trackable:
// qualified names were assumed constant, so a palette source would have updated
// and repainted nothing. Its replacement shares one recursion with the
// dependency collector, so the two cannot disagree about what a value reads.

// binding is one bound property: what was written, and what was last applied.
type binding struct {
	node NodeID
	prop string
	// expr is the ORIGINAL expression. The evaluated terminal is what gets
	// applied; this is what gets re-evaluated when a source changes.
	expr parse.SpecValue
	pos  parse.Position
	// deps are the source names this binding reads, deduped.
	deps []string
	// applied is the last value that reached the adapter, and cached is whether
	// one ever did. The engine owns this comparison because the widgets do not
	// agree about it: Button.SetLabel and Text.SetText self-guard, while
	// Box.SetTitle and Box.SetStatus assign and invalidate unconditionally.
	applied parse.SpecValue
	cached  bool
}

// DeclareSource registers a named source and its initial value, before Mount.
//
// Declaration is separate from update because a planning-time refusal of an
// unknown Ref needs a known set to check against. With SetSource alone,
// "unknown" would only mean "not pushed yet", and the check could not be
// trusted.
//
// The value must be terminal: an unrestricted expression is not a runtime value.
//
// It is [Tree.Inject] with [SourceValue], kept because it reads better at a call
// site that only wants a source. Both write the same registry, so a name
// declared here cannot be injected again as something else.
func (t *Tree) DeclareSource(name string, v parse.SpecValue) error {
	_, err := t.inject("declare source", name, SourceValue(v))
	return err
}

// DeclareFunc registers a value function, before Mount.
//
// It is [Tree.Inject] with [Pure]; see [KindPureFunction] for the determinism
// contract a value function carries.
func (t *Tree) DeclareFunc(name string, fn ValueFunc) error {
	if fn == nil {
		return SchemaError{Op: "declare func", Detail: name, Err: fmt.Errorf(
			"%w: a nil function is refused here rather than discovered at the first call", ErrAdapter)}
	}
	_, err := t.inject("declare func", name, Pure(PureFunc(fn)))
	return err
}

// Source reports a declared source's current value.
func (t *Tree) Source(name string) (parse.SpecValue, bool) {
	v, ok := t.sources[name]
	return v, ok
}

// checkBindable validates a node's declared properties before anything is built.
//
// It is called during planning for every node — mounted fresh, rebuilt, or
// reconciled — so a schema mistake is found while the tree is still intact.
func (t *Tree) checkBindable(typeName string, props []parse.SpecProp, node NodeID) error {
	var bound bool
	for _, p := range props {
		if t.isBinding(p.Value) {
			bound = true
			break
		}
	}
	if !bound {
		return nil
	}

	// R11: the consumed-set fallback cannot answer for a node that does not
	// exist yet, so without a Classifier a binding on a constructor-only
	// property would pass planning, Create would mutate, and only then would
	// Apply fail. Literal-only schemas keep the fallback untouched.
	classifier, ok := t.adapter.(Classifier)
	if !ok {
		for _, p := range props {
			if t.isBinding(p.Value) {
				return SchemaError{Op: "bind", Node: node, Detail: p.Name, Pos: p.Value.Pos,
					Err: fmt.Errorf("%w: %q on type %q", ErrBindingUnsupported, p.Name, typeName)}
			}
		}
	}

	// R12: a property declared more than once, where any declaration is a
	// binding, would give one target two writers and two caches — and a source
	// tick would revive the earlier declaration over a later literal. Duplicate
	// LITERALS stay legal, document order, last wins.
	seen := map[string]int{}
	for _, p := range props {
		seen[p.Name]++
	}
	for _, p := range props {
		if seen[p.Name] > 1 && t.isBinding(p.Value) {
			return SchemaError{Op: "bind", Node: node, Detail: p.Name, Pos: p.Value.Pos,
				Err: fmt.Errorf("%w: %q is declared %d times on type %q",
					ErrDuplicateBinding, p.Name, seen[p.Name], typeName)}
		}
	}

	// R11 again: a binding needs a setter, and a constructor-only property has
	// none. Binding one would mean rebuilding the node on every source tick —
	// structural work that destroys exactly the scroll offset, focus and
	// in-flight tasks a reconcile exists to preserve.
	for _, p := range props {
		if !t.isBinding(p.Value) {
			continue
		}
		switch classifier.ClassifyProperty(typeName, p.Name) {
		case PropRuntime:
		case PropConstructorOnly:
			return SchemaError{Op: "bind", Node: node, Detail: p.Name, Pos: p.Value.Pos,
				Err: fmt.Errorf("%w: %q is taken at construction and has no setter, so it cannot be "+
					"bound — a source change could only be applied by rebuilding the node", ErrAdapter, p.Name)}
		default:
			return SchemaError{Op: "bind", Node: node, Detail: p.Name, Pos: p.Value.Pos,
				Err: fmt.Errorf("%w: type %q has no property %q", ErrAdapter, typeName, p.Name)}
		}
	}
	return nil
}

// preEvaluate validates and evaluates every binding in a schema, depth-first,
// without allocating a single node.
//
// It is the Mount-level form of "plan before you mutate": a binding failure at
// the bottom of a file must leave the tree exactly as it was, not with the
// nodes above it allocated and the tree latched.
func (t *Tree) preEvaluate(sn *parse.SpecNode) error {
	if err := t.checkBindable(sn.Type, sn.Props, NoNode); err != nil {
		return err
	}
	_, effective, err := t.bindingsFor(NoNode, sn.Props)
	if err != nil {
		return err
	}
	t.preEval[sn] = effective
	for _, child := range sn.Children {
		if err := t.preEvaluate(child); err != nil {
			return err
		}
	}
	return nil
}

// bindingsOn builds a node's binding registrations. The values were already
// evaluated; this records what to re-evaluate later and what it depends on.
func (t *Tree) bindingsOn(node NodeID, props []parse.SpecProp) ([]*binding, error) {
	var out []*binding
	for _, p := range props {
		names := t.sourcesOf(p.Value)
		if len(names) == 0 {
			continue
		}
		out = append(out, &binding{
			node: node, prop: p.Name, expr: p.Value, pos: p.Value.Pos, deps: names,
		})
	}
	return out, nil
}

// bindingsFor turns a node's declared properties into registrations, evaluating
// each against the current sources.
//
// The ORIGINAL expression is kept: the terminal is what gets applied, the
// expression is what gets re-evaluated when a source changes.
func (t *Tree) bindingsFor(node NodeID, props []parse.SpecProp) ([]*binding, []parse.SpecProp, error) {
	var out []*binding
	effective := make([]parse.SpecProp, len(props))
	copy(effective, props)

	for i, p := range props {
		if !needsResolution(p.Value) {
			continue
		}
		res, err := t.evalValue(ctxBinding, p.Value, node, nil)
		if err != nil {
			return nil, nil, SchemaError{Op: "bind", Node: node, Detail: p.Name, Pos: p.Value.Pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
		// The adapter receives the TERMINAL, never the expression. That is what
		// keeps a builder and a setter from needing to know bindings exist.
		effective[i].Value = res.value

		if !res.tracked {
			continue // a constant: resolved once, never tracked
		}
		out = append(out, &binding{
			node: node, prop: p.Name, expr: p.Value, pos: p.Value.Pos, deps: res.deps,
		})
	}
	return out, effective, nil
}
