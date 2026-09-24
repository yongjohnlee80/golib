package decl

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/parse"
)

// Kind is what an injected name IS.
//
// The host declares it; the engine never infers it. Nothing in a Go value says
// whether it changes, whether a call is pure, or whether a name is a widget —
// the host knows, and saying so is what lets the engine answer a question it
// otherwise could not: may this name appear HERE?
type Kind uint8

const (
	// KindUnknown is the zero value and never names an injection.
	KindUnknown Kind = iota
	// KindConstant is a value that cannot change: `tui.Horizontal`. Resolved
	// once at planning and never tracked, so it costs the reactive graph
	// nothing.
	KindConstant
	// KindSource is a value that changes: `Theme.surface`. Tracked, and its
	// dependents recompute when it moves.
	KindSource
	// KindPureFunction derives a value from its arguments.
	//
	// It must be DETERMINISTIC and FREE OF SIDE EFFECTS, depending only on the
	// arguments it is given. That is a contract rather than a description: the
	// graph caches its result and re-runs it on retry, so a function with
	// hidden state would make the quiet cache lie and make a retry paint a
	// different screen from the first attempt.
	KindPureFunction
	// KindHandler performs an effect in response to a signal. An effectful
	// callable is a handler, not a function — that is the whole distinction.
	KindHandler
	// KindNamespace is the left of a member chain: `tui`, `Theme`. Never a
	// value in itself.
	KindNamespace
	// KindType is a node type a schema may instantiate: `Button`.
	KindType
)

// String renders the kind for diagnostics.
func (k Kind) String() string {
	switch k {
	case KindConstant:
		return "constant"
	case KindSource:
		return "source"
	case KindPureFunction:
		return "function"
	case KindHandler:
		return "handler"
	case KindNamespace:
		return "namespace"
	case KindType:
		return "type"
	default:
		return "unknown"
	}
}

// PureFunc derives a value from already-evaluated terminal arguments.
type PureFunc func(args []parse.SpecValue) (parse.SpecValue, error)

// HandlerFunc performs an effect. Its arguments are already evaluated.
type HandlerFunc func(args []parse.SpecValue) error

// Injected is one named thing the host has handed to the evaluator.
//
// It is a tagged union rather than an interface with six implementations,
// because the set is CLOSED by ADR and every consumer switches on the tag
// anyway. An interface would hide that switch without removing it.
type Injected struct {
	Kind Kind
	// Value carries a KindConstant, or a KindSource's initial value. It must be
	// terminal.
	Value parse.SpecValue
	// Pure is set for KindPureFunction.
	Pure PureFunc
	// Handle is set for KindHandler.
	Handle HandlerFunc
}

// Sentinels for the injection layer.
var (
	// ErrNotInjected reports a name nothing was injected under.
	ErrNotInjected = errors.New("decl: nothing was injected under this name")

	// ErrWrongKind reports an injected name used where its kind cannot appear —
	// a handler asked to produce a value, say.
	ErrWrongKind = errors.New("decl: this name cannot be used here")

	// ErrReservedName reports an attempt to bind `parent` or `root`.
	ErrReservedName = errors.New("decl: this name is reserved by the language")

	// ErrAmbiguousName reports two imports binding the same leaf.
	ErrAmbiguousName = errors.New("decl: this name is bound twice")
)

// reserved names are part of the language rather than slots a document may
// fill. An id or an injection that captured `parent` would silently change what
// every `parent.x` beneath it meant, which is a defect with no symptom.
var reserved = map[string]bool{"parent": true, "root": true}

// Constant is a convenience for the common injection.
func Constant(v parse.SpecValue) Injected { return Injected{Kind: KindConstant, Value: v} }

// SourceValue injects a trackable value with its initial contents.
func SourceValue(v parse.SpecValue) Injected { return Injected{Kind: KindSource, Value: v} }

// Pure injects a derivation. See [KindPureFunction] for the contract it carries.
func Pure(fn PureFunc) Injected { return Injected{Kind: KindPureFunction, Pure: fn} }

// Handle injects an effect.
func Handle(fn HandlerFunc) Injected { return Injected{Kind: KindHandler, Handle: fn} }

// Namespace injects a name that only ever appears to the left of a dot.
func Namespace() Injected { return Injected{Kind: KindNamespace} }

// Inject registers a typed name, before Mount.
//
// **Injection is authorisation.** A host decides what a schema can reach by
// choosing what to hand over; there is no second gate asking whether it really
// meant to. What injection does not do is erase types — a handler is still
// refused where a value belongs, which is [Tree.Resolve]'s business.
//
// A dotted name registers a qualified entry: `tui.Horizontal`. Its prefix is
// implicitly a namespace, so `tui` alone resolves and `tui.Nope` does not.
func (t *Tree) Inject(name string, in Injected) error { return t.inject("inject", name, in) }

