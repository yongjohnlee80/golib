package decl

import (
	"fmt"
	"strconv"

	"github.com/yongjohnlee80/golib/parse/js"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// REPEATER AND INSTANTIATOR — Qt's delegate per model row.
//
//	Flex {
//	    Repeater {
//	        model: App.leader            // a host model
//	        Text { text: model.label }   // the delegate, once per row
//	    }
//	}
//
//	MenuBar {
//	    Instantiator {
//	        model: App.menu
//	        Menu {
//	            title: model.label
//	            Instantiator { model: model.rows; MenuItem { text: model.label; onTriggered: App.run(model.id) } }
//	        }
//	    }
//	}
//
// Two of Qt's types, one mechanism. A Repeater's delegate is instantiated in an
// Item parent (a Flex); an Instantiator's in a Menu or MenuBar, which in Qt
// needs `onObjectAdded: menu.insertItem(index, object)` — golib's named
// divergence is that the parent takes the objects itself.
//
// The Repeater is EXPANDED, as a component use is: its place in its parent is
// taken by one copy of the delegate per row, and in each copy `model.<role>` is
// that row's value, typed, and `index` its row — the innermost Repeater's, so a
// nested `model: model.rows` reads the outer row and shadows it inside. Each
// copy's identity is its row's KEY, so when the model changes the engine
// re-expands and reconciles the document as a reload does: a changed row is
// patched in place, an inserted one built, a removed one destroyed. A handler's
// `model.id` is its row's value, so a row whose neighbours moved cannot run
// another row's command.

// isRepeater reports whether a type is one of the two.
func isRepeater(typeName string) bool { return typeName == "Repeater" || typeName == "Instantiator" }

// repeaterScope is one expansion's bookkeeping: the models it read and the
// sources those came from, for the tree to follow.
type repeaterScope struct {
	models  []Model
	sources map[string]bool
}

// expandRepeaters returns the document with every Repeater replaced by its
// delegate copies, and what it read.
func (t *Tree) expandRepeaters(root *qml.SpecNode) (*qml.SpecNode, repeaterScope, error) {
	sc := repeaterScope{sources: map[string]bool{}}
	var walk func(sn *qml.SpecNode, key string) ([]*qml.SpecNode, error)
	walk = func(sn *qml.SpecNode, key string) ([]*qml.SpecNode, error) {
		if isRepeater(sn.Type) {
			return t.instantiate(sn, key, &sc, walk)
		}
		out := *sn
		out.Children = nil
		seen := map[string]int{}
		for _, c := range sn.Children {
			ck := c.ID
			if ck == "" {
				ck = key + "/" + c.Type + "#" + strconv.Itoa(seen[c.Type])
				seen[c.Type]++
			}
			kids, err := walk(c, ck)
			if err != nil {
				return nil, err
			}
			out.Children = append(out.Children, kids...)
		}
		return []*qml.SpecNode{&out}, nil
	}
	nodes, err := walk(root, "")
	if err != nil {
		return nil, sc, err
	}
	if len(nodes) != 1 {
		return nil, sc, SchemaError{Op: "repeater", Pos: root.Pos,
			Err: fmt.Errorf("%w: a document's root cannot be a %s", ErrComponent, root.Type)}
	}
	return nodes[0], sc, nil
}

// instantiate expands one Repeater: its delegate, once per row.
func (t *Tree) instantiate(sn *qml.SpecNode, key string, sc *repeaterScope,
	walk func(*qml.SpecNode, string) ([]*qml.SpecNode, error)) ([]*qml.SpecNode, error) {
	fail := func(format string, a ...any) error {
		return SchemaError{Op: "repeater", Pos: sn.Pos, Err: fmt.Errorf("%w: "+format, append([]any{ErrComponent}, a...)...)}
	}
	if len(sn.Children) != 1 {
		return nil, fail("a %s holds exactly one delegate, got %d (at %s)", sn.Type, len(sn.Children), sn.Pos)
	}
	if len(sn.Handlers) > 0 {
		return nil, fail("a %s raises no signals here (at %s)", sn.Type, sn.Handlers[0].Pos)
	}
	var model Model
	for _, p := range sn.Props {
		if p.Name != "model" {
			return nil, fail("a %s takes only a model; %q is not one of its properties (at %s)", sn.Type, p.Name, p.Pos)
		}
		m, err := t.repeaterModel(p.Value, sc)
		if err != nil {
			return nil, fail("%v (at %s)", err, p.Value.Pos)
		}
		model = m
	}
	if model == nil {
		return nil, fail("a %s needs a model (at %s)", sn.Type, sn.Pos)
	}
	sc.models = append(sc.models, model)
	delegate := sn.Children[0]
	var out []*qml.SpecNode
	for r := range model.RowCount(nil) {
		ix := Index{Row: r}
		rowKey := key + "[" + model.Key(ix) + "]"
		copied := bindRow(delegate, model, ix)
		if copied.ID == "" {
			copied.ID = "row@" + rowKey // a stable identity for the reconcile
		}
		copied = scopeIDs(copied, rowKey, copied.ID)
		kids, err := walk(copied, rowKey)
		if err != nil {
			return nil, err
		}
		out = append(out, kids...)
	}
	return out, nil
}

// repeaterModel reads a Repeater's model: a host source (`App.menu`), or —
// already bound by an outer row — an object value (`model.rows`).
func (t *Tree) repeaterModel(v qml.SpecValue, sc *repeaterScope) (Model, error) {
	switch v.Kind {
	case qml.SpecValueObject:
		if m, ok := v.Obj.(Model); ok {
			return m, nil
		}
		return nil, fmt.Errorf("a %T is not a model", v.Obj)
	case qml.SpecValueRef:
		cur, ok := t.sources[v.Raw]
		if !ok {
			return nil, fmt.Errorf("%w: %q is not a source holding a model", ErrNotInjected, v.Raw)
		}
		sc.sources[v.Raw] = true
		if m, ok := cur.Obj.(Model); ok && cur.Kind == qml.SpecValueObject {
			return m, nil
		}
		return nil, fmt.Errorf("the source %q holds a %s, not a model", v.Raw, cur.Kind)
	}
	return nil, fmt.Errorf("a model is a source holding one, not a %s", v.Kind)
}

// bindRow is a copy of the delegate with `model.<role>` and `index` read from
// row ix. A Repeater inside is bound too — its own `model:` reads this row —
// but its delegate is left for it: there, `model` is its own row.
func bindRow(sn *qml.SpecNode, m Model, ix Index) *qml.SpecNode {
	out := *sn
	out.Props = make([]qml.SpecProp, len(sn.Props))
	for i, p := range sn.Props {
		p.Value = rowValue(p.Value, m, ix)
		out.Props[i] = p
	}
	out.Handlers = make([]qml.SpecHandler, len(sn.Handlers))
	for i, h := range sn.Handlers {
		h.Body = rowStmts(h.Body, m, ix)
		out.Handlers[i] = h
	}
	if isRepeater(sn.Type) {
		return &out // its delegate binds to its own rows
	}
	out.Children = make([]*qml.SpecNode, len(sn.Children))
	for i, c := range sn.Children {
		out.Children[i] = bindRow(c, m, ix)
	}
	return &out
}

// roleValue is a row's role as a value: "" for a role the model lacks.
func roleValue(m Model, ix Index, role string, pos qml.SpecValue) qml.SpecValue {
	v := m.Data(ix, role)
	if v.Kind == qml.SpecValueInvalid {
		v = qml.SpecValue{Kind: qml.SpecValueString}
	}
	v.Pos = pos.Pos
	return v
}

func rowValue(v qml.SpecValue, m Model, ix Index) qml.SpecValue {
	switch v.Kind {
	case qml.SpecValueRef:
		switch {
		case len(v.Path) == 2 && v.Path[0] == "model":
			return roleValue(m, ix, v.Path[1], v)
		case len(v.Path) == 1 && v.Path[0] == "index":
			return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: strconv.Itoa(ix.Row), Pos: v.Pos}
		}
	case qml.SpecValueCall:
		args := make([]qml.SpecValue, len(v.Args))
		for i, a := range v.Args {
			args[i] = rowValue(a, m, ix)
		}
		v.Args = args
	case qml.SpecValueExpr:
		if v.Expr != nil {
			v.Expr = rowExpr(v.Expr, m, ix)
		}
	}
	return v
}

