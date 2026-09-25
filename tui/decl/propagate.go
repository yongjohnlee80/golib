package decl

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// PALETTE PROPAGATION — Qt's rule: "Items propagate explicit palette properties
// from parents to children." (Item.palette, Qt 6.)
//
//	Dialog {
//	    palette.window: Theme.dialog.window     // the card, and everything in it
//	    palette.windowText: Theme.dialog.windowText
//	    Text { text: "Quit?" }                  // on the card's colours
//	}
//
// A node's EFFECTIVE palette is its own roles over its parent's effective
// palette, nearest winning. Every role propagates, whether or not the node in
// between uses it — a palette is one object in Qt, not per-type fields — so a
// Flex can colour a subtree it paints nothing of. `palette` is on every type,
// as it is on every Qt Item.
//
// Propagation is LIVE, as Qt's is. A role that changes — a binding, a reload —
// restyles the nodes whose effective palette changed, through their type's
// restyle function; nothing is rebuilt, so an editor keeps its text. A role a
// reload removes is RESET ([decl.Resetter], Qt's RESET) and the node inherits
// the parent's again.
//
// The adapter learns the tree from the engine's own calls: a parent's Create
// lists its children, InsertChild and RemoveChild move them, Destroy drops
// them. Children are built before their parents, so a subtree first paints its
// own roles and is pushed its parent's when the parent is built.

// palNode is one node's place in the palette tree.
type palNode struct {
	own, eff  palette
	parent    decl.NodeID
	hasParent bool
}

// restyler dresses a built widget in an effective palette. An empty palette
// must leave the widget as a build with no palette would — golib's own look.
type restyler func(c tui.Component, p palette)

func withRestyle(typeName string, fn restyler) Option {
	return func(a *Adapter) { a.restylers[typeName] = fn }
}

// paletteRoles is every role a document may write, by its property name.
var paletteRoles = func() map[string]Role {
	out := map[string]Role{}
	for _, r := range []Role{roleWindow, roleWindowText, roleBase, roleText, roleHighlight,
		roleHighlightedText, roleAccent, roleButton, roleButtonText, roleInactiveHighlight,
		roleInactiveHighlightText, roleMid, roleLight} {
		out[r.prop()] = r
	}
	for i := range highlight.Styles {
		r := SyntaxRole(highlight.Style(i))
		out[r.prop()] = r
	}
	return out
}()

// isPaletteProp reports whether a property names a palette role — any
// `palette.` property, known or not, so a misspelt role is refused by name
// rather than reported as a property the type lacks.
func isPaletteProp(name string) bool {
	return strings.HasPrefix(name, "palette.") || strings.HasPrefix(name, syntaxPrefix)
}

func roleOf(name string, pos fmt.Stringer) (Role, error) {
	r, ok := paletteRoles[name]
	if !ok {
		group, what := "palette.", "a palette role"
		if strings.HasPrefix(name, syntaxPrefix) {
			group, what = syntaxPrefix, "a syntax style"
		}
		var names []string
		for n := range paletteRoles {
			if strings.HasPrefix(n, group) {
				names = append(names, strings.TrimPrefix(n, group))
			}
		}
		sort.Strings(names)
		return "", fmt.Errorf("%s is not %s; want one of %s (at %s)",
			name, what, strings.Join(names, ", "), pos)
	}
	return r, nil
}

// takePalette removes a declaration's palette roles from its properties.
func takePalette(props []qml.SpecProp) (palette, []qml.SpecProp, []string, error) {
	own := palette{}
	var rest []qml.SpecProp
	var names []string
	for _, p := range props {
		if !isPaletteProp(p.Name) {
			rest = append(rest, p)
			continue
		}
		r, err := roleOf(p.Name, p.Value.Pos)
		if err != nil {
			return nil, nil, nil, err
		}
		c, err := colorOf(p.Value)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", p.Name, err)
		}
		own[r] = c
		names = append(names, p.Name)
	}
	return own, rest, names, nil
}

// paletteBuilt records a node the adapter just built, adopts its children, and
// paints the subtree.
func (a *Adapter) paletteBuilt(id decl.NodeID, own palette, kids []decl.NodeID) {
	pn := &palNode{own: own}
	a.pal[id] = pn
	a.refresh(id)
	for _, k := range kids {
		if kn, ok := a.pal[k]; ok {
			kn.parent, kn.hasParent = id, true
			a.refresh(k)
		}
	}
}

