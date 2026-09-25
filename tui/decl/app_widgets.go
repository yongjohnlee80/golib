package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/parse/qml"
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
// Colours come in through `palette.<role>` (see palette.go), bound to a theme
// module the document imports — so a document's structure and its theme change
// independently, and the theme by its import line alone.

func appTypes() []Type {
	return []Type{
		{Name: "Window", Build: buildWindow},
		{Name: "Frame", Build: buildFrame, Ctor: []string{"title"}, restyle: restyleFrame, Setters: map[string]Setter{
			"title": setter("a Frame", stringOf, (*widget.Box).SetTitle),
		}},
		{Name: "SyntaxHighlighter", Build: buildSyntaxHighlighter, restyle: restyleSyntax, Setters: syntaxSetters()},
		{Name: "Editor", Build: buildEditor, Ctor: []string{"text", "wrap"}, restyle: restyleEditor, Setters: map[string]Setter{
			"keyset":   setter("an Editor", keysets.read, (*widget.Editor).SetKeyset),
			"readOnly": setter("an Editor", boolOf, (*widget.Editor).SetReadOnly),
			// Qt's TextEdit.text: setting it replaces the buffer, as a load does.
			"text": setter("an Editor", stringOf, (*widget.Editor).SetValue),
		}},
		{Name: "StatusBar", Build: buildStatusBar, restyle: restyleStatusBar, Setters: map[string]Setter{
			"left":   setter("a StatusBar", stringOf, statusSegment((*widget.StatusBar).SetLeft)),
			"center": setter("a StatusBar", stringOf, statusSegment((*widget.StatusBar).SetCenter)),
			"right":  setter("a StatusBar", stringOf, statusSegment((*widget.StatusBar).SetRight)),
		}},
		{Name: "MenuBar", Build: buildMenuBar, Ctor: []string{"vimNavigation"}, restyle: restyleMenuBar},
		{Name: "Menu", Build: buildMenu, Ctor: []string{"title", "align"}},
		{Name: "MenuItem", Build: buildMenuItem, Ctor: []string{"text", "checkable", "group", "shortcut"},
			Setters: map[string]Setter{
				"checked": setter("a menu row", boolOf, (*menuNode).setChecked),
				"enabled": setter("a menu row", boolOf, (*menuNode).setEnabled),
			},
			Getters: map[string]Getter{
				"checked": func(c tui.Component) (qml.SpecValue, error) {
					return boolValue(c.(*menuNode).model.Checked), nil
				},
			}},
		{Name: "MenuSeparator", Build: buildMenuSeparator},
		{Name: "Shortcut", Build: buildShortcut, Ctor: []string{"sequence"}},
		{Name: "FileDialog", Build: buildFileDialog,
			Ctor:    []string{"title", "helpText", "dim", "fileMode", "preview"},
			restyle: restyleDialog,
			Setters: map[string]Setter{
				"currentFolder": setter("a FileDialog", stringOf, (*dialogNode).setFolder),
				"selectedFile":  setter("a FileDialog", stringOf, (*dialogNode).setSelected),
			},
			Methods:   dialogMethods,
			Signals:   map[string][]string{"accepted": {"selectedFile"}},
			Destroyed: releaseDialog},
		{Name: "Dialog", Build: buildDialog,
			Ctor:    []string{"title", "helpText", "dim", "width", "standardButtons", "defaultButton"},
			restyle: restyleDialog,
			Setters: map[string]Setter{
				"title":    setter("a Dialog", stringOf, (*dialogNode).setTitle),
				"helpText": setter("a Dialog", stringOf, (*dialogNode).setHelp),
			},
			Methods:   dialogMethods,
			Destroyed: releaseDialog},
	}
}

// registerStdAttached declares the attaching schemas the standard vocabulary
// uses, and which containers honour them.
func registerStdAttached(r *Registry) {
	// `Dock.edge` is written on a child and read by the Window that docks it.
	RegisterAttached(r, "Dock", "edge")
	Honour(r, "Window", "Dock")
	// `DialogButtonBox.buttonRole` is written on a Button and read by its box.
	RegisterAttached(r, "DialogButtonBox", "buttonRole")
	Honour(r, "DialogButtonBox", "DialogButtonBox")
	// `Layout.fillHeight` / `Layout.fillWidth` are written on a Flex's child —
	// Qt's ColumnLayout / RowLayout — and read by the Flex.
	RegisterAttached(r, "Layout", "fillHeight", "fillWidth")
	Honour(r, "Flex", "Layout")
}

var dockEdges = enum[tui.DockEdge]{values: map[string]tui.DockEdge{
	"Top":    tui.DockTop,
	"Bottom": tui.DockBottom,
	"Left":   tui.DockLeft,
	"Right":  tui.DockRight,
}}

var keysets = enum[widget.Keyset]{values: map[string]widget.Keyset{
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
	highlighters, err := editorChildren(b)
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
	e := widget.NewEditor(opts...)
	for _, h := range highlighters {
		h.attach(e)
	}
	return e, consumed, nil
}

// ---------------------------------------------------------------- StatusBar

func buildStatusBar(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("StatusBar takes no children (at %s)", b.Pos)
	}
	consumed, err := readProps(b.Props, map[string]field{})
	if err != nil {
		return nil, nil, err
	}
	return widget.NewStatusBar(), consumed, nil
}

// statusSegment adapts one of the StatusBar's segment setters, which take an
// optional per-segment style this vocabulary does not set: the bar's palette
// colours every segment.
func statusSegment(set func(*widget.StatusBar, string, ...style.Style)) func(*widget.StatusBar, string) {
	return func(sb *widget.StatusBar, text string) { set(sb, text) }
}

// dialogMethods are what a handler can call on any dialog by its id.
var dialogMethods = map[string]Method{
	"open":  method("a dialog", (*dialogNode).open),
	"close": method("a dialog", (*dialogNode).close),
}
