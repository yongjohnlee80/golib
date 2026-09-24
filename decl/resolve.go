package decl

import (
	"errors"
	"fmt"
	"sort"

	"github.com/yongjohnlee80/golib/parse"
)

// resolution is what a structural walk produces for one declared value.
type resolution struct {
	// value is the terminal the adapter will receive.
	value parse.SpecValue
	// deps are the source names this value reads, deduped and sorted.
	deps []string
	// tracked reports whether the value can change — whether any source was
	// reached. A value with no sources is a constant expression however many
	// functions it went through.
	tracked bool
}

// evalValue walks a declared value, validating every node and collecting every
// dependency, and returns the terminal it evaluates to.
//
// THE RECURSION IS THE JUDGEMENT; the injected-kind table is only its leaf
// rule. An earlier design said operators "resolve to themselves", which quietly
// exempted array elements, object values, template substitutions and grouped
// sub-expressions — so a source reachable only inside an array element, or a
// bad name inside a template, would have been missed entirely.
//
// Every child is validated BEFORE any function runs, so a schema mistake is
// never discovered half way through a host call that has already had an effect.
func (t *Tree) evalValue(ctx context, v parse.SpecValue, at NodeID,
	overlay map[string]parse.SpecValue) (resolution, error) {

	out, err := t.walkValue(ctx, v, at, overlay, false)
	if err != nil {
		return resolution{}, err
	}
	names := t.sourcesOf(v)
	return resolution{value: out, deps: names, tracked: len(names) > 0}, nil
}

// sourcesOf is the ONE answer to "which sources does this value read".
//
// It is separate from the evaluating walk and shared with [Tree.isBinding],
// because the alternative is two recursions computing the same property — and
// two instruments agreeing proves nothing while two disagreeing is a defect
// with no symptom until a source ticks and the wrong thing repaints.
//
// A name nothing was injected under contributes nothing: this is a predicate,
// and refusing it is the evaluating walk's business.
func (t *Tree) sourcesOf(v parse.SpecValue) []string {
	into := map[string]bool{}
	t.collectSources(v, into)
	if len(into) == 0 {
		return nil
	}
	names := make([]string, 0, len(into))
	for d := range into {
		names = append(names, d)
	}
	sort.Strings(names)
	return names
}

func (t *Tree) collectSources(v parse.SpecValue, into map[string]bool) {
	switch v.Kind {
	case parse.SpecValueRef:
		// The INJECTED KIND decides this, not the shape of the name. An earlier
		// version tracked exactly the single-segment names, which silently made
		// `Theme.surface` un-trackable: a palette change would have updated the
		// source and repainted nothing.
		if in, ok := t.lookupName(v.Raw); ok && in.Kind == KindSource {
			into[v.Raw] = true
		}
	case parse.SpecValueCall:
		for _, a := range v.Args {
			t.collectSources(a, into)
		}
	}
}

// isBinding reports whether a declared value must be re-evaluated when a source
// moves. A value that reads no source is a literal however it was written.
func (t *Tree) isBinding(v parse.SpecValue) bool {
	into := map[string]bool{}
	t.collectSources(v, into)
	return len(into) > 0
}

