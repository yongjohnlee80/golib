package decl

import (
	"fmt"
	"strconv"

	"github.com/yongjohnlee80/golib/parse/js"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// BITWISE OR — the one operator this evaluator runs.
//
//	standardButtons: Dialog.Yes | Dialog.No
//
// It is how Qt combines FLAGS, and flags are the only thing it is here for: an
// adapter exposes a flag set as integer constants, and a document combines them
// the way every Qt document does. General arithmetic is still refused — a
// binding that needs it calls a function — because each operator accepted is a
// rule about operand kinds that every reader of a document has to know.

// bitOr reports whether v is `left | right`, with both operands projected into
// values the evaluator already walks. The ONE place the shape is recognised, so
// evaluation and dependency collection cannot come to disagree about it.
func bitOr(v qml.SpecValue) (left, right qml.SpecValue, ok bool) {
	e := v.Expr
	if v.Kind != qml.SpecValueExpr || e == nil || e.Kind != js.ExprBinary || e.Raw != "|" ||
		e.Left == nil || e.Right == nil {
		return qml.SpecValue{}, qml.SpecValue{}, false
	}
	return qml.ProjectValue(e.Left), qml.ProjectValue(e.Right), true
}

// walkBitOr evaluates both operands and ORs them. Each must be an integer: a
// flag set is a set of bits, and a string or a fraction OR-ed into one is a
// document mistake worth naming rather than a value worth guessing at.
func (t *Tree) walkBitOr(ctx context, v, left, right qml.SpecValue, at NodeID,
	overlay map[string]qml.SpecValue, called bool, mode walkMode) (qml.SpecValue, error) {
	if called {
		return qml.SpecValue{}, t.refuse(at, v, "a combination of flags cannot be called")
	}
	var bits int64
	for _, operand := range []qml.SpecValue{left, right} {
		ev, err := t.walkValue(ctx, operand, at, overlay, false, mode)
		if err != nil {
			return qml.SpecValue{}, err
		}
		if mode == modeValidate {
			// A function's result is not known yet, and a validating walk checks
			// shape only; the evaluating walk that follows checks the kinds.
			continue
		}
		n, err := flagOf(ev)
		if err != nil {
			return qml.SpecValue{}, t.refuse(at, operand, err.Error())
		}
		bits |= n
	}
	return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: strconv.FormatInt(bits, 10), Pos: v.Pos}, nil
}

func flagOf(v qml.SpecValue) (int64, error) {
	if v.Kind != qml.SpecValueNumber {
		return 0, fmt.Errorf("`|` combines integer flags, and this is a %s", v.Kind)
	}
	n, err := strconv.ParseInt(v.Raw, 0, 64)
	if err != nil {
		return 0, fmt.Errorf("`|` combines integer flags, and %s is not an integer", v.Raw)
	}
	return n, nil
}
