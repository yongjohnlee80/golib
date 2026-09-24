package decl

import (
	"fmt"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// SetSource updates a declared source and propagates the change.
//
// The value must be terminal. Updating an undeclared name is refused: the
// declared set is fixed before Mount so that a schema's references can be
// checked against something.
//
// # What happens, and in what order
//
// A value equal to the current one does NOTHING — no evaluation, no
// application, an empty result. That is the common case and it costs one
// comparison.
//
// Otherwise every binding that reads this source is recomputed in DOCUMENT
// ORDER, the only order a schema author can see. A binding whose derived value
// is unchanged reaches no setter at all: the engine owns that comparison,
// because the widgets disagree about it — Button.SetLabel and Text.SetText
// self-guard, Box.SetTitle and Box.SetStatus assign and invalidate
// unconditionally.
//
// # Failure
//
// Propagation is NOT atomic, and the result says how far it got. If an
// evaluation or an application fails, later bindings do not run and earlier
// applications STAY APPLIED — the adapter's setters are not reversible.
//
// But the source itself is only committed once the whole fan-out succeeds. A
// failure leaves its committed value OLD, so retrying the same value is a
// CHANGED-source attempt: the bindings that already succeeded recompute to
// values they already have and go quiet, and the one that failed is called
// again. Committing the source on failure would make that retry take the
// "unchanged, do nothing" path, and the failure would become permanent in
// silence.
//
// The tree is not latched by a failure here. Nothing was created, destroyed or
// restructured, and the retry above depends on the host being able to call
// again.
func (t *Tree) SetSource(name string, v qml.SpecValue) (PropagationResult, error) {
	return t.SetSources(map[string]qml.SpecValue{name: v})
}

// SetSources updates SEVERAL sources as ONE propagation.
//
// This is not a convenience wrapper around a loop, and doing it as a loop is
// the bug it exists to prevent. Updating a palette one name at a time paints a
// MIXED PALETTE: after the first call the screen is half the old theme and half
// the new one, and every binding that reads two of those names is evaluated
// against a combination that never existed in the host.
//
// One overlay, one fan-out, one commit: every binding sees all the new values
// or none of them, and a binding reading two changed names recomputes ONCE
// rather than twice.
//
// Ordering and failure behave exactly as [SetSource] describes, because they
// are the same code — that is a single-name update with a map of one.
func (t *Tree) SetSources(values map[string]qml.SpecValue) (PropagationResult, error) {
	res, err := t.setSources(values)
	return res, t.settle(err)
}

func (t *Tree) setSources(values map[string]qml.SpecValue) (PropagationResult, error) {
	detail := joinSorted(values)
	if err := t.propagationAllowed("set source", detail); err != nil {
		return PropagationResult{}, err
	}

	// EVERY name is validated before ANY is applied. A batch that checked as it
	// went would apply the good half of a bad update, which is the mixed state
	// this call exists to make impossible.
	overlay := make(map[string]qml.SpecValue, len(values))
	for name, v := range values {
		cur, ok := t.sources[name]
		if !ok {
			return PropagationResult{}, SchemaError{Op: "set source", Detail: name,
				Err: fmt.Errorf("%w: %q was never declared", ErrNoSuchSource, name)}
		}
		if !isTerminal(v) {
			return PropagationResult{}, SchemaError{Op: "set source", Detail: name, Pos: v.Pos,
				Err: fmt.Errorf("%w: a source holds a %s, not an expression", ErrNotTerminal, v.Kind)}
		}
		if sameValue(cur, v) {
			// Unchanged names are dropped from the overlay rather than the
			// batch: a theme switch where one colour happens to be the same
			// must still be one propagation.
			continue
		}
		overlay[name] = v
	}
	if len(overlay) == 0 {
		return PropagationResult{}, nil
	}

	prev := t.ph
	t.ph = phasePropagating
	defer func() { t.ph = prev }()

	var res PropagationResult
	for _, b := range t.bindingsOfAny(overlay) {
		ev, err := t.evalValue(ctxBinding, b.expr, b.node, overlay)
		if err != nil {
			return res, SchemaError{Op: "propagate", Node: b.node, Detail: b.prop, Pos: b.pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
		derived := ev.value
		res.Recomputed++

		if b.cached && sameValue(b.applied, derived) {
			res.Quiet++
			continue
		}
		app := Application{Node: b.node, Prop: b.prop, Value: derived, Origin: FromBinding}
		if err := t.adapter.Apply(app); err != nil {
			return res, SchemaError{Op: "apply", Node: b.node, Detail: b.prop, Pos: b.pos,
				Err: fmt.Errorf("%w: %w", ErrAdapter, err)}
		}
		// Only now. A cache advanced on a failed Apply would make the retry
		// look unchanged, and the failure would never reach the setter again.
		b.applied, b.cached = derived, true
		res.Applied++
	}

	// Committed only now, and all together: a partially committed batch would
	// leave the host's palette split across two propagations with no way back.
	for name, v := range overlay {
		t.sources[name] = v
	}
	return res, nil
}

// joinSorted renders a batch's names for a diagnostic, deterministically.
func joinSorted(values map[string]qml.SpecValue) string {
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	sortStrings(names)
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}

// propagationAllowed enforces the phase matrix.
//
// Evaluation and the whole fan-out run under one phase, so nothing can mutate
// the tree BETWEEN two entries of a fan-out — which would leave half a
// propagation applied against a tree the other half no longer describes.
//
// SetSource from an ordinary signal handler is LEGAL, and is the case this
// feature exists for: a button's handler updates a source and the screen
// follows. What is refused is the reverse — a value function, or an adapter
// that calls back synchronously from Apply, starting a second propagation while
// one is running.
func (t *Tree) propagationAllowed(op, detail string) error {
	switch t.ph {
	case phasePropagating:
		return SchemaError{Op: op, Detail: detail, Err: fmt.Errorf(
			"%w: a propagation is already running; a value function or an Apply callback "+
				"cannot start another", ErrPhase)}
	case phaseMounting, phaseReconciling, phaseDestroying:
		return SchemaError{Op: op, Detail: detail, Err: fmt.Errorf("%w: %s", ErrPhase, t.ph)}
	default:
		// idle, or nested inside an emission — both legal.
		return nil
	}
}

// bindingsOfAny returns every binding reading ANY of these sources, in DOCUMENT
// ORDER and WITHOUT REPEATS.
//
// The de-duplication is the point: a binding reading two names that both
// changed must recompute once. Running it twice would apply an intermediate
// value to a real setter — visible on screen as a flicker through a state the
// host never asked for.
func (t *Tree) bindingsOfAny(sources map[string]qml.SpecValue) []*binding {
	var out []*binding
	for _, b := range t.bindings {
		for _, d := range b.deps {
			if _, ok := sources[d]; ok {
				out = append(out, b)
				break
			}
		}
	}
	return out
}

// bindingsOf returns the bindings reading a source, in DOCUMENT ORDER.
//
// Document order is the only order a schema author can see, which makes it the
// only defensible one when a fan-out stops part-way.
func (t *Tree) bindingsOf(source string) []*binding {
	var out []*binding
	for _, b := range t.bindings {
		for _, d := range b.deps {
			if d == source {
				out = append(out, b)
				break
			}
		}
	}
	return out
}

// registerBindings records a node's bindings, preserving document order across
// the whole tree.
func (t *Tree) registerBindings(bs []*binding) {
	t.bindings = append(t.bindings, bs...)
}

// dropBindings forgets every binding belonging to a node. A node that is
// rebuilt drops its bindings with it; one that keeps its identity keeps them.
func (t *Tree) dropBindings(node NodeID) {
	if len(t.bindings) == 0 {
		return
	}
	out := t.bindings[:0]
	for _, b := range t.bindings {
		if b.node != node {
			out = append(out, b)
		}
	}
	t.bindings = out
}

// bindingFor returns a node's binding for a property, if it has one.
func (t *Tree) bindingFor(node NodeID, prop string) (*binding, bool) {
	for _, b := range t.bindings {
		if b.node == node && b.prop == prop {
			return b, true
		}
	}
	return nil, false
}

// noteApplied records that a value reached the adapter, so a later recompute to
// the same value can stay quiet.
//
// Create-consumed properties come through here too: a builder that claimed a
// bound property leaves no Apply to succeed, so the terminal handed to
// Construction IS the applied value and seeds the cache there instead.
func (t *Tree) noteApplied(node NodeID, prop string, v qml.SpecValue) {
	if b, ok := t.bindingFor(node, prop); ok {
		b.applied, b.cached = v, true
	}
}
