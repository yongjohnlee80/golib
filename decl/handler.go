package decl

import (
	"fmt"
	"github.com/yongjohnlee80/golib/parse/js"
	"github.com/yongjohnlee80/golib/parse/qml"
	"strings"

	"github.com/yongjohnlee80/golib/parse"
)

// ErrHandlerBody reports a handler body this evaluator does not run.
//
// It is NOT a syntax error and deliberately not reported as one: the source is
// valid QML, the parser read it correctly, and this engine declines to execute
// it. Saying so precisely is the difference between "you wrote this wrong" and
// "this engine does not do that yet", and only the second is true.
var ErrHandlerBody = fmt.Errorf("decl: this engine does not run that handler body")

// compileHandler turns one parsed handler into something that can be invoked.
//
// The accepted shape is deliberately narrow: a sequence of CALLS to injected
// handlers, whose arguments are ordinary values. That is a property of this
// EVALUATOR, not of the language — the parser reads the whole of QML's
// JavaScript, and widening what runs here means accepting more statement kinds
// below, not reparsing anything.
//
// Compilation happens during PLANNING, before any node is built. A handler that
// names something unresolvable is therefore found while the tree is still
// intact, which is what stopped a typo in a fresh subtree from destroying the
// working screen it was replacing.
func (t *Tree) compileHandler(node NodeID, typeName string, h qml.SpecHandler) (boundHandler, error) {
	if len(h.Body) == 0 {
		return boundHandler{}, SchemaError{Op: "bind", Node: node, Detail: h.Signal, Pos: h.Pos,
			Err: fmt.Errorf("%w: the handler is empty", ErrHandlerBody)}
	}

	type invocation struct {
		fn HandlerFunc
		// argExprs are the arguments AS WRITTEN, re-evaluated every time the
		// signal fires. They are not the values they had at mount: a handler
		// reading a source must see what the source holds WHEN THE BUTTON IS
		// PRESSED, and an earlier version baked the mount-time values into the
		// closure — so `submit(count)` submitted the count the screen had when
		// it was built, forever.
		argExprs []qml.SpecValue
		// params maps an argument's position to the signal parameter it
		// passes: `App.openFile(selectedFile)`.
		params map[int]int
		// reads maps an argument's position to a read of an object's
		// property by id: `App.login(user.text)` (property_read.go).
		reads map[int]func() (qml.SpecValue, error)
		name  string
	}
	var calls []invocation
	params := t.signalParams(typeName, h.Signal)

	for _, st := range h.Body {
		if st.Kind != js.StmtExpr || st.Value == nil {
			return boundHandler{}, t.refuseBody(node, h, st.Pos, fmt.Sprintf(
				"a %s statement; this engine runs calls to injected handlers", st.Kind))
		}
		e := st.Value
		if e.Kind != js.ExprCall {
			// The distinction the parser keeps and an earlier design erased:
			// `onClicked: save` is the IDENTIFIER save, not a call to it. Saying
			// so is more useful than silently invoking what the author named.
			if e.Kind == js.ExprIdent {
				return boundHandler{}, t.refuseBody(node, h, e.Pos, fmt.Sprintf(
					"the name %q on its own; a handler is invoked by calling it, so write %s()",
					e.Raw, e.Raw))
			}
			return boundHandler{}, t.refuseBody(node, h, e.Pos, fmt.Sprintf(
				"a %s expression; this engine runs calls to injected handlers", e.Kind))
		}

		name, ok := calleeName(e.Left)
		if !ok {
			return boundHandler{}, t.refuseBody(node, h, e.Pos,
				"a call to something that is not a name")
		}
		ref := qml.SpecValue{Kind: qml.SpecValueRef, Raw: name,
			Path: splitDots(name), Pos: e.Pos}
		// A call through a document id — `quitDialog.open()` — is a METHOD of
		// that node, not an injected handler.
		fn, isMethod, err := t.methodCall(node, name, ref)
		if err != nil {
			return boundHandler{}, err
		}
		if !isMethod {
			in, _, err := t.lookupRef(ref, node)
			if err != nil {
				return boundHandler{}, err
			}
			if ok, why := allowedIn(ctxHandler, in.Kind, true); !ok {
				return boundHandler{}, t.refuse(node, ref, why)
			}
			fn = in.Handle
		}

		// Arguments are VALUES wherever the call sits, so they resolve in the
		// binding context — which is what lets `submit(count)` read a source
		// without this evaluator having to run JavaScript at all.
		//
		// They are VALIDATED here and EVALUATED when the signal fires. Both
		// halves matter: validating now means a typo is found while the tree is
		// still intact, and evaluating later means the handler sees the value
		// the source holds at the moment it runs.
		argExprs := make([]qml.SpecValue, 0, len(e.Args))
		var passed map[int]int
		var reads map[int]func() (qml.SpecValue, error)
		for i := range e.Args {
			av, err := t.argExpr(node, h, &e.Args[i])
			if err != nil {
				return boundHandler{}, err
			}
			// A SIGNAL PARAMETER, named as the signal declares it. It shadows
			// an injected name of the same spelling, as a parameter does in
			// QML: the handler is the innermost scope.
			if at, ok := paramIndex(params, av); ok {
				if passed == nil {
					passed = map[int]int{}
				}
				passed[i] = at
				argExprs = append(argExprs, av)
				continue
			}
			// An object's property, by its id: read when the handler runs.
			read, isRead, err := t.objectRead(node, av)
			if err != nil {
				return boundHandler{}, err
			}
			if isRead {
				if reads == nil {
					reads = map[int]func() (qml.SpecValue, error){}
				}
				reads[i] = read
				argExprs = append(argExprs, av)
				continue
			}
			if _, err := t.evalValue(ctxBinding, av, node, nil); err != nil {
				return boundHandler{}, err
			}
			argExprs = append(argExprs, av)
		}
		calls = append(calls, invocation{fn: fn, argExprs: argExprs, params: passed, reads: reads, name: name})
	}

	label := calls[0].name
	if len(calls) > 1 {
		label = fmt.Sprintf("%s and %d more", label, len(calls)-1)
	}
	return boundHandler{
		name: label,
		key:  handlerKey(h),
		pos:  h.Pos,
		fn: func(signalArgs []qml.SpecValue) error {
			// In order, and STOPPING AT THE FIRST FAILURE. Running the rest
			// would be running the tail of a handler whose head did not happen,
			// which no author writing two statements in sequence expects.
			for _, c := range calls {
				args := make([]qml.SpecValue, 0, len(c.argExprs))
				for i, ax := range c.argExprs {
					if at, ok := c.params[i]; ok {
						if at >= len(signalArgs) {
							return fmt.Errorf("%s: the signal was raised without its parameter %q",
								c.name, params[at])
						}
						args = append(args, signalArgs[at])
						continue
					}
					if read, ok := c.reads[i]; ok {
						v, err := read()
						if err != nil {
							return fmt.Errorf("%s: %w", c.name, err)
						}
						args = append(args, v)
						continue
					}
					// Re-evaluated NOW. This can fail even though it validated
					// at mount — a host function may refuse at this moment —
					// and the handler stops rather than passing a value it
					// could not compute.
					res, err := t.evalValue(ctxBinding, ax, node, nil)
					if err != nil {
						return fmt.Errorf("%s: %w", c.name, err)
					}
					args = append(args, res.value)
				}
				if err := c.fn(args); err != nil {
					return fmt.Errorf("%s: %w", c.name, err)
				}
			}
			return nil
		},
	}, nil
}