// inject is the ONE write path into the typed registry. DeclareSource and
// DeclareFunc route through it under their own op names, so a name cannot be a
// source in one registry and a function in another — a disagreement that would
// make the resolver's answer depend on which map it happened to consult.
func (t *Tree) inject(op, name string, in Injected) error {
	if t.ph != phaseIdle || t.root != NoNode {
		return SchemaError{Op: op, Detail: name, Err: fmt.Errorf(
			"%w: injection happens before Mount, so the set a schema is checked "+
				"against is fixed when planning begins", ErrPhase)}
	}
	if in.Kind == KindUnknown {
		return SchemaError{Op: op, Detail: name,
			Err: fmt.Errorf("%w: no kind was declared", ErrWrongKind)}
	}
	if name == "" {
		return SchemaError{Op: op, Detail: name,
			Err: fmt.Errorf("%w: a name is required", ErrWrongKind)}
	}
	segs := strings.Split(name, ".")
	for _, s := range segs {
		if s == "" {
			return SchemaError{Op: op, Detail: name, Err: fmt.Errorf(
				"%w: %q has an empty segment, which no reference can ever name",
				ErrWrongKind, name)}
		}
		// EVERY segment is checked, not only the first: `Theme.parent` would
		// otherwise bind a member that shadows nothing today and silently
		// changes meaning the moment scoping grows a qualified `parent`.
		if reserved[s] {
			return SchemaError{Op: op, Detail: name,
				Err: fmt.Errorf("%w: %q", ErrReservedName, s)}
		}
	}
	switch in.Kind {
	case KindConstant, KindSource:
		if !isTerminal(in.Value) {
			return SchemaError{Op: op, Detail: name, Pos: in.Value.Pos, Err: fmt.Errorf(
				"%w: a %s holds a %s, not an expression", ErrNotTerminal, in.Kind, in.Value.Kind)}
		}
	case KindPureFunction:
		if in.Pure == nil {
			return SchemaError{Op: op, Detail: name, Err: fmt.Errorf(
				"%w: a function with no implementation is refused here rather than "+
					"discovered at the first call", ErrWrongKind)}
		}
	case KindHandler:
		if in.Handle == nil {
			return SchemaError{Op: op, Detail: name, Err: fmt.Errorf(
				"%w: a handler with no implementation is refused here rather than "+
					"discovered at the first signal", ErrWrongKind)}
		}
	}
	// A name the adapter already defines is refused rather than shadowed. The
	// two scopes have different lifetimes — Destroy forgets injections and not
	// the adapter — so a shadowing name would mean one thing before a Destroy
	// and another after, with no diagnostic either time.
	if _, clash := t.consts[name]; clash {
		return SchemaError{Op: op, Detail: name, Err: fmt.Errorf(
			"%w: the adapter already defines %q, and shadowing it would change what "+
				"a schema means without saying so", ErrAmbiguousName, name)}
	}
	if t.injected == nil {
		t.injected = map[string]Injected{}
	}
	if prior, dup := t.injected[name]; dup {
		// A namespace implied by a dotted name is not a declaration, so
		// injecting `tui` explicitly after `tui.Horizontal` is not a clash.
		if !(prior.Kind == KindNamespace && in.Kind == KindNamespace) {
			return SchemaError{Op: op, Detail: name, Err: fmt.Errorf(
				"%w: %q is already injected as a %s", ErrDuplicateDecl, name, prior.Kind)}
		}
		return nil
	}
	t.injected[name] = in

	// Every prefix of a dotted name is a namespace, so `tui` resolves as one
	// without the host having to inject it separately — and `tui.Nope` fails as
	// an unknown member of a known namespace rather than as an unbound name,
	// which is a better thing to be told.
	for i := 1; i < len(segs); i++ {
		prefix := strings.Join(segs[:i], ".")
		if prior, exists := t.injected[prefix]; exists {
			if prior.Kind != KindNamespace {
				return SchemaError{Op: op, Detail: name, Err: fmt.Errorf(
					"%w: %q is a %s, so %q cannot name something inside it",
					ErrWrongKind, prefix, prior.Kind, name)}
			}
			continue
		}
		t.injected[prefix] = Injected{Kind: KindNamespace}
	}

	// The live stores are seeded from the same call, because the CURRENT value
	// of a source is not the injected one — propagation moves it — and the
	// registry must stay the authority on TYPE without also claiming to be the
	// authority on value.
	switch in.Kind {
	case KindSource:
		if t.sources == nil {
			t.sources = map[string]parse.SpecValue{}
		}
		t.sources[name] = in.Value
	case KindPureFunction:
		if t.funcs == nil {
			t.funcs = map[string]ValueFunc{}
		}
		t.funcs[name] = ValueFunc(in.Pure)
	}
	return nil
}

// Lookup reports what was injected under a name.
func (t *Tree) Lookup(name string) (Injected, bool) {
	in, ok := t.injected[name]
	return in, ok
}

// context is where a value sits, which is half of what decides whether a name
// may appear there.
type context uint8

const (
	// ctxBinding is a property value.
	ctxBinding context = iota
	// ctxHandler is a signal body.
	ctxHandler
)

func (c context) String() string {
	if c == ctxHandler {
		return "a signal handler"
	}
	return "a property binding"
}

// allowedIn reports whether an injected kind may be the RESOLVED LEAF of a
// value in this context, and says why not when it may not.
//
// This is the leaf rule of the resolution judgement, not the whole of it: the
// structural walk in evalValue validates every child before this is consulted
// for any of them.
func allowedIn(ctx context, k Kind, called bool) (bool, string) {
	switch k {
	case KindConstant, KindSource:
		if called {
			return false, fmt.Sprintf("a %s is a value and cannot be called", k)
		}
		if ctx == ctxHandler {
			return false, fmt.Sprintf("a %s is a value, and a signal wants an effect", k)
		}
		return true, ""

	case KindPureFunction:
		if !called {
			return false, "a function must be called to produce a value"
		}
		if ctx == ctxHandler {
			return false, "a function produces a value, and a signal wants an effect"
		}
		return true, ""

	case KindHandler:
		if ctx == ctxBinding {
			return false, "a handler performs an effect and cannot produce a value"
		}
		if !called {
			return false, "a handler is invoked by calling it: write name() rather than name"
		}
		return true, ""

	case KindNamespace:
		return false, "a namespace is not a value; name something inside it"

	case KindType:
		return false, "a type is instantiated as a node, not used as a value"

	default:
		return false, "this name has no usable kind"
	}
}
