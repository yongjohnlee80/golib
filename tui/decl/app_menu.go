package decl

import (
	"errors"
	"fmt"
	"strconv"
	"unicode"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// MENUS.
//
//	MenuBar {
//	    Menu { title: "&File"
//	        MenuItem { text: "&New"; onTriggered: App.newFile() }
//	        MenuSeparator {}
//	        MenuItem { text: "E&xit"; onTriggered: App.exit() }
//	    }
//	    Menu { title: "&Help"; align: Tui.Right
//	        MenuItem { text: "&About"; onTriggered: App.about() }
//	    }
//	}
//
// A Menu and a MenuItem are DATA, not widgets. golib's Menu takes a model of
// rows and owns everything a row does — selection, hotkeys, submenus, popups —
// so a row declared in QML becomes a row in that model, assembled by the
// MenuBar that contains it. Nothing here is mounted or painted on its own.
//
// `&` MARKS THE MNEMONIC, as it does in Qt: `"E&xit"` is the label "Exit" with
// `x` as its hotkey, and `&&` is a literal ampersand. That is QML's own
// spelling, so a hotkey needs no property of its own.

// menuNode is one declared row, before a MenuBar adopts it.
type menuNode struct {
	kind     string // "Menu", "MenuItem" or "MenuSeparator" — for diagnostics
	id       string // the node's engine identity, "" when anonymous; see rowID
	model    widget.MenuItemModel
	trigger  func()
	toggled  func() // the row's `toggled`, nil when the document bound none
	children []*menuNode

	// owner is the menu that adopted this row, nil until then. The row's model
	// is its state, and the menu the one rule that changes it: a document's
	// write goes through the menu once there is one (the engine applies a
	// row's properties right after building it, BEFORE its MenuBar exists,
	// since children are built first), and every change the menu makes comes
	// back through its report (WithMenuOnToggled) — as it would to any owner of
	// menu rows.
	owner *widget.Menu
	// bar is the MenuBar that adopted this row: a Menu whose rows change —
	// an Instantiator's model moved — asks it to project the menu again.
	bar *menuBarNode
}

func (*menuNode) Init(*tui.Context)               {}
func (*menuNode) Layout(tui.Constraints) tui.Size { return tui.Size{} }
func (*menuNode) declarationOnly()                {}
func (*menuNode) Render(tui.Surface)              {}
func (*menuNode) HandleEvent(tui.Event) bool      { return false }

// arrange takes a Menu's rows as they now are, and has its bar project them.
func (n *menuNode) arrange(children []tui.Component, _ []map[string]qml.SpecValue, _ tui.Component) error {
	if n.kind != "Menu" {
		return fmt.Errorf("a %s takes no rows", n.kind)
	}
	rows, err := menuRows(children, "a Menu holds MenuItem, MenuSeparator and Menu rows only")
	if err != nil {
		return err
	}
	n.children = rows
	if n.bar == nil {
		return nil
	}
	return n.bar.project()
}

// menuRows reads a menu's children as its rows.
func menuRows(children []tui.Component, refusal string) ([]*menuNode, error) {
	rows := make([]*menuNode, 0, len(children))
	for _, c := range children {
		n, ok := c.(*menuNode)
		if !ok {
			return nil, errors.New(refusal)
		}
		rows = append(rows, n)
	}
	return rows, nil
}

// setChecked is the row's `checked`. Once the menu owns the rule its report
// records the change — this row's and a radio group's.
func (n *menuNode) setChecked(on bool) {
	if n.owner != nil {
		n.owner.SetChecked(n.model.ID, on)
		return
	}
	n.model.Checked = on
}

func (n *menuNode) setEnabled(on bool) {
	n.model.Enabled = on
	if n.owner != nil {
		n.owner.SetEnabled(n.model.ID, on)
	}
}

// SetVisible is the row's `visible` — Qt's MenuItem.visible: a hidden row
// takes no row in its menu, and keeps its state.
func (n *menuNode) SetVisible(on bool) {
	n.model.Visible = on
	if n.owner != nil {
		n.owner.SetVisible(n.model.ID, on)
	}
}

// mnemonic splits `&`-marked text into the label, the hotkey and its index.
func mnemonic(text string) (label string, hotkey rune, idx int) {
	idx = -1
	out := make([]rune, 0, len(text))
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		if rs[i] == '&' && i+1 < len(rs) {
			if rs[i+1] == '&' {
				out = append(out, '&')
				i++
				continue
			}
			if idx < 0 {
				idx = len(out)
				hotkey = unicode.ToLower(rs[i+1])
			}
			continue
		}
		out = append(out, rs[i])
	}
	return string(out), hotkey, idx
}

func withMnemonic(m widget.MenuItemModel, text string) widget.MenuItemModel {
	label, key, idx := mnemonic(text)
	m.Label = label
	if idx >= 0 {
		m.Hotkey, m.HotkeyIdx = key, idx
	}
	return m
}

