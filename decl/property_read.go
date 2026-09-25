package decl

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// READING AN OBJECT'S PROPERTY — `user.text`, in a handler.
//
//	TextField { id: user }
//	Button { onClicked: App.login(user.text) }
//
// A handler argument naming a property of an object by its id is read from the
// live object WHEN THE HANDLER RUNS — what the field holds when the button is
// pressed, as in Qt. Only in a handler: a binding over another object's
// property would need that property's change signal, which this engine does not
// have, and a binding that silently never updates is worse than a refusal.

// ErrNoProperty reports a read of a property an object's type does not let a
// handler read.
var ErrNoProperty = errors.New("decl: this object has no such readable property")

// PropertyReader is an OPTIONAL capability an [Adapter] may implement to let a
// handler read an object's property by its id.
type PropertyReader interface {
	// ReadablesOf lists the properties of a type a handler may read.
	ReadablesOf(typeName string) []string
	// ReadProperty reads one from a live node. It is called only for a
	// property ReadablesOf listed for the node's type.
	ReadProperty(node NodeID, prop string) (qml.SpecValue, error)
}

// objectRead compiles a handler argument that reads an object's property by
// id. ok is false when the argument does not start with a document id, and the
// caller resolves it as any other value.
func (t *Tree) objectRead(node NodeID, v qml.SpecValue) (read func() (qml.SpecValue, error), ok bool, err error) {
	if v.Kind != qml.SpecValueRef || len(v.Path) == 0 {
		return nil, false, nil
	}
	typ, isID := t.imported.ids[v.Path[0]]
	if !isID {
		return nil, false, nil
	}
	fail := func(why string) (func() (qml.SpecValue, error), bool, error) {
		return nil, true, SchemaError{Op: "bind", Node: node, Detail: v.Raw, Pos: v.Pos,
			Err: fmt.Errorf("%w: %s", ErrNoProperty, why)}
	}
	if len(v.Path) != 2 {
		return fail(fmt.Sprintf("%q is the %s declared with that id; a handler reads one of "+
			"its properties, as %s.<property>", v.Path[0], typ, v.Path[0]))
	}
	pr, capable := t.adapter.(PropertyReader)
	var readable []string
	if capable {
		readable = pr.ReadablesOf(typ)
	}
	for _, name := range readable {
		if name != v.Path[1] {
			continue
		}
		id, prop := v.Path[0], v.Path[1]
		return func() (qml.SpecValue, error) {
			// Resolved at FIRE time: a reload may have rebuilt the node under
			// the same id, and the read means the one the document holds now.
			target, live := t.NodeByID(id)
			if !live {
				return qml.SpecValue{}, fmt.Errorf("%s.%s: no node has the id %q now", id, prop, id)
			}
			return pr.ReadProperty(target, prop)
		}, true, nil
	}
	has := "no property a handler can read"
	if len(readable) > 0 {
		has = "the readable properties " + strings.Join(readable, ", ")
	}
	return fail(fmt.Sprintf("the %s %q has %s", typ, v.Path[0], has))
}
