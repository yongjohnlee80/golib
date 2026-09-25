package decl

import (
	"fmt"
	"strconv"
	"unicode"

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
	model    widget.MenuItemModel
	trigger  func()
	children []*menuNode

	// owner is the menu that adopted this row, nil until then. The engine
	// applies a row's runtime properties right after building it — which is
	// BEFORE its MenuBar exists, since children are built first — so a setter
	// writes the model until adoption and the live menu afterwards.
	owner *widget.Menu
}

func (*menuNode) Init(*tui.Context)               {}
func (*menuNode) Layout(tui.Constraints) tui.Size { return tui.Size{} }
func (*menuNode) Render(tui.Surface)              {}
func (*menuNode) HandleEvent(tui.Event) bool      { return false }

func (n *menuNode) setChecked(on bool) {
	if n.owner != nil {
		n.owner.SetChecked(n.model.ID, on)
		return
	}
	n.model.Checked = on
}

func (n *menuNode) setEnabled(on bool) {
	if n.owner != nil {
		n.owner.SetEnabled(n.model.ID, on)
		return
	}
	n.model.Enabled = on
}

// SetVisible is the row's `visible` — Qt's MenuItem.visible: a hidden row
// takes no row in its menu, and keeps its state.
func (n *menuNode) SetVisible(on bool) {
	if n.owner != nil {
		n.owner.SetVisible(n.model.ID, on)
		return
	}
	n.model.Visible = on
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
	n := &menuNode{kind: "MenuItem", model: m}
	// Only a row the document BOUND gets a trigger. Build.Emitter hands back a
	// no-op for an unbound signal, which is right for a widget callback and
	// wrong here: every row would then report its activation as handled, and
	// a row with no onTriggered would look like one that worked.
	if _, bound := b.Emitters["triggered"]; bound {
		n.trigger = b.Emitter("triggered")
	}
	return n, consumed, nil
}

func buildMenuSeparator(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 || len(b.Props) != 0 {
		return nil, nil, fmt.Errorf("MenuSeparator takes no properties or children (at %s)", b.Pos)
	}
	return &menuNode{kind: "MenuSeparator", model: widget.NewSeparator("")}, nil, nil
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
	n := &menuNode{kind: "Menu"}
	for _, c := range b.Children {
		child, ok := c.(*menuNode)
		if !ok {
			return nil, nil, fmt.Errorf("a Menu holds MenuItem, MenuSeparator and Menu rows only (at %s)", b.Pos)
		}
		n.children = append(n.children, child)
	}
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

	triggers := map[tui.ActionID]func(){}
	var nodes []*menuNode
	next := 0
	var adopt func(n *menuNode) (widget.MenuItemModel, error)
	adopt = func(n *menuNode) (widget.MenuItemModel, error) {
		next++
		id := widget.ItemID("m" + strconv.Itoa(next))
		n.model.ID = id
		nodes = append(nodes, n)
		if n.kind == "MenuItem" && n.trigger != nil {
			act := tui.ActionID(id)
			n.model.Action = menuCommand{act}
			triggers[act] = n.trigger
		}
		if n.kind == "Menu" {
			n.model.Children = nil
			for _, c := range n.children {
				cm, err := adopt(c)
				if err != nil {
					return widget.MenuItemModel{}, err
				}
				n.model.Children = append(n.model.Children, cm)
			}
		}
		return n.model, nil
	}

	var model []widget.MenuItemModel
	var categories []menuCategory
	for _, c := range b.Children {
		n, ok := c.(*menuNode)
		if !ok || n.kind != "Menu" {
			return nil, nil, fmt.Errorf("a MenuBar holds Menu rows only (at %s)", b.Pos)
		}
		m, err := adopt(n)
		if err != nil {
			return nil, nil, err
		}
		model = append(model, m)
		// A top-level Menu's mnemonic is its ACCESS KEY: Alt+F opens File.
		if m.Hotkey != 0 {
			categories = append(categories, menuCategory{hotkey: m.Hotkey, id: m.ID})
		}
	}

	menuOpts = append(menuOpts, widget.WithActionExecutor(func(inv tui.ActionInvocation) bool {
		if inv.Action == nil {
			return false
		}
		fn, ok := triggers[inv.Action.ActionID()]
		if !ok {
			// Not ours — a row with no onTriggered. Reporting "not handled"
			// keeps that visible rather than swallowing it.
			return false
		}
		fn()
		return true
	}))
	menu := widget.NewMenu(menuOpts...)
	if err := menu.SetModel(model); err != nil {
		return nil, nil, fmt.Errorf("the menu is malformed: %w (at %s)", err, b.Pos)
	}
	for _, n := range nodes {
		n.owner = menu
	}

	var barOpts []widget.MenuBarOption
	if v, ok := b.SelfAttached["Dock.edge"]; ok {
		edge, err := dockEdges.read(v)
		if err != nil {
			return nil, nil, fmt.Errorf("Dock.edge: %w", err)
		}
		barOpts = append(barOpts, widget.WithBarPlacement(barPlacements[edge]))
	}
	return &menuBarNode{
		bar:        widget.NewMenuBar(menu, barOpts...),
		menu:       menu,
		categories: categories,
	}, consumed, nil
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