// rowExpr is a copy of e with `model.<role>` and `index` as literals.
func rowExpr(e *js.Expr, m Model, ix Index) *js.Expr {
	if e == nil {
		return nil
	}
	if e.Kind == js.ExprMember && !e.Computed && e.Left != nil &&
		e.Left.Kind == js.ExprIdent && e.Left.Raw == "model" {
		return literal(roleValue(m, ix, e.Name, qml.SpecValue{}), e)
	}
	if e.Kind == js.ExprIdent && e.Raw == "index" {
		return &js.Expr{Kind: js.ExprNumber, Raw: strconv.Itoa(ix.Row), Pos: e.Pos}
	}
	out := *e
	out.Left = rowExpr(e.Left, m, ix)
	out.Right = rowExpr(e.Right, m, ix)
	out.Alt = rowExpr(e.Alt, m, ix)
	if e.Args != nil {
		out.Args = make([]js.Expr, len(e.Args))
		for i := range e.Args {
			out.Args[i] = *rowExpr(&e.Args[i], m, ix)
		}
	}
	return &out
}

// literal writes a role's value where an expression stood.
func literal(v qml.SpecValue, at *js.Expr) *js.Expr {
	kind := js.ExprString
	switch v.Kind {
	case qml.SpecValueNumber:
		kind = js.ExprNumber
	case qml.SpecValueBool:
		kind = js.ExprBool
	}
	return &js.Expr{Kind: kind, Raw: v.Raw, Pos: at.Pos}
}

