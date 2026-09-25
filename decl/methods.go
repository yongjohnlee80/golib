package decl

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// METHODS — a handler calling a declared object by its id.
//
//	MenuItem { text: "E&xit"; onTriggered: quitDialog.open() }
//	Dialog   { id: quitDialog; onAccepted: App.quit() }
//
// Qt's imperative half: a Popup is opened with open(), an item takes focus with
// forceActiveFocus(). It is what lets a component own its own lifecycle. A
// dialog opened by a bound `visible` would close itself on Escape and leave the
// binding claiming it was open, so every host would have to write the reset —
// the same escape logic in every consumer, and a dialog that silently never
// reopens in the one that forgets. A dialog opened by a call closes itself and
// owes the host nothing.
//
// The ADAPTER says which methods a type has, as it says which properties it
// has; the engine resolves the id, checks the method when the handler is
// COMPILED, and asks the adapter to run it when the signal fires.

// Methods is an OPTIONAL capability an [Adapter] may implement to give node
// types methods a handler can call by id.
type Methods interface {
	// MethodsOf lists the methods nodes of a type answer to, or nothing.
	MethodsOf(typeName string) []string
	// Invoke runs a method on a live node. It is called only for a method
	// MethodsOf listed for the node's type.
	Invoke(node NodeID, method string, args []qml.SpecValue) error
}

// ErrNoMethod reports a call to a method a node's type does not have.
var ErrNoMethod = errors.New("decl: this object has no such method")

// documentIDs maps every id in a document to its node's type, for resolving
// `id.method()` while the document's handlers are compiled.
func documentIDs(root *qml.SpecNode) map[string]string {
	ids := map[string]string{}
	_ = walkSpec(root, func(_ string, sn *qml.SpecNode) error {
		if sn.ID != "" {
			ids[sn.ID] = sn.Type
		}
		return nil
	})
	return ids
}

// nameTaken reports whether a name already means something at the root of a
// reference: an injected name, or one an import brought into scope.
func (t *Tree) nameTaken(name string) bool {
	if _, ok := t.injected[name]; ok {
		return true
	}
	_, byName := t.imported.byName[name]
	_, byQualifier := t.imported.byQualifier[name]
	return byName || byQualifier
}

// methodCall compiles a call whose callee starts with a document id. ok is
// false when the callee does not start with one, and the caller resolves it as
// an injected name instead.
func (t *Tree) methodCall(node NodeID, callee string, at qml.SpecValue) (fn HandlerFunc, ok bool, err error) {
	path := splitDots(callee)
	typ, isID := t.imported.ids[path[0]]
	if !isID {
		return nil, false, nil
	}
	fail := func(why string) (HandlerFunc, bool, error) {
		return nil, true, SchemaError{Op: "bind", Node: node, Detail: callee, Pos: at.Pos,
			Err: fmt.Errorf("%w: %s", ErrNoMethod, why)}
	}
	if len(path) != 2 {
		return fail(fmt.Sprintf("%q is the %s declared with that id; a handler calls "+
			"one of its methods, as %s.<method>()", path[0], typ, path[0]))
	}
	var methods []string
	m, capable := t.adapter.(Methods)
	if capable {
		methods = m.MethodsOf(typ)
	}
	for _, name := range methods {
		if name == path[1] {
			id, method := path[0], path[1]
			return func(args []qml.SpecValue) error {
				// Resolved at FIRE time, not compile time: a reload may have
				// rebuilt the node under the same id, and the call means the
				// one the document holds now.
				target, live := t.NodeByID(id)
				if !live {
					return fmt.Errorf("%s.%s: no node has the id %q now", id, method, id)
				}
				return m.Invoke(target, method, args)
			}, true, nil
		}
	}
	has := "no methods"
	if len(methods) > 0 {
		has = "the methods " + strings.Join(methods, ", ")
	}
	return fail(fmt.Sprintf("the %s %q has %s", typ, path[0], has))
}
