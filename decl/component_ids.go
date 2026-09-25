package decl

import (
	"strings"

	"github.com/yongjohnlee80/golib/parse/js"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// IDS INSIDE A COMPONENT — Qt's component scope.
//
//	// dialogs/Login.qml
//	Dialog {
//	    TextField { id: user }
//	    onAccepted: App.login(user.text)
//	}
//
// An id a component file declares is that file's own: it names an object of
// ONE instance, and two uses of the component have two. The document using the
// component names the instance; it cannot reach the ids inside, and the ids
// inside shadow any of the document's with the same spelling.
//
// A use is expanded into a copy of the component, so each copy's ids are given
// a name of their own — `user@login`, from the use site's id, or its place
// under its nearest named ancestor when it has none; a spelling QML cannot
// write, so it can never collide with an id a document declares — and every
// reference to them in that copy is rewritten to match. The component's ROOT is the instance: its
// own id, if it has one, becomes the use site's (or a private one when the use
// site gives none), so `leader.close()` inside Leader.qml closes this Leader.
//
// Only the component's OWN file is renamed. A component used inside it is
// expanded afterwards, with its own ids renamed for itself.

// scopeIDs returns a copy of a component's root with its ids renamed for the
// use keyed key (expand's stable key for the use site). rootID is the use
// site's id, "" for none.
func scopeIDs(def *qml.SpecNode, key string, rootID string) *qml.SpecNode {
	rename := map[string]string{}
	_ = walkSpec(def, func(_ string, sn *qml.SpecNode) error {
		if sn.ID != "" {
			rename[sn.ID] = sn.ID + "@" + key
		}
		return nil
	})
	if def.ID != "" && rootID != "" {
		rename[def.ID] = rootID
	}
	if len(rename) == 0 {
		return def
	}
	return renameNode(def, rename)
}

func renameNode(sn *qml.SpecNode, rename map[string]string) *qml.SpecNode {
	out := *sn
	if to, ok := rename[sn.ID]; ok && sn.ID != "" {
		out.ID = to
	}
	out.Props = make([]qml.SpecProp, len(sn.Props))
	for i, p := range sn.Props {
		p.Value = renameValue(p.Value, rename)
		out.Props[i] = p
	}
	out.Handlers = make([]qml.SpecHandler, len(sn.Handlers))
	for i, h := range sn.Handlers {
		h.Body = renameStmts(h.Body, rename)
		out.Handlers[i] = h
	}
	out.Children = make([]*qml.SpecNode, len(sn.Children))
	for i, c := range sn.Children {
		out.Children[i] = renameNode(c, rename)
	}
	return &out
}

// renameValue rewrites a value's references to renamed ids.
func renameValue(v qml.SpecValue, rename map[string]string) qml.SpecValue {
	switch v.Kind {
	case qml.SpecValueRef:
		if len(v.Path) > 0 {
			if to, ok := rename[v.Path[0]]; ok {
				v.Path = append([]string{to}, v.Path[1:]...)
				v.Raw = strings.Join(v.Path, ".")
			}
		}
	case qml.SpecValueCall:
		v.Raw = renameDotted(v.Raw, rename)
		args := make([]qml.SpecValue, len(v.Args))
		for i, a := range v.Args {
			args[i] = renameValue(a, rename)
		}
		v.Args = args
	case qml.SpecValueExpr:
		if v.Expr != nil {
			v.Expr = renameExpr(v.Expr, rename)
		}
	}
	return v
}

// renameDotted rewrites the first segment of a dotted name.
func renameDotted(name string, rename map[string]string) string {
	head, rest, dotted := strings.Cut(name, ".")
	to, ok := rename[head]
	if !ok {
		return name
	}
	if dotted {
		return to + "." + rest
	}
	return to
}

// renameExpr returns a copy of e with identifiers naming renamed ids rewritten.
// Only an IDENTIFIER is a reference; a member's name (`x.user`) is not.
func renameExpr(e *js.Expr, rename map[string]string) *js.Expr {
	if e == nil {
		return nil
	}
	out := *e
	if e.Kind == js.ExprIdent {
		if to, ok := rename[e.Raw]; ok {
			out.Raw = to
		}
	}
	out.Left = renameExpr(e.Left, rename)
	out.Right = renameExpr(e.Right, rename)
	out.Alt = renameExpr(e.Alt, rename)
	if e.Args != nil {
		out.Args = make([]js.Expr, len(e.Args))
		for i := range e.Args {
			out.Args[i] = *renameExpr(&e.Args[i], rename)
		}
	}
	if e.Props != nil {
		out.Props = make([]js.ExprProperty, len(e.Props))
		for i, p := range e.Props {
			p.KeyExpr = renameExpr(p.KeyExpr, rename)
			p.Value = *renameExpr(&p.Value, rename)
			out.Props[i] = p
		}
	}
	return &out
}

func renameStmts(body []js.Stmt, rename map[string]string) []js.Stmt {
	if body == nil {
		return nil
	}
	out := make([]js.Stmt, len(body))
	for i, st := range body {
		st.Body = renameStmts(st.Body, rename)
		st.Cond = renameExpr(st.Cond, rename)
		st.Value = renameExpr(st.Value, rename)
		if st.Then != nil {
			then := renameStmts([]js.Stmt{*st.Then}, rename)[0]
			st.Then = &then
		}
		if st.Else != nil {
			els := renameStmts([]js.Stmt{*st.Else}, rename)[0]
			st.Else = &els
		}
		if st.Decls != nil {
			decls := make([]js.Declarator, len(st.Decls))
			for j, d := range st.Decls {
				d.Init = renameExpr(d.Init, rename)
				decls[j] = d
			}
			st.Decls = decls
		}
		out[i] = st
	}
	return out
}
