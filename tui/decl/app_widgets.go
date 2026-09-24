package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// THE APPLICATION VOCABULARY — what an editor-shaped program is made of.
//
//	Window {                              an OverlayHost around a Dock
//	    MenuBar { Dock.edge: Tui.Top      pinned by the ATTACHED property
//	        Menu { title: "&File"
//	            MenuItem { text: "&New"; onTriggered: App.newFile() }
//	        }
//	    }
//	    Frame { Editor { id: editor; focus: true } }
//	    StatusBar { Dock.edge: Tui.Bottom }
//	}
//
// LAYOUT ONLY. Nothing here takes a colour: how these things look is a separate
// concern with its own module, so a document's structure and its theme can
// change independently.

func appTypes() []widgetType {
	return []widgetType{
		{name: "Window", build: buildWindow},
		{name: "Frame", build: buildFrame, ctor: []string{"title"}},
		{name: "Editor", build: buildEditor, ctor: []string{"text", "wrap"}, setters: map[string]Setter{
			"keyset":   setter("an Editor", keysets.read, (*widget.Editor).SetKeyset),
			"readOnly": setter("an Editor", boolOf, (*widget.Editor).SetReadOnly),
		}},
		{name: "StatusBar", build: buildStatusBar, setters: map[string]Setter{
			"left":   setter("a StatusBar", stringOf, statusSegment((*widget.StatusBar).SetLeft)),
			"center": setter("a StatusBar", stringOf, statusSegment((*widget.StatusBar).SetCenter)),
			"right":  setter("a StatusBar", stringOf, statusSegment((*widget.StatusBar).SetRight)),
		}},
		{name: "MenuBar", build: buildMenuBar, ctor: []string{"vimNavigation"}},
		{name: "Menu", build: buildMenu, ctor: []string{"title", "align"}},
		{name: "MenuItem", build: buildMenuItem, ctor: []string{"text", "checkable", "group", "shortcut"},
			setters: map[string]Setter{
				"checked": setter("a menu row", boolOf, (*menuNode).setChecked),
				"enabled": setter("a menu row", boolOf, (*menuNode).setEnabled),
			}},
		{name: "MenuSeparator", build: buildMenuSeparator},
		{name: "Shortcut", build: buildShortcut, ctor: []string{"sequence"}},
	}
}

// registerStdAttached declares the attaching schemas the standard vocabulary
// uses, and which containers honour them.
func registerStdAttached(r *Registry) {
	// `Dock.edge` is written on a child and read by the Window that docks it.
	RegisterAttached(r, "Dock", "edge")
	Honour(r, "Window", "Dock")
}

var dockEdges = enum[tui.DockEdge]{prop: "Dock.edge", values: map[string]tui.DockEdge{
	"Top":    tui.DockTop,
	"Bottom": tui.DockBottom,
	"Left":   tui.DockLeft,
	"Right":  tui.DockRight,
}}

var keysets = enum[widget.Keyset]{prop: "keyset", values: map[string]widget.Keyset{
	"Vim":      widget.KeysetVim,
	"Nano":     widget.KeysetNano,
	"Standard": widget.KeysetStandard,
}}

// ---------------------------------------------------------------- Frame

// buildFrame boxes exactly one child.
func buildFrame(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 1 {
		return nil, nil, fmt.Errorf("Frame needs exactly 1 child, got %d (at %s)", len(b.Children), b.Pos)
	}
	var title string
	consumed, err := readProps(b.Props, map[string]field{"title": into(&title, stringOf)})
	if err != nil {
		return nil, nil, err
	}
	var opts []widget.BoxOption
	if title != "" {
		opts = append(opts, widget.WithTitle(title))
	}
	return widget.NewBox(b.Children[0], opts...), consumed, nil
}

// ---------------------------------------------------------------- Editor

// EditorOf returns the editor an `Editor` declaration built, for a host that
// reaches it through [decl.Tree.NodeByID] and [Adapter.Component].
func EditorOf(c tui.Component) (*widget.Editor, bool) {
	e, ok := c.(*widget.Editor)
	return e, ok
}

// buildEditor builds a widget.Editor and connects its two notifications to the
// document's signals, `modeChanged` and `textChanged`.
//
// The widget reports both itself, through constructor options. An earlier cut
// WRAPPED the editor in a type that compared its state around every event —
// which embedded the widget and overrode its methods, and Go has no virtual
// dispatch: any editor method calling its own receiver would have bypassed the
// wrapper silently. It also re-read the whole buffer on every keystroke to see
// whether it had changed, which the widget already knew.
func buildEditor(b Build) (tui.Component, []string, error) {
	var text string
	var wrap bool
	consumed, err := readProps(b.Props, map[string]field{
		"text": into(&text, stringOf),
		"wrap": into(&wrap, boolOf),
	})
	if err != nil {
		return nil, nil, err
	}
	modeChanged := b.Emitter("modeChanged")
	opts := []widget.EditorOption{
		widget.WithOnModeChange(func(widget.EditorMode) { modeChanged() }),
		widget.WithOnChange(b.Emitter("textChanged")),
		widget.WithInitialText(text),
	}
	if wrap {
		opts = append(opts, widget.WithEditorWrap(widget.WrapSoft))
	}
	return widget.NewEditor(opts...), consumed, nil
}

// ---------------------------------------------------------------- StatusBar

func buildStatusBar(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("StatusBar takes no children (at %s)", b.Pos)
	}
	return widget.NewStatusBar(), nil, nil
}

// statusSegment adapts one of the StatusBar's segment setters, which take an
// optional style this vocabulary does not set: how the bar looks is the
// theme's concern, not the layout's.
func statusSegment(set func(*widget.StatusBar, string, ...style.Style)) func(*widget.StatusBar, string) {
	return func(sb *widget.StatusBar, text string) { set(sb, text) }
}