// argExpr translates one handler argument into the evaluator's value
// vocabulary, WITHOUT evaluating it.
//
// The separation is the point. The bridge from the expression AST to
// [qml.SpecValue] is a question about SHAPE and has one answer for the life
// of the tree, so it is settled once at compile time; what the argument is
// WORTH is a question about the moment the signal fires, and is asked then.
// An argument the bridge cannot express is refused here rather than
// mistranslated into something that resolves to the wrong thing.
func (t *Tree) argExpr(node NodeID, h qml.SpecHandler, e *js.Expr) (qml.SpecValue, error) {
	v, ok := specValueOf(e)
	if !ok {
		return qml.SpecValue{}, t.refuseBody(node, h, e.Pos, fmt.Sprintf(
			"a %s argument; this engine passes literals and injected names", e.Kind))
	}
	return v, nil
}

// specValueOf translates an expression node into the evaluator's value
// vocabulary, reporting false for the shapes it has no equivalent for.
func specValueOf(e *js.Expr) (qml.SpecValue, bool) {
	switch e.Kind {
	case js.ExprString:
		return qml.SpecValue{Kind: qml.SpecValueString, Raw: e.Raw, Pos: e.Pos}, true
	case js.ExprNumber:
		return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: e.Raw, Pos: e.Pos}, true
	case js.ExprBool:
		return qml.SpecValue{Kind: qml.SpecValueBool, Raw: e.Raw, Pos: e.Pos}, true
	case js.ExprIdent, js.ExprMember:
		name, ok := calleeName(e)
		if !ok {
			return qml.SpecValue{}, false
		}
		return qml.SpecValue{Kind: qml.SpecValueRef, Raw: name,
			Path: splitDots(name), Pos: e.Pos}, true
	default:
		return qml.SpecValue{}, false
	}
}