// walkValue is the recursive half. called reports whether this node sits in
// callee position, which is what separates `save` from `save()`.
func (t *Tree) walkValue(ctx context, v parse.SpecValue, at NodeID,
	overlay map[string]parse.SpecValue, called bool) (parse.SpecValue, error) {

	switch v.Kind {
	case parse.SpecValueString, parse.SpecValueNumber, parse.SpecValueBool, parse.SpecValueToken:
		if called {
			return parse.SpecValue{}, t.refuse(at, v, "a literal cannot be called")
		}
		return v, nil

	case parse.SpecValueRef:
		return t.walkRef(ctx, v, at, overlay, called)

	case parse.SpecValueCall:
		// The CALLEE is resolved first and in callee position, so a handler in
		// a binding is refused before any argument is touched.
		callee := parse.SpecValue{Kind: parse.SpecValueRef, Raw: v.Raw,
			Path: splitDots(v.Raw), Pos: v.Pos}
		in, err := t.lookupRef(callee, at)
		if err != nil {
			return parse.SpecValue{}, err
		}
		if ok, why := allowedIn(ctx, in.Kind, true); !ok {
			return parse.SpecValue{}, t.refuse(at, v, why)
		}

		// EVERY argument is validated and its dependencies collected before the
		// function runs. Arguments are values wherever the call sits: inside a
		// handler's parentheses the context is a binding, which is what makes
		// `submit(count)` work without a handler evaluator.
		args := make([]parse.SpecValue, 0, len(v.Args))
		for _, a := range v.Args {
			ev, err := t.walkValue(ctxBinding, a, at, overlay, false)
			if err != nil {
				return parse.SpecValue{}, err
			}
			if !isTerminal(ev) {
				return parse.SpecValue{}, t.refuse(at, a, fmt.Sprintf(
					"an argument must be a value, and this is a %s", ev.Kind))
			}
			args = append(args, ev)
		}

		if in.Kind == KindHandler {
			// A handler is COMPILED, not evaluated: the terminal it "produces"
			// is never used, and the invocation happens when the signal fires.
			return parse.SpecValue{}, nil
		}
		res, err := in.Pure(args)
		if err != nil {
			// The host's own error, NOT a misuse of its name. Wrapping it in the
			// kind sentinel made a failing function read as a schema mistake and
			// hid the error the host actually reported.
			return parse.SpecValue{}, fmt.Errorf("%q (at %s): %w", v.Raw, v.Pos, err)
		}
		if !isTerminal(res) {
			return parse.SpecValue{}, SchemaError{Op: "resolve", Node: at, Detail: v.Raw, Pos: v.Pos,
				Err: fmt.Errorf("%w: %q returned a %s; a function returns a value, not an expression",
					ErrNotTerminal, v.Raw, res.Kind)}
		}
		res.Pos = v.Pos
		return res, nil

	case parse.SpecValueExpr:
		// The parser reads every JavaScript expression QML allows; this engine
		// evaluates the subset above. Saying which is true — the document is
		// correct and this evaluator is the limit — rather than reporting a
		// syntax error in source that has none.
		return parse.SpecValue{}, SchemaError{Op: "resolve", Node: at, Detail: v.Raw, Pos: v.Pos,
			Err: fmt.Errorf("%w: %s", ErrExpressionValue, describeExpr(v.Expr))}

	default:
		return parse.SpecValue{}, t.refuse(at, v, fmt.Sprintf("a %s cannot appear here", v.Kind))
	}
}

// ErrExpressionValue reports a property value this engine does not evaluate.
//
// It is separate from every other refusal because the remedy is different and
// the author has done nothing wrong: the document is valid QML, and what is
// missing is evaluator capability. A consumer can tell "rewrite this" from
// "this is not supported yet" only if the two carry different sentinels.
var ErrExpressionValue = errors.New("decl: this engine does not evaluate that expression")

func describeExpr(e *parse.Expr) string {
	if e == nil {
		return "an expression"
	}
	switch e.Kind {
	case parse.ExprBinary, parse.ExprLogical:
		return fmt.Sprintf("the operator %q; bind a source or call a function instead", e.Raw)
	case parse.ExprConditional:
		return "a conditional; call a function that decides instead"
	default:
		return fmt.Sprintf("a %s expression", e.Kind)
	}
}

// walkRef resolves a name or a member chain to an injected leaf.
func (t *Tree) walkRef(ctx context, v parse.SpecValue, at NodeID,
	overlay map[string]parse.SpecValue, called bool) (parse.SpecValue, error) {

	in, err := t.lookupRef(v, at)
	if err != nil {
		return parse.SpecValue{}, err
	}
	if ok, why := allowedIn(ctx, in.Kind, called); !ok {
		return parse.SpecValue{}, t.refuse(at, v, why)
	}
	switch in.Kind {
	case KindConstant:
		c := in.Value
		c.Pos = v.Pos
		return c, nil
	case KindSource:
		if sv, ok := overlay[v.Raw]; ok {
			sv.Pos = v.Pos
			return sv, nil
		}
		// The LIVE value, not the injected one: propagation moves a source, and
		// reading the registry here would resolve every binding against the value
		// the source had at startup.
		sv, ok := t.sources[v.Raw]
		if !ok {
			sv = in.Value
		}
		sv.Pos = v.Pos
		return sv, nil
	default:
		// Unreachable while allowedIn covers every kind, and deliberately worded
		// so it cannot be mistaken for that gate's refusal: two guards with the
		// same message let a test pass while the gate it names is switched off.
		return parse.SpecValue{}, t.refuse(at, v, fmt.Sprintf(
			"a %s reached value resolution, which the kind gate should have refused", in.Kind))
	}
}