// refresh recomputes a node's effective palette, restyles it if that changed,
// and carries a change on to its children.
func (a *Adapter) refresh(id decl.NodeID) {
	pn, ok := a.pal[id]
	if !ok {
		return
	}
	var inherited palette
	if pn.hasParent {
		if parent, ok := a.pal[pn.parent]; ok {
			inherited = parent.eff
		}
	}
	eff := palette{}
	for r, c := range inherited {
		eff[r] = c
	}
	for r, c := range pn.own {
		eff[r] = c
	}
	if samePalette(eff, pn.eff) {
		return
	}
	pn.eff = eff
	if b, ok := a.nodes[id]; ok {
		if fn := a.restylers[b.typ]; fn != nil {
			fn(b.comp, eff)
		}
	}
	for _, k := range a.kids[id] {
		a.refresh(k)
	}
}

func samePalette(a, b palette) bool {
	if len(a) != len(b) {
		return false
	}
	for r, c := range a {
		if d, ok := b[r]; !ok || d != c {
			return false
		}
	}
	return true
}

// setRole is a palette role's runtime setter, for every type.
func (a *Adapter) setRole(id decl.NodeID, prop string, v qml.SpecValue) error {
	r, err := roleOf(prop, v.Pos)
	if err != nil {
		return err
	}
	c, err := colorOf(v)
	if err != nil {
		return fmt.Errorf("%s: %w", prop, err)
	}
	pn, ok := a.pal[id]
	if !ok {
		return fmt.Errorf("%s: node %d has no palette", prop, id)
	}
	pn.own[r] = c
	a.refresh(id)
	return nil
}

// Resettable implements [decl.Resetter]: every palette role can be reset, and
// the node then inherits its parent's.
func (a *Adapter) Resettable(_, prop string) bool {
	_, ok := paletteRoles[prop]
	return ok
}

// Reset implements [decl.Resetter].
func (a *Adapter) Reset(id decl.NodeID, prop string) error {
	r, ok := paletteRoles[prop]
	if !ok {
		return fmt.Errorf("%s cannot be reset", prop)
	}
	pn, ok := a.pal[id]
	if !ok {
		return fmt.Errorf("%s: node %d has no palette", prop, id)
	}
	delete(pn.own, r)
	a.refresh(id)
	return nil
}

var _ decl.Resetter = (*Adapter)(nil)

// paletteAdopt places a child under a parent a reload spliced it into.
func (a *Adapter) paletteAdopt(parent, child decl.NodeID) {
	cn := a.pal[child]
	if a.pal[parent] == nil || cn == nil {
		return
	}
	cn.parent, cn.hasParent = parent, true
	a.refresh(child)
}

// paletteRelease takes a child from its parent: a reload removed it, or it is
// being destroyed.
func (a *Adapter) paletteRelease(child decl.NodeID) {
	if cn := a.pal[child]; cn != nil {
		cn.hasParent = false
	}
}

// ---------------------------------------------------------------- restylers
//
// One per type that takes roles. Each maps the effective palette to its
// widget's looks the one way palette.go defines, and an empty palette to
// golib's own.

func restyleText(c tui.Component, p palette) {
	c.(*widget.Text).WithStyle(p.look(roleWindow, roleWindowText))
}

func restyleFrame(c tui.Component, p palette) {
	st, focused := p.frameStyles()
	c.(*widget.Box).WithStyle(st).WithFocusedStyle(focused)
}

func restyleEditor(c tui.Component, p palette) {
	st, _ := p.editorStyles()
	c.(*widget.Editor).WithStyles(st)
}

func restyleStatusBar(c tui.Component, p palette) {
	st, _ := p.barStyle()
	c.(*widget.StatusBar).WithBarStyle(st)
}

func restyleMenuBar(c tui.Component, p palette) {
	st, _ := p.menuStyle()
	c.(*menuBarNode).bar.WithStyle(st)
}

func restyleDialog(c tui.Component, p palette) {
	d := c.(*dialogNode)
	card, buttons := p.dialogStyles()
	d.modal.WithStyle(card)
	for _, b := range d.modal.Buttons() {
		b.WithStyle(buttons)
	}
	if d.chooser == nil {
		return
	}
	st, _ := p.browserStyles()
	switch v := d.chooser.(type) {
	case *widget.FileOpenView:
		v.WithStyles(st)
		v.SetPreviewHighlighting(d.highlighterFor, p.syntaxStyles())
	case *widget.FileSaveView:
		v.WithStyles(st)
	}
}

// frameStyles are a Frame's base and focused styles: its interior and border
// on window, the border line in windowText, and in highlight while focus is
// inside it. Zero where no role was set.
func (p palette) frameStyles() (style.Style, style.Style) {
	var st, focused style.Style
	if c, ok := p[roleWindow]; ok {
		st = st.Background(c)
	}
	if c, ok := p[roleWindowText]; ok {
		st = st.BorderForeground(c)
	}
	if c, ok := p[roleHighlight]; ok {
		focused = style.New().BorderForeground(c)
	}
	return st, focused
}
