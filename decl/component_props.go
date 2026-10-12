package decl

import (
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/js"
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
//	    property alias footer: footerRow.data      // a slot: the use site's footer object goes there
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
	filled := map[string]string{} // an alias's target, to the alias that fills it
	for _, d := range root.Decls {
		switch {
		case seen[d.Name]:
			return fmt.Errorf("%w: %s: %q is declared twice (at %s)", ErrComponent, file, d.Name, d.Pos)
		case ids[d.Name]:
			return fmt.Errorf("%w: %s: %q is declared and is also an id in the file, so a "+
				"reference to it could mean either (at %s)", ErrComponent, file, d.Name, d.Pos)
		case d.Target != "" && !d.Slot && alias != "":
			return fmt.Errorf("%w: %s: %q and %q are both default aliases, and a component's "+
				"children go to one place (at %s)", ErrComponent, file, alias, d.Name, d.Pos)
		case d.Target != "" && !ids[d.Target]:
			return fmt.Errorf("%w: %s: the %s %q is %s.data, and %q is no id in the "+
				"file (at %s)", ErrComponent, file, aliasKind(d), d.Name, d.Target, d.Target, d.Pos)
		case d.Target != "" && filled[d.Target] != "":
			return fmt.Errorf("%w: %s: %q and %q both fill %s, and an object is filled from one "+
				"place (at %s)", ErrComponent, file, filled[d.Target], d.Name, d.Target, d.Pos)
		case d.Target == "" && d.Default == nil && !d.Required:
			return fmt.Errorf("%w: %s: give %s a default, or make it required: every declared "+
				"property has a value, so a reference to it never falls through to a name of the "+
				"same spelling outside the component (at %s)", ErrComponent, file, d.Name, d.Pos)
		case d.Default != nil && wrongKind(d, *d.Default):
			return fmt.Errorf("%w: %s: %q is declared %s, and its default is a %s (at %s)",
				ErrComponent, file, d.Name, d.Type, d.Default.Kind, d.Default.Pos)
		}
		seen[d.Name] = true
		if d.Target != "" {
			filled[d.Target] = d.Name
		}
		if d.Target != "" && !d.Slot {
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

// substitute returns a copy of def — a component's root, its ids already
// scoped for this use — with every reference to a declared property replaced
// by use's value, or the default. A required property use does not set is an
// ErrComponent naming it and the use.
//
// It runs BEFORE the copy's own nested uses expand, so a value passed down
// to an inner component's property arrives there already substituted. It
// never walks a value it put in: the use site's value means what it means in
// the using document, and nothing inside the component can capture a name in
// it.
//
// Inside a handler, a SIGNAL PARAMETER shadows a declared property of the
// same name, as it shadows an injected name (handler.go): params names them
// per (type, signal), the adapter's signalParams, and types resolves a nested
// component use to the type its handlers are bound on. `picker.text` through
// the root id still means the property.
func substitute(def *qml.SpecNode, use *qml.SpecNode, ex expansion) (*qml.SpecNode, error) {
	if len(def.Decls) == 0 {
		return def, nil
	}
	given := map[string]qml.SpecValue{}
	for _, p := range use.Props {
		if len(p.Path) == 1 {
			given[p.Name] = p.Value
		}
	}
	b := binder{root: def.ID, values: map[string]qml.SpecValue{}, use: use, ex: ex}
	for _, d := range def.Decls {
		v, set := given[d.Name]
		switch {
		case d.Slot:
			// a slot is filled with an object where the use is expanded
			// (slotFills); it binds no value here
			if set && v.Kind != qml.SpecValueTemplate {
				return nil, fmt.Errorf("%w: %s's %q is a slot, which takes one object, as in %s: "+
					"Flex { … }, and this use gives it a %s (at %s)", ErrComponent, use.Type, d.Name,
					d.Name, v.Kind, v.Pos)
			}
			continue
		case d.Target != "":
			if set {
				return nil, fmt.Errorf("%w: %s's %q is its default alias, so the use writes it as "+
					"children, not a value (at %s)", ErrComponent, use.Type, d.Name, v.Pos)
			}
			continue
		case set && wrongKind(d, v):
			return nil, fmt.Errorf("%w: %s's %q is declared %s, and this use gives it a %s (at %s)",
				ErrComponent, use.Type, d.Name, d.Type, v.Kind, v.Pos)
		case set:
		case d.Default != nil:
			v = *d.Default
		case d.Required:
			return nil, fmt.Errorf("%w: %s's required property %q is not set by this use (at %s)",
				ErrComponent, use.Type, d.Name, use.Pos)
		default:
			// checkDecls refuses this when a component FILE loads; a loader of
			// its own may hand over a root that never met it.
			return nil, fmt.Errorf("%w: %s's %q has no value: give it a default, or make it "+
				"required (at %s)", ErrComponent, use.Type, d.Name, d.Pos)
		}
		b.values[d.Name] = v
	}
	out, err := b.node(def)
	if err != nil {
		return nil, err
	}
	out.Decls = nil
	return out, nil
}

// withoutDeclared is use without the properties def declares: they were
// substituted, and are not the root's to apply.
func withoutDeclared(use *qml.SpecNode, def *qml.SpecNode) *qml.SpecNode {
	out := *use
	if len(def.Decls) == 0 {
		return &out
	}
	declared := map[string]bool{}
	for _, d := range def.Decls {
		declared[d.Name] = true
	}
	out.Props = nil
	for _, p := range use.Props {
		if len(p.Path) != 1 || !declared[p.Name] {
			out.Props = append(out.Props, p)
		}
	}
	return &out
}

// aliasTarget is the id, as scopeIDs renamed it for this use, of the object a
// component's default alias sends the use site's children to; "" for none.
func aliasTarget(def *qml.SpecNode, key, rootID string) string {
	for _, d := range def.Decls {
		if d.Target == "" || d.Slot {
			continue
		}
		return scopedTarget(def, d.Target, key, rootID)
	}
	return ""
}

// scopedTarget is the id inner, as scopeIDs renamed it for this use: the use
// site's own id for the root, when it names one.
func scopedTarget(def *qml.SpecNode, inner, key, rootID string) string {
	if inner == def.ID && rootID != "" {
		return rootID
	}
	return scopedID(inner, key)
}

// slotFill is a slot's object, expanded, and the scoped id of the object it goes
// under.
type slotFill struct {
	target string
	node   *qml.SpecNode
}

// aliasKind names an alias in a message: the default one, or a slot.
func aliasKind(d qml.SpecDecl) string {
	if d.Slot {
		return "slot"
	}
	return "default alias"
}

// binder replaces references to a component's declared properties. Its
// walkers are component_ids.go's, generalised from renaming an identifier to
// replacing it with a value.
type binder struct {
	// root is the copy's root id AS scopeIDs LEFT IT — the use site's id, or
	// the private id@key — through which `picker.label` reaches a declared
	// property; "" when the root has none.
	root   string
	values map[string]qml.SpecValue
	use    *qml.SpecNode
	ex     expansion
}

// expansion is what substitution needs to know of the document it expands
// in: the component types in scope, and the signal parameters an adapter
// declares, by type and signal (nil for none).
type expansion struct {
	types  map[string]*qml.SpecNode
	params func(typeName, signal string) []string
}

// handlerType is the type a node's handlers bind on: its own, or for a use
// of a component the type that component expands to.
func (ex expansion) handlerType(typ string) string {
	for i := 0; i <= maxComponentDepth; i++ {
		def, ok := ex.types[typ]
		if !ok {
			break
		}
		typ = def.Type
	}
	return typ
}

// shadowed is the signal parameters of a node's handler, which shadow a
// declared property of the same name inside it.
func (ex expansion) shadowed(typ, signal string) map[string]bool {
	if ex.params == nil {
		return nil
	}
	ps := ex.params(ex.handlerType(typ), signal)
	if len(ps) == 0 {
		return nil
	}
	out := make(map[string]bool, len(ps))
	for _, p := range ps {
		out[p] = true
	}
	return out
}

// head reports the value a reference path starts with — `label`, or
// `picker.label` through the root id — and how many segments that took.
func (b *binder) head(path []string) (qml.SpecValue, int, bool) {
	if len(path) > 0 {
		if v, ok := b.values[path[0]]; ok {
			return v, 1, true
		}
	}
	if b.root != "" && len(path) > 1 && path[0] == b.root {
		if v, ok := b.values[path[1]]; ok {
			return v, 2, true
		}
	}
	return qml.SpecValue{}, 0, false
}

func (b *binder) node(sn *qml.SpecNode) (*qml.SpecNode, error) {
	out := *sn
	out.Props = make([]qml.SpecProp, len(sn.Props))
	for i, p := range sn.Props {
		v, err := b.value(p.Value)
		if err != nil {
			return nil, err
		}
		p.Value = v
		out.Props[i] = p
	}
	out.Handlers = make([]qml.SpecHandler, len(sn.Handlers))
	for i, h := range sn.Handlers {
		body, err := b.stmts(h.Body, b.ex.shadowed(sn.Type, h.Signal))
		if err != nil {
			return nil, err
		}
		h.Body = body
		out.Handlers[i] = h
	}
	out.Children = make([]*qml.SpecNode, len(sn.Children))
	for i, c := range sn.Children {
		nc, err := b.node(c)
		if err != nil {
			return nil, err
		}
		out.Children[i] = nc
	}
	return &out, nil
}

// value substitutes in a property value. A reference that IS a declared
// property becomes the use site's value whatever its kind — an object or a
// template included. One that reads a member of it (`label.length`), and a
// call whose arguments now hold an expression, become an expression.
//
// A template's own body is not walked: it is a scope of its own, as it is for
// ids (component_ids.go), and the view that instantiates it owns its names.
func (b *binder) value(v qml.SpecValue) (qml.SpecValue, error) {
	switch v.Kind {
	case qml.SpecValueRef:
		bound, n, ok := b.head(v.Path)
		switch {
		case !ok:
			return v, nil
		case n == len(v.Path):
			return bound, nil
		}
		return b.reproject(v)
	case qml.SpecValueCall:
		if _, _, ok := b.head(v.Path); ok {
			return b.reproject(v)
		}
		args := make([]qml.SpecValue, len(v.Args))
		expr := false
		for i, a := range v.Args {
			sa, err := b.value(a)
			if err != nil {
				return qml.SpecValue{}, err
			}
			args[i] = sa
			expr = expr || sa.Kind == qml.SpecValueExpr
		}
		v.Args = args
		if expr {
			// A call whose arguments are not all projected keeps its tree, as
			// the parser's projection does.
			e, ok := valueExpr(v)
			if !ok {
				return qml.SpecValue{}, fmt.Errorf("%w: %s's call to %s mixes an object with an "+
					"expression, which JavaScript cannot write (at %s; the use at %s)",
					ErrComponent, b.use.Type, v.Raw, v.Pos, b.use.Pos)
			}
			return qml.ProjectValue(e), nil
		}
		return v, nil
	case qml.SpecValueExpr:
		if v.Expr == nil {
			return v, nil
		}
		e, changed, err := b.expr(v.Expr, nil)
		if err != nil || !changed {
			return v, err
		}
		return qml.ProjectValue(e), nil
	}
	return v, nil
}

// reproject substitutes in a reference or call read as the expression it is,
// and projects the result as the parser would have.
func (b *binder) reproject(v qml.SpecValue) (qml.SpecValue, error) {
	e, ok := valueExpr(v)
	if !ok {
		return v, nil // an argument the parser never projects; nothing to substitute
	}
	se, _, err := b.expr(e, nil)
	if err != nil {
		return qml.SpecValue{}, err
	}
	return qml.ProjectValue(se), nil
}

// asExpr is a declared property's value in an expression or a handler, where
// only what JavaScript can write may go: an object or a template cannot. Nor,
// in v1, may a call: substituted into a handler it would be a call this
// engine's handler evaluator refuses, or one evaluated where it was not
// written; in a property's whole value it stays the binding it is.
func (b *binder) asExpr(name string, v qml.SpecValue) (*js.Expr, error) {
	if v.Kind == qml.SpecValueCall {
		return nil, fmt.Errorf("%w: %s's %q is the call %s(…), which an expression or a "+
			"handler cannot hold: pass a value, or bind it where it is used (at %s; the use at %s)",
			ErrComponent, b.use.Type, name, v.Raw, v.Pos, b.use.Pos)
	}
	e, ok := valueExpr(v)
	if !ok {
		return nil, fmt.Errorf("%w: %s's %q is a %s, which an expression or a handler cannot "+
			"hold; use it as a property's whole value (at %s; the use at %s)",
			ErrComponent, b.use.Type, name, v.Kind, v.Pos, b.use.Pos)
	}
	return e, nil
}

// expr substitutes in an expression, reporting whether anything changed. Only
// an IDENTIFIER is a reference, or a member of the root id; a member's name
// (`x.label`) is not.
//
// shadow is the signal parameters in scope, in a handler: a bare name among
// them is the parameter, not the property.
func (b *binder) expr(e *js.Expr, shadow map[string]bool) (*js.Expr, bool, error) {
	if e == nil {
		return nil, false, nil
	}
	switch {
	case e.Kind == js.ExprIdent && !shadow[e.Raw]:
		if v, ok := b.values[e.Raw]; ok {
			r, err := b.asExpr(e.Raw, v)
			return r, true, err
		}
	case e.Kind == js.ExprMember && !e.Computed && b.root != "" &&
		e.Left != nil && e.Left.Kind == js.ExprIdent && e.Left.Raw == b.root:
		if v, ok := b.values[e.Name]; ok {
			r, err := b.asExpr(e.Name, v)
			return r, true, err
		}
	}
	out := *e
	changed := false
	var err error
	sub := func(x *js.Expr) *js.Expr {
		if err != nil {
			return x
		}
		r, c, e2 := b.expr(x, shadow)
		changed, err = changed || c, e2
		return r
	}
	out.Left = sub(e.Left)
	out.Right = sub(e.Right)
	out.Alt = sub(e.Alt)
	if e.Args != nil {
		out.Args = make([]js.Expr, len(e.Args))
		for i := range e.Args {
			if r := sub(&e.Args[i]); r != nil {
				out.Args[i] = *r
			}
		}
	}
	if e.Props != nil {
		out.Props = make([]js.ExprProperty, len(e.Props))
		for i, p := range e.Props {
			p.KeyExpr = sub(p.KeyExpr)
			if r := sub(&p.Value); r != nil {
				p.Value = *r
			}
			out.Props[i] = p
		}
	}
	return &out, changed, err
}

func (b *binder) stmts(body []js.Stmt, shadow map[string]bool) ([]js.Stmt, error) {
	if body == nil {
		return nil, nil
	}
	out := make([]js.Stmt, len(body))
	for i, st := range body {
		var err error
		if st.Body, err = b.stmts(st.Body, shadow); err != nil {
			return nil, err
		}
		if st.Cond, _, err = b.expr(st.Cond, shadow); err != nil {
			return nil, err
		}
		if st.Value, _, err = b.expr(st.Value, shadow); err != nil {
			return nil, err
		}
		for _, branch := range []**js.Stmt{&st.Then, &st.Else} {
			if *branch == nil {
				continue
			}
			s, err := b.stmts([]js.Stmt{**branch}, shadow)
			if err != nil {
				return nil, err
			}
			*branch = &s[0]
		}
		if st.Decls != nil {
			decls := make([]js.Declarator, len(st.Decls))
			for j, d := range st.Decls {
				if d.Init, _, err = b.expr(d.Init, shadow); err != nil {
					return nil, err
				}
				decls[j] = d
			}
			st.Decls = decls
		}
		out[i] = st
	}
	return out, nil
}

// valueExpr writes a value as the JavaScript expression it was, or reports
// false for an object or a template, which JavaScript cannot write.
func valueExpr(v qml.SpecValue) (*js.Expr, bool) {
	switch v.Kind {
	case qml.SpecValueString:
		return &js.Expr{Kind: js.ExprString, Raw: v.Raw, Pos: v.Pos}, true
	case qml.SpecValueBool:
		return &js.Expr{Kind: js.ExprBool, Raw: v.Raw, Pos: v.Pos}, true
	case qml.SpecValueNumber:
		// The parser folded a sign onto the literal; JavaScript writes it as
		// an operator.
		if digits, neg := strings.CutPrefix(v.Raw, "-"); neg {
			return &js.Expr{Kind: js.ExprUnary, Raw: "-", Pos: v.Pos,
				Left: &js.Expr{Kind: js.ExprNumber, Raw: digits, Pos: v.Pos}}, true
		}
		return &js.Expr{Kind: js.ExprNumber, Raw: v.Raw, Pos: v.Pos}, true
	case qml.SpecValueRef:
		return pathExpr(v.Path, v.Pos), true
	case qml.SpecValueCall:
		args := make([]js.Expr, len(v.Args))
		for i, a := range v.Args {
			ae, ok := valueExpr(a)
			if !ok {
				return nil, false
			}
			args[i] = *ae
		}
		return &js.Expr{Kind: js.ExprCall, Pos: v.Pos, Left: pathExpr(v.Path, v.Pos), Args: args}, true
	case qml.SpecValueExpr:
		if v.Expr == nil {
			return nil, false
		}
		e := *v.Expr
		return &e, true
	}
	return nil, false
}

// pathExpr writes `a.b.c` as an identifier and its member chain.
func pathExpr(path []string, at parse.Position) *js.Expr {
	e := &js.Expr{Kind: js.ExprIdent, Raw: path[0], Pos: at}
	for _, seg := range path[1:] {
		e = &js.Expr{Kind: js.ExprMember, Left: e, Name: seg, Pos: at}
	}
	return e
}