// lookupRef resolves a dotted name, reporting the three failures separately
// because a reader needs a different thing from each.
func (t *Tree) lookupRef(v parse.SpecValue, at NodeID) (Injected, error) {
	path := v.Path
	if len(path) == 0 {
		path = splitDots(v.Raw)
	}
	name := v.Raw

	// The gate is on the NAME THE DOCUMENT WROTE, not on the module behind it.
	// Keying it on the module made an alias additive: `import tui 1.0 as T`
	// left `tui.Vertical` resolving, because the module WAS imported — under
	// another name. QML's alias REPLACES the spelling, and a document that
	// mounts here but not in a real QML runtime is the worst kind of
	// compatibility.
	if len(path) > 1 {
		if mod, bound := t.imported[path[0]]; bound {
			// An import binds this name; resolve through the module it names,
			// which for an unaliased import is the same string.
			path = append(splitDots(mod), path[1:]...)
			name = joinDots(path)
		} else if mod, isModule := t.moduleOf(path); isModule {
			return Injected{}, t.fail(at, v, ErrNotImported, fmt.Sprintf(
				"%q comes from the %q module; add `import %s` to use it", v.Raw, mod, mod))
		}
	}

	if in, ok := t.lookupName(name); ok {
		return in, nil
	}
	if len(path) > 1 {
		// A known namespace with an unknown member is a better diagnostic than
		// "unbound name": it tells the reader the import worked and the member
		// is the mistake.
		prefix := joinDots(path[:len(path)-1])
		if in, ok := t.lookupName(prefix); ok && in.Kind == KindNamespace {
			// The module resolved and the MEMBER did not. Distinct from an
			// unknown module because the reader's next move differs: check the
			// spelling of the member, not the import.
			return Injected{}, t.fail(at, v, ErrNotResolvable, fmt.Sprintf(
				"%q has no member %q", prefix, path[len(path)-1]))
		}
		return Injected{}, t.fail(at, v, ErrNotInjected, fmt.Sprintf(
			"nothing named %q was injected, so no import brings %q into scope", path[0], v.Raw))
	}
	return Injected{}, t.fail(at, v, ErrNotInjected, fmt.Sprintf("unbound name %q", v.Raw))
}

// lookupName resolves a name across the two scopes, in precedence order.
//
// The host's registry comes first and the adapter's fixed vocabulary second.
// They are separate because their LIFETIMES differ — Destroy forgets the host's
// injections and does not forget what the adapter is — but a name in both would
// mean one thing before a Destroy and another after, so [Tree.inject] refuses
// the collision outright rather than leaving this precedence to decide it.
func (t *Tree) lookupName(name string) (Injected, bool) {
	if in, ok := t.injected[name]; ok {
		return in, true
	}
	if c, ok := t.consts[name]; ok {
		return Injected{Kind: KindConstant, Value: c}, true
	}
	// A prefix of an adapter constant is a namespace for the same reason an
	// injected one is: `tui.Nope` should say the member is wrong, not the module.
	for qualified := range t.consts {
		if len(qualified) > len(name) && qualified[:len(name)] == name &&
			qualified[len(name)] == '.' {
			return Injected{Kind: KindNamespace}, true
		}
	}
	return Injected{}, false
}

// refuse reports a name used where its KIND cannot appear.
//
// The three resolution failures carry three sentinels, because a consumer has a
// different response to each and one shared sentinel would force it to match on
// message text: [ErrNotInjected] for a name nothing was injected under,
// [ErrNotResolvable] for a known module's unknown member, and [ErrWrongKind]
// here — the name resolved, and this position will not take what it is.
func (t *Tree) refuse(node NodeID, v parse.SpecValue, why string) error {
	return t.fail(node, v, ErrWrongKind, why)
}

func (t *Tree) fail(node NodeID, v parse.SpecValue, sentinel error, why string) error {
	return SchemaError{Op: "resolve", Node: node, Detail: v.Raw, Pos: v.Pos,
		Err: fmt.Errorf("%w: %s", sentinel, why)}
}

func splitDots(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func joinDots(p []string) string {
	s := ""
	for i, seg := range p {
		if i > 0 {
			s += "."
		}
		s += seg
	}
	return s
}