// calleeName flattens an identifier or a non-computed member chain to its dotted
// spelling, and reports false for anything else.
//
// A COMPUTED member — `handlers[name]` — is deliberately not a name: which
// member it reaches is decided at run time, and a registry checked during
// planning cannot answer for it.
func calleeName(e *js.Expr) (string, bool) {
	if e == nil {
		return "", false
	}
	switch e.Kind {
	case js.ExprIdent:
		return e.Raw, true
	case js.ExprMember:
		if e.Computed {
			return "", false
		}
		left, ok := calleeName(e.Left)
		if !ok {
			return "", false
		}
		return left + "." + e.Name, true
	default:
		return "", false
	}
}

// handlerKey is a structural fingerprint of a handler's body.
//
// It exists because a reconcile has to answer "is this the same handler" BEFORE
// compiling it, and the answer used to be a comparison of handler NAMES — which
// a body has no single one of. It is deliberately independent of position and
// whitespace, so reformatting a file does not re-bind every signal in it, and it
// covers every statement and expression kind rather than only the ones this
// evaluator runs: a body that changed from something unrunnable to something
// else unrunnable must still read as changed.
func handlerKey(h qml.SpecHandler) string {
	var b []byte
	b = append(b, h.Signal...)
	b = append(b, ':')
	for i := range h.Body {
		h.Body[i].Walk(func(s *js.Stmt) bool {
			b = append(b, byte('0'+s.Kind))
			b = append(b, s.Raw...)
			b = append(b, ';')
			return true
		})
		h.Body[i].WalkExprs(func(e *js.Expr) bool {
			b = append(b, byte('0'+e.Kind))
			b = append(b, e.Raw...)
			b = append(b, '.')
			b = append(b, e.Name...)
			b = append(b, ',')
			return true
		})
	}
	return string(b)
}

// refuseBody reports a body shape this evaluator declines to run, naming the
// signal, the position inside the body, and what was found there.
func (t *Tree) refuseBody(node NodeID, h qml.SpecHandler, at parse.Position, found string) error {
	return SchemaError{Op: "bind", Node: node, Detail: h.Signal, Pos: at,
		Err: fmt.Errorf("%w: found %s", ErrHandlerBody, found)}
}

// signalParams are the parameter names an adapter declared for a type's signal.
func (t *Tree) signalParams(typeName, signal string) []string {
	sp, ok := t.adapter.(SignalParameters)
	if !ok {
		return nil
	}
	return sp.SignalParams(typeName, signal)
}

// paramIndex reports which declared parameter a bare-name argument passes.
func paramIndex(params []string, v qml.SpecValue) (int, bool) {
	if v.Kind != qml.SpecValueRef || strings.Contains(v.Raw, ".") {
		return 0, false
	}
	for i, p := range params {
		if p == v.Raw {
			return i, true
		}
	}
	return 0, false
}

// SignalParameters is an OPTIONAL capability an [Adapter] may implement to name
// the parameters its signals are raised with, so a handler can pass one on:
//
//	FileDialog { onAccepted: App.openFile(selectedFile) }
//
// The names are the handler's innermost scope, as a signal's parameters are in
// QML, and the values are the ones the adapter raised the signal with.
type SignalParameters interface {
	SignalParams(typeName, signal string) []string
}
