package decl

import (
	"errors"
	"fmt"
	"sort"

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

// isBinding reports whether a declared value has to be evaluated.
//
// A REF IS A BINDING ONLY IF IT NAMES A DECLARED SOURCE. That is not a
// convenience; it is what `SpecValueRef` already means. Its own documentation
// says "a bare identifier: a single reference, RESOLVED BY THE ADAPTER", and the
// shipped registry depends on it: `orientation: horizontal` parses as a Ref
// whose Raw is "horizontal", and the Split builder switches on that to pick an
// enum. Treating every Ref as a source reference would silently reinterpret
// every enum-valued property in every existing schema.
//
// So the declared-source set is the disambiguator, and it is decidable at
// planning time precisely because that set is fixed before Mount. A Ref naming
// no source passes through to the adapter untouched, exactly as today — and the
// adapter already refuses the ones it does not understand.
//
// A Call is unambiguous: `SpecValueCall` is documented as "a call into the host
// function registry", and nothing else consumes it.
func (t *Tree) isBinding(v parse.SpecValue) bool {
	switch v.Kind {
	case parse.SpecValueRef:
		_, declared := t.sources[v.Raw]
		return declared
	case parse.SpecValueCall:
		return true
	default:
		return false
	}
}

// refsOf collects the source names a value depends on, DEDUPED, by walking the
// expression. A value cannot acquire a dependency at runtime that was not
// written in the file, so this static walk is the whole dependency set.
func (t *Tree) refsOf(v parse.SpecValue, into map[string]bool) {
	switch v.Kind {
	case parse.SpecValueRef:
		if _, declared := t.sources[v.Raw]; declared {
			into[v.Raw] = true
		}
	case parse.SpecValueCall:
		for _, a := range v.Args {
			t.refsOf(a, into)
		}
	}
}

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
func (t *Tree) DeclareSource(name string, v parse.SpecValue) error {
	if t.ph != phaseIdle {
		return SchemaError{Op: "declare source", Detail: name,
			Err: fmt.Errorf("%w: %s", ErrPhase, t.ph)}
	}
	if t.root != NoNode {
		return SchemaError{Op: "declare source", Detail: name, Err: fmt.Errorf(
			"%w: sources are declared before Mount, so the set a schema is checked against is fixed", ErrPhase)}
	}
	if _, dup := t.sources[name]; dup {
		return SchemaError{Op: "declare source", Detail: name, Err: ErrDuplicateDecl}
	}
	if !isTerminal(v) {
		return SchemaError{Op: "declare source", Detail: name, Pos: v.Pos,
			Err: fmt.Errorf("%w: a source holds a %s, not an expression", ErrNotTerminal, v.Kind)}
	}
	if t.sources == nil {
		t.sources = map[string]parse.SpecValue{}
	}
	t.sources[name] = v
	return nil
}

// DeclareFunc registers a value function, before Mount.
func (t *Tree) DeclareFunc(name string, fn ValueFunc) error {
	if t.ph != phaseIdle {
		return SchemaError{Op: "declare func", Detail: name,
			Err: fmt.Errorf("%w: %s", ErrPhase, t.ph)}
	}
	if t.root != NoNode {
		return SchemaError{Op: "declare func", Detail: name, Err: fmt.Errorf(
			"%w: value functions are declared before Mount, so a schema's calls can be checked", ErrPhase)}
	}
	if fn == nil {
		return SchemaError{Op: "declare func", Detail: name, Err: fmt.Errorf(
			"%w: a nil function is refused here rather than discovered at the first call", ErrAdapter)}
	}
	if _, dup := t.funcs[name]; dup {
		return SchemaError{Op: "declare func", Detail: name, Err: ErrDuplicateDecl}
	}
	if t.funcs == nil {
		t.funcs = map[string]ValueFunc{}
	}
	t.funcs[name] = fn
	return nil
}

// Source reports a declared source's current value.
func (t *Tree) Source(name string) (parse.SpecValue, bool) {
	v, ok := t.sources[name]
	return v, ok
}

// evaluate reduces an expression to a terminal value.
//
// Arguments evaluate LEFT TO RIGHT, depth-first, so a nested call is already a
// terminal by the time the outer function sees it. That is the only depth in
// this design: nesting deepens one binding's evaluation, never the graph.
//
// overlay lets a propagation evaluate against a TENTATIVE source value without
// committing it — the source is only committed once the whole fan-out succeeds.
func (t *Tree) evaluate(v parse.SpecValue, overlay map[string]parse.SpecValue) (parse.SpecValue, error) {
	switch v.Kind {
	case parse.SpecValueRef:
		if sv, ok := overlay[v.Raw]; ok {
			return sv, nil
		}
		if sv, ok := t.sources[v.Raw]; ok {
			return sv, nil
		}
		// Not a source: an adapter-resolved identifier, which is what a bare
		// word has always been. It passes through untouched.
		return v, nil

	case parse.SpecValueCall:
		fn, ok := t.funcs[v.Raw]
		if !ok {
			return parse.SpecValue{}, fmt.Errorf("%w: %q (at %s)", ErrNoSuchFunc, v.Raw, v.Pos)
		}
		args := make([]parse.SpecValue, 0, len(v.Args))
		for _, a := range v.Args {
			ev, err := t.evaluate(a, overlay)
			if err != nil {
				return parse.SpecValue{}, err
			}
			args = append(args, ev)
		}
		out, err := fn(args)
		if err != nil {
			return parse.SpecValue{}, fmt.Errorf("%q (at %s): %w", v.Raw, v.Pos, err)
		}
		if !isTerminal(out) {
			return parse.SpecValue{}, fmt.Errorf("%w: %q returned a %s (at %s)",
				ErrNotTerminal, v.Raw, out.Kind, v.Pos)
		}
		// The result carries the CALL's position, not the function's idea of
		// one: a diagnostic should name the line someone wrote.
		out.Pos = v.Pos
		return out, nil

	default:
		return v, nil
	}
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
		if !t.isBinding(p.Value) {
			continue
		}
		v, err := t.evaluate(p.Value, nil)
		if err != nil {
			return nil, nil, SchemaError{Op: "bind", Node: node, Detail: p.Name, Pos: p.Value.Pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
		deps := map[string]bool{}
		t.refsOf(p.Value, deps)
		names := make([]string, 0, len(deps))
		for d := range deps {
			names = append(names, d)
		}
		// Deterministic, so a trace is assertable.
		sort.Strings(names)

		out = append(out, &binding{
			node: node, prop: p.Name, expr: p.Value, pos: p.Value.Pos, deps: names,
		})
		// The adapter receives the TERMINAL, never the expression. That is what
		// keeps a builder and a setter from needing to know bindings exist.
		effective[i].Value = v
	}
	return out, effective, nil
}
