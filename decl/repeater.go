package decl

import (
	"fmt"
	"strconv"
	"strings"

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

// DELEGATE CHOOSER — Qt's DelegateChooser: a delegate per row chosen by a role.
//
//	Instantiator {
//	    model: App.menu                        // rows of kind "item" or "submenu"
//	    DelegateChooser {
//	        role: "kind"
//	        DelegateChoice { roleValue: "item";    MenuItem { text: model.label } }
//	        DelegateChoice { roleValue: "submenu"; Menu { title: model.label; … } }
//	    }
//	}
//
// As Qt defines it: the DelegateChooser is a Repeater's or an Instantiator's one
// delegate, and holds DelegateChoices, each with one delegate. For each row, the
// FIRST choice whose roleValue equals the row's `role` is instantiated — a
// choice with no roleValue matches every row — and a row no choice matches has
// no delegate. The rows keep their keys, so a row whose value changes is built
// again as its new choice, and its neighbours are untouched.

// isChooser reports whether a type is one of the chooser's two.
func isChooser(typeName string) bool {
	return typeName == "DelegateChooser" || typeName == "DelegateChoice"
}

// delegateChooser is a DelegateChooser, read: the role, and its choices.
type delegateChooser struct {
	role    string
	choices []delegateChoice
}

type delegateChoice struct {
	value    *qml.SpecValue // nil: matches every row
	delegate *qml.SpecNode
}

// readChooser reads a DelegateChooser, refusing what Qt's would not take.
func readChooser(sn *qml.SpecNode, fail func(string, ...any) error) (*delegateChooser, error) {
	if len(sn.Handlers) > 0 {
		return nil, fail("a DelegateChooser raises no signals (at %s)", sn.Handlers[0].Pos)
	}
	c := &delegateChooser{}
	for _, p := range sn.Props {
		if p.Name != "role" {
			return nil, fail("a DelegateChooser takes only a role; %q is not one of its properties (at %s)", p.Name, p.Pos)
		}
		if p.Value.Kind != qml.SpecValueString || p.Value.Raw == "" {
			return nil, fail("a DelegateChooser's role is the name of a model role, a string (at %s)", p.Value.Pos)
		}
		c.role = p.Value.Raw
	}
	if c.role == "" {
		return nil, fail("a DelegateChooser needs a role (at %s)", sn.Pos)
	}
	for _, ch := range sn.Children {
		if ch.Type != "DelegateChoice" {
			return nil, fail("a DelegateChooser holds DelegateChoices only, not a %s (at %s)", ch.Type, ch.Pos)
		}
		if len(ch.Handlers) > 0 {
			return nil, fail("a DelegateChoice raises no signals (at %s)", ch.Handlers[0].Pos)
		}
		if len(ch.Children) != 1 {
			return nil, fail("a DelegateChoice holds exactly one delegate, got %d (at %s)", len(ch.Children), ch.Pos)
		}
		choice := delegateChoice{delegate: ch.Children[0]}
		for _, p := range ch.Props {
			if p.Name != "roleValue" {
				return nil, fail("a DelegateChoice takes only a roleValue; %q is not one of its properties (at %s)", p.Name, p.Pos)
			}
			switch p.Value.Kind {
			case qml.SpecValueString, qml.SpecValueNumber, qml.SpecValueBool:
			default:
				return nil, fail("a DelegateChoice's roleValue is a string, number or bool (at %s)", p.Value.Pos)
			}
			v := p.Value
			choice.value = &v
		}
		c.choices = append(c.choices, choice)
	}
	if len(c.choices) == 0 {
		return nil, fail("a DelegateChooser needs at least one DelegateChoice (at %s)", sn.Pos)
	}
	return c, nil
}

// vetTemplate checks a delegate template's placement rules — every
// DelegateChooser and DelegateChoice in it is a Repeater's or an Instantiator's
// delegate, and every chooser is sound — whatever rows the model has NOW. A
// choice no row selects today, or the delegate of an empty model, is still
// the document: accepting it until a row reaches it would turn a model change
// into a failure the document had all along.
func vetTemplate(sn *qml.SpecNode, fail func(string, ...any) error) error {
	if isChooser(sn.Type) {
		return fail("a %s is a Repeater's or an Instantiator's delegate, and only that (at %s)", sn.Type, sn.Pos)
	}
	if isRepeater(sn.Type) && len(sn.Children) != 1 {
		return fail("a %s holds exactly one delegate, got %d (at %s)", sn.Type, len(sn.Children), sn.Pos)
	}
	for _, c := range sn.Children {
		if isRepeater(sn.Type) && c.Type == "DelegateChooser" {
			ch, err := readChooser(c, fail)
			if err != nil {
				return err
			}
			for _, choice := range ch.choices {
				if err := vetTemplate(choice.delegate, fail); err != nil {
					return err
				}
			}
			continue
		}
		if err := vetTemplate(c, fail); err != nil {
			return err
		}
	}
	return nil
}

// choose is the delegate for row ix, nil when no choice matches.
func (c *delegateChooser) choose(m Model, ix Index) *qml.SpecNode {
	have := m.Data(ix, c.role)
	for _, ch := range c.choices {
		if ch.value == nil || sameRoleValue(*ch.value, have) {
			return ch.delegate
		}
	}
	return nil
}

// sameRoleValue is Qt's QQmlDelegateChoice::match for a roleValue and a row's
// value: equal as values (a number by its value); else both converted to an
// integer and equal; else both converted to a string and equal. So a
// roleValue of 1 matches a row's "1", and "true" a row's true — as they do in
// Qt. The conversions are QVariant's for these kinds: a bool is 1 or 0 as an
// integer and "true" or "false" as a string; a number is an integer only when
// it is whole, and its string is its shortest form (1.0 is "1"); a string is an
// integer when it reads as one.
func sameRoleValue(want, have qml.SpecValue) bool {
	if want.Kind == have.Kind {
		if want.Kind == qml.SpecValueNumber {
			a, okA := numberOf(want)
			b, okB := numberOf(have)
			return okA && okB && a == b
		}
		if want.Raw == have.Raw {
			return true
		}
	}
	if a, okA := intOf(want); okA {
		if b, okB := intOf(have); okB && a == b {
			return true
		}
	}
	a, okA := stringOf(want)
	b, okB := stringOf(have)
	return okA && okB && a == b
}

func numberOf(v qml.SpecValue) (float64, bool) {
	f, err := strconv.ParseFloat(v.Raw, 64)
	return f, err == nil
}

// intOf is QVariant::toInt for the kinds a roleValue takes.
func intOf(v qml.SpecValue) (int64, bool) {
	switch v.Kind {
	case qml.SpecValueBool:
		if v.Raw == "true" {
			return 1, true
		}
		return 0, true
	case qml.SpecValueNumber:
		f, ok := numberOf(v)
		if !ok || f != float64(int64(f)) {
			return 0, false
		}
		return int64(f), true
	case qml.SpecValueString:
		n, err := strconv.ParseInt(strings.TrimSpace(v.Raw), 10, 64)
		return n, err == nil
	}
	return 0, false
}

// stringOf is QVariant::toString for the kinds a roleValue takes.
func stringOf(v qml.SpecValue) (string, bool) {
	switch v.Kind {
	case qml.SpecValueString, qml.SpecValueBool:
		return v.Raw, true
	case qml.SpecValueNumber:
		f, ok := numberOf(v)
		if !ok {
			return "", false
		}
		return strconv.FormatFloat(f, 'g', -1, 64), true
	}
	return "", false
}

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
		if isChooser(sn.Type) {
			return nil, SchemaError{Op: "repeater", Pos: sn.Pos, Err: fmt.Errorf(
				"%w: a %s is a Repeater's or an Instantiator's delegate, and only that (at %s)",
				ErrComponent, sn.Type, sn.Pos)}
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
	var chooser *delegateChooser
	if delegate.Type == "DelegateChooser" {
		c, err := readChooser(delegate, fail)
		if err != nil {
			return nil, err
		}
		chooser = c
		for _, choice := range c.choices {
			if err := vetTemplate(choice.delegate, fail); err != nil {
				return nil, err
			}
		}
	} else if err := vetTemplate(delegate, fail); err != nil {
		return nil, err
	}
	var out []*qml.SpecNode
	for r := range model.RowCount(nil) {
		ix := Index{Row: r}
		rowKey := key + "[" + strconv.Quote(model.Key(ix)) + "]" // quoted: a key cannot close the bracket
		d := delegate
		if chooser != nil {
			if d = chooser.choose(model, ix); d == nil {
				continue // no choice for this row: it has no delegate
			}
		}
		copied := bindRow(d, model, ix)
		// Every id in the delegate is its row's — the root's too, since each row
		// is its own copy of it. A root with none gets one: a stable identity
		// for the reconcile.
		copied = scopeIDs(copied, rowKey, "")
		if copied.ID == "" {
			copied.ID = "row@" + rowKey
		}
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