func rowStmts(body []js.Stmt, m Model, ix Index) []js.Stmt {
	if body == nil {
		return nil
	}
	out := make([]js.Stmt, len(body))
	for i, st := range body {
		st.Body = rowStmts(st.Body, m, ix)
		st.Cond = rowExpr(st.Cond, m, ix)
		st.Value = rowExpr(st.Value, m, ix)
		if st.Then != nil {
			then := rowStmts([]js.Stmt{*st.Then}, m, ix)[0]
			st.Then = &then
		}
		if st.Else != nil {
			els := rowStmts([]js.Stmt{*st.Else}, m, ix)[0]
			st.Else = &els
		}
		if st.Decls != nil {
			decls := make([]js.Declarator, len(st.Decls))
			for j, d := range st.Decls {
				d.Init = rowExpr(d.Init, m, ix)
				decls[j] = d
			}
			st.Decls = decls
		}
		out[i] = st
	}
	return out
}

// ---------------------------------------------------------------- following

// followRepeaters makes the tree follow the models and sources the last
// successful expansion read: a change to any of them re-expands the document
// and reconciles it, on the scheduler — after the emission in progress, never
// inside it.
func (t *Tree) followRepeaters(written qml.SpecTree, sc repeaterScope) {
	t.written = &written
	t.repSources = sc.sources
	keep := map[Model]bool{}
	for _, m := range sc.models {
		keep[m] = true
		if _, ok := t.repSubs[m]; ok {
			continue
		}
		if t.repSubs == nil {
			t.repSubs = map[Model]func(){}
		}
		t.repSubs[m] = m.Subscribe(func(Change) { t.repeatersChanged() })
	}
	for m, cancel := range t.repSubs {
		if !keep[m] {
			cancel()
			delete(t.repSubs, m)
		}
	}
}

// repeatersChanged schedules one re-expansion however many changes arrive
// before it runs.
func (t *Tree) repeatersChanged() {
	if t.repPending || t.written == nil {
		return
	}
	t.repPending = true
	run := func() {
		t.repPending = false
		if t.written == nil || t.root == NoNode || t.failed {
			return
		}
		if _, err := t.Reconcile(*t.written); err != nil && t.repErr != nil {
			t.repErr(err)
		}
	}
	if t.sched != nil {
		t.sched(run)
		return
	}
	run()
}

// dropRepeaters stops following every model.
func (t *Tree) dropRepeaters() {
	for m, cancel := range t.repSubs {
		cancel()
		delete(t.repSubs, m)
	}
	t.written, t.repSources = nil, nil
}

// WithRepeaterErrors is where a re-expansion's refusal goes — a model changed
// into rows the document cannot take. Nil keeps the screen as it was, silently.
func WithRepeaterErrors(fn func(error)) Option {
	return func(t *Tree) { t.repErr = fn }
}