// ---------------------------------------------------------------- rows

func buildMenuItem(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("MenuItem takes no children; use a Menu for a submenu (at %s)", b.Pos)
	}
	var text, group, shortcut string
	var checkable bool
	consumed, err := readProps(b.Props, map[string]field{
		"text":      into(&text, stringOf),
		"group":     into(&group, stringOf),
		"shortcut":  into(&shortcut, stringOf),
		"checkable": into(&checkable, boolOf),
	})
	if err != nil {
		return nil, nil, err
	}
	if text == "" {
		return nil, nil, fmt.Errorf("MenuItem needs text (at %s)", b.Pos)
	}

	// The ID and the Action are filled in when a MenuBar adopts the row: they
	// must be unique across the whole bar, and only the bar sees all of it.
	var m widget.MenuItemModel
	switch {
	case group != "":
		m = widget.NewRadio("", "", group, nil)
	case checkable:
		m = widget.NewCheck("", "", nil)
	default:
		m = widget.NewCommand("", "", nil)
	}
	m = withMnemonic(m, text)
	m.Accel = shortcut
	n := &menuNode{kind: "MenuItem", id: b.ID, model: m}
	// Only a row the document BOUND gets a trigger. Build.Emitter hands back a
	// no-op for an unbound signal, which is right for a widget callback and
	// wrong here: every row would then report its activation as handled, and
	// a row with no onTriggered would look like one that worked.
	if _, bound := b.Emitters["triggered"]; bound {
		n.trigger = b.Emitter("triggered")
	}
	if _, bound := b.Emitters["toggled"]; bound {
		n.toggled = b.Emitter("toggled")
	}
	return n, consumed, nil
}

func buildMenuSeparator(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 || len(b.Props) != 0 {
		return nil, nil, fmt.Errorf("MenuSeparator takes no properties or children (at %s)", b.Pos)
	}
	return &menuNode{kind: "MenuSeparator", id: b.ID, model: widget.NewSeparator("")}, nil, nil
}

// menuAligns is where a top-level Menu sits on its bar.
var menuAligns = enum[bool]{values: map[string]bool{
	"Left":  false,
	"Right": true,
}}

func buildMenu(b Build) (tui.Component, []string, error) {
	var title string
	var right bool
	consumed, err := readProps(b.Props, map[string]field{
		"title": into(&title, stringOf),
		"align": into(&right, menuAligns.read),
	})
	if err != nil {
		return nil, nil, err
	}
	if title == "" {
		return nil, nil, fmt.Errorf("Menu needs a title (at %s)", b.Pos)
	}
	rows, err := menuRows(b.Children, "a Menu holds MenuItem, MenuSeparator and Menu rows only")
	if err != nil {
		return nil, nil, fmt.Errorf("%w (at %s)", err, b.Pos)
	}
	n := &menuNode{kind: "Menu", id: b.ID, children: rows}
	n.model = withMnemonic(widget.NewSubmenu("", "", nil), title)
	n.model.PegRight = right
	return n, consumed, nil
}

// ---------------------------------------------------------------- bar

// menuCommand is a row's intent, carrying identity only — so the model stays
// data and the bar's executor is the one place a row becomes an effect.
type menuCommand struct{ id tui.ActionID }

func (c menuCommand) ActionID() tui.ActionID { return c.id }

// buildMenuBar adopts every row beneath it, gives each an identity, and builds
// golib's Menu and MenuBar over the resulting model.
//
// A triggered row reaches its `onTriggered` through the bar's executor, under
// the engine's emission rules — the menu never calls a handler itself.
func buildMenuBar(b Build) (tui.Component, []string, error) {
	var vim bool
	consumed, err := readProps(b.Props, map[string]field{"vimNavigation": into(&vim, boolOf)})
	if err != nil {
		return nil, nil, err
	}
	menuOpts := []widget.MenuOption{widget.WithMenuVimNavigation(vim)}

	top, err := barRows(b.Children)
	if err != nil {
		return nil, nil, fmt.Errorf("%w (at %s)", err, b.Pos)
	}
	bar := &menuBarNode{rows: top, triggers: map[tui.ActionID]func(){}}
	// The menu's report is the rows' state: each change recorded on its row,
	// and a user's raised as that row's `toggled`, after the state has changed.
	menuOpts = append(menuOpts, widget.WithMenuOnToggled(func(id widget.ItemID, checked, byUser bool) {
		n := bar.byID[id]
		if n == nil {
			return
		}
		n.model.Checked = checked
		if byUser && n.toggled != nil {
			n.toggled()
		}
	}))
	menuOpts = append(menuOpts, widget.WithActionExecutor(func(inv tui.ActionInvocation) bool {
		if inv.Action == nil {
			return false
		}
		fn, ok := bar.triggers[inv.Action.ActionID()]
		if !ok {
			// Not ours — a row with no onTriggered. Reporting "not handled"
			// keeps that visible rather than swallowing it.
			return false
		}
		fn()
		return true
	}))
	bar.menu = widget.NewMenu(menuOpts...)
	if err := bar.project(); err != nil {
		return nil, nil, fmt.Errorf("%w (at %s)", err, b.Pos)
	}

	var barOpts []widget.MenuBarOption
	if v, ok := b.SelfAttached["Dock.edge"]; ok {
		edge, err := dockEdges.read(v)
		if err != nil {
			return nil, nil, fmt.Errorf("Dock.edge: %w", err)
		}
		barOpts = append(barOpts, widget.WithBarPlacement(barPlacements[edge]))
	}
	bar.bar = widget.NewMenuBar(bar.menu, barOpts...)
	return bar, consumed, nil
}

