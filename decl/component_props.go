package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// A COMPONENT'S OWN PROPERTIES — parameters its uses set by name.
//
//	// dialogs/Picker.qml
//	Dialog {
//	    id: picker
//	    property string label: "Find"              // a value, with a default
//	    required property var model                // a value the use site must give
//	    default property alias columns: list.data  // the use site's children go to list
//	    title: label
//	    TableView { id: list; model: model }
//	    onAccepted: App.pick(picker.label, list.currentIndex)
//	}
//
//	// main.qml
//	Picker { id: files; label: "Open"; model: App.rows; TableViewColumn { role: "path" } }
//
// A component is a macro, so a declared property is resolved by SUBSTITUTION
// when a use is expanded, not as a property of anything at run time: every
// reference to it inside the copy — `label`, or `picker.label` through the
// root's id — is replaced by the use site's value, or the default. The use
// site's value is bound as written, so `model: App.rows` becomes a binding to
// App.rows inside the TableView, re-evaluated as any binding is. Nothing gains
// a change signal, which this engine does not have (property_read.go).

// declaredKind is the literal kind each scalar type accepts. A literal of
// another kind is refused; any other value — a binding, an expression — is the
// type's to judge where it lands.
var declaredKind = map[string]qml.SpecValueKind{
	"string": qml.SpecValueString,
	"int":    qml.SpecValueNumber,
	"real":   qml.SpecValueNumber,
	"double": qml.SpecValueNumber,
	"bool":   qml.SpecValueBool,
}

// wrongKind reports a literal whose kind the declared type refuses.
func wrongKind(d qml.SpecDecl, v qml.SpecValue) bool {
	want, typed := declaredKind[d.Type]
	if !typed {
		return false
	}
	switch v.Kind {
	case qml.SpecValueString, qml.SpecValueNumber, qml.SpecValueBool:
		return v.Kind != want
	}
	return false
}

// checkDecls judges a component file's declarations when the file loads, so
// a mistake in one is reported against the file rather than against each use.
func checkDecls(file string, root *qml.SpecNode) error {
	if sn := firstDeclaring(root, false); sn != nil {
		return fmt.Errorf("%w: %s: a %s inside the component declares %q, and only the "+
			"component's root declares properties (at %s)",
			ErrComponent, file, sn.Type, sn.Decls[0].Name, sn.Decls[0].Pos)
	}
	ids := map[string]bool{}
	_ = walkSpec(root, func(_ string, sn *qml.SpecNode) error {
		if sn.ID != "" {
			ids[sn.ID] = true
		}
		return nil
	})
	seen := map[string]bool{}
	alias := ""
	for _, d := range root.Decls {
		switch {
		case seen[d.Name]:
			return fmt.Errorf("%w: %s: %q is declared twice (at %s)", ErrComponent, file, d.Name, d.Pos)
		case ids[d.Name]:
			return fmt.Errorf("%w: %s: %q is declared and is also an id in the file, so a "+
				"reference to it could mean either (at %s)", ErrComponent, file, d.Name, d.Pos)
		case d.Target != "" && alias != "":
			return fmt.Errorf("%w: %s: %q and %q are both default aliases, and a component's "+
				"children go to one place (at %s)", ErrComponent, file, alias, d.Name, d.Pos)
		case d.Target != "" && !ids[d.Target]:
			return fmt.Errorf("%w: %s: the default alias %q is %s.data, and %q is no id in the "+
				"file (at %s)", ErrComponent, file, d.Name, d.Target, d.Target, d.Pos)
		case d.Default != nil && wrongKind(d, *d.Default):
			return fmt.Errorf("%w: %s: %q is declared %s, and its default is a %s (at %s)",
				ErrComponent, file, d.Name, d.Type, d.Default.Kind, d.Default.Pos)
		}
		seen[d.Name] = true
		if d.Target != "" {
			alias = d.Name
		}
	}
	return nil
}

// firstDeclaring returns the first node under sn — and sn itself when self is
// set — that declares a property, looking inside object templates too, or nil.
func firstDeclaring(sn *qml.SpecNode, self bool) *qml.SpecNode {
	if sn == nil {
		return nil
	}
	if self && len(sn.Decls) > 0 {
		return sn
	}
	for _, p := range sn.Props {
		if p.Value.Kind == qml.SpecValueTemplate {
			if bad := firstDeclaring(p.Value.Template, true); bad != nil {
				return bad
			}
		}
	}
	for _, c := range sn.Children {
		if bad := firstDeclaring(c, true); bad != nil {
			return bad
		}
	}
	return nil
}