// project builds the menu's model from the bar's rows as they now are, and
// hands it to the live menu — at construction, and whenever a Menu's rows
// change (an Instantiator's model moved). Every row keeps its ItemID while it
// lives, so the menu keeps an open submenu whose row survived and closes one
// whose row went, as widget.Menu.SetModel defines.
//
// Each row's model is its current state — every change the menu made came
// back through its report — so the projection is built from the rows alone.
func (m *menuBarNode) project() error {
	triggers := map[tui.ActionID]func(){}
	byID := map[widget.ItemID]*menuNode{}
	var nodes []*menuNode
	var adopt func(n *menuNode, id widget.ItemID) widget.MenuItemModel
	adopt = func(n *menuNode, id widget.ItemID) widget.MenuItemModel {
		n.model.ID = id
		byID[id] = n
		nodes = append(nodes, n)
		if n.kind == "MenuItem" && n.trigger != nil {
			act := tui.ActionID(id)
			n.model.Action = menuCommand{act}
			triggers[act] = n.trigger
		}
		if n.kind == "Menu" {
			n.model.Children = nil
			anon := 0
			for _, c := range n.children {
				n.model.Children = append(n.model.Children, adopt(c, rowID(id, c, &anon)))
			}
		}
		return n.model
	}
	var model []widget.MenuItemModel
	var categories []menuCategory
	anon := 0
	for _, n := range m.rows {
		row := adopt(n, rowID("", n, &anon))
		model = append(model, row)
		// A top-level Menu's mnemonic is its ACCESS KEY: Alt+F opens File.
		if row.Hotkey != 0 {
			categories = append(categories, menuCategory{hotkey: row.Hotkey, id: row.ID})
		}
	}
	if err := m.menu.SetModel(model); err != nil {
		return fmt.Errorf("the menu is malformed: %w", err)
	}
	for _, n := range nodes {
		n.owner, n.bar = m.menu, m
	}
	m.triggers, m.categories, m.byID = triggers, categories, byID
	return nil
}

// rowID is a row's ItemID: its engine identity when it has one — every
// delegate instance does, keyed by its model row — so the id follows the row
// wherever the model moves it; otherwise its place among its menu's anonymous
// rows, under its menu's id. The first form is quoted, which keeps the two
// apart: a quoted id never ends in a digit.
func rowID(parent widget.ItemID, n *menuNode, anon *int) widget.ItemID {
	if n.id != "" {
		return widget.ItemID("mk:" + strconv.Quote(n.id))
	}
	*anon++
	if parent == "" {
		return widget.ItemID("mk:#" + strconv.Itoa(*anon))
	}
	return widget.ItemID(string(parent) + "/#" + strconv.Itoa(*anon))
}

// arrange takes the bar's Menus as they now are and projects them.
func (m *menuBarNode) arrange(children []tui.Component, _ []map[string]qml.SpecValue, _ tui.Component) error {
	rows, err := barRows(children)
	if err != nil {
		return err
	}
	m.rows = rows
	return m.project()
}

// barRows reads a MenuBar's children: Menus, and nothing else.
func barRows(children []tui.Component) ([]*menuNode, error) {
	const refusal = "a MenuBar holds Menu rows only"
	rows, err := menuRows(children, refusal)
	if err != nil {
		return nil, err
	}
	for _, n := range rows {
		if n.kind != "Menu" {
			return nil, errors.New(refusal)
		}
	}
	return rows, nil
}

// barPlacements orients a bar to the edge it is docked on, so dropdowns open
// away from that edge. The Window decides WHERE the bar sits; the bar only
// needs to know which way to face — read off the same Dock.edge value.
var barPlacements = map[tui.DockEdge]widget.BarPlacement{
	tui.DockTop:    widget.BarPlacementTop,
	tui.DockBottom: widget.BarPlacementBottom,
	tui.DockLeft:   widget.BarPlacementLeft,
	tui.DockRight:  widget.BarPlacementRight,
}
