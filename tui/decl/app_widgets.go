package decl

import (
	"fmt"
	"strconv"
	"strings"

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
		{Name: "Window", Build: buildWindow, Setters: map[string]Setter{
			// Qt's Window.color: the background under everything, where no widget paints
			"color": setter("a Window", colorOf, (*windowNode).setColor),
			// Qt's Window.minimumWidth / minimumHeight, in cells: on a smaller screen the App
			// shows its too-small screen instead of the layout (tui.App.SetMinimumSize)
			"minimumWidth":  setter("a Window", wholeNumber, (*windowNode).setMinimumWidth),
			"minimumHeight": setter("a Window", wholeNumber, (*windowNode).setMinimumHeight),
		}},
		{Name: "Frame", Build: buildFrame, Ctor: []string{"title"}, restyle: restyleFrame, Setters: map[string]Setter{
			"title": textSetter("a Frame", (*widget.Box).SetTitle, (*widget.Box).SetTitleMessage),
			// golib's: at most this many columns, border included, centred — a page, not the screen
			"maximumWidth": setter("a Frame", func(v qml.SpecValue) (int, error) {
				n, err := numberOf(v)
				return int(n), err
			}, (*widget.Box).SetMaximumWidth),
		}},
		{Name: "SyntaxHighlighter", Build: buildSyntaxHighlighter, restyle: restyleSyntax, Setters: syntaxSetters()},
		// Qt Quick's Image, without its source: the host gives it a PNG (widget.Image.SetPNG), and
		// it shows on a terminal that draws images (tui.Capabilities.KittyGraphics)
		{Name: "Image", Build: func(b Build) (tui.Component, []string, error) {
			consumed, err := readProps(b.Props, map[string]field{})
			return widget.NewImage(), consumed, err
		}, Setters: map[string]Setter{
			// golib's: shown at its width and scrolled by the keys and the wheel (widget.Image)
			"scrollable": setter("an Image", boolOf, (*widget.Image).SetScrollable),
		}},
		{Name: "Terminal", Build: buildTerminal, Ctor: []string{"command", "dir", "scrollback"}, restyle: restyleTerminal,
			Setters: map[string]Setter{
				// command and dir take effect at the next start(): a host learns where to start
				// the program after building it, and moves it when the user changes workspace.
				"command":     setter("a Terminal", stringOf, func(t *widget.Terminal, p string) { t.SetCommand(p) }),
				"dir":         setter("a Terminal", stringOf, (*widget.Terminal).SetDir),
				"vimKeys":     setter("a Terminal", boolOf, (*widget.Terminal).SetVimKeys),
				"themeColors": setter("a Terminal", boolOf, (*widget.Terminal).SetThemeColors),
			},
			Methods: map[string]Method{
				"start": NoArgMethod((*widget.Terminal).Start),
				"stop":  NoArgMethod(func(t *widget.Terminal) error { t.Stop(); return nil }),
				"focus": NoArgMethod(func(t *widget.Terminal) error {
					if ctx := t.Context(); ctx != nil {
						ctx.RequestFocus()
					}
					return nil
				}),
			},
			Signals:   map[string][]string{"exited": {"code"}, "titleChanged": {"title"}, "modeChanged": {"mode"}},
			Destroyed: func(c tui.Component) { c.(*widget.Terminal).Stop() },
		},
		{Name: "Editor", Build: buildEditor, Ctor: []string{"text", "wrap"}, restyle: restyleEditor, Setters: map[string]Setter{
			"keyset":   setter("an Editor", keysets.read, (*widget.Editor).SetKeyset),
			"readOnly": setter("an Editor", boolOf, (*widget.Editor).SetReadOnly),
			// golib's: a right-click menu with Undo, Redo, Copy, Cut and Paste, opened at the pointer
			"contextMenu": setter("an Editor", boolOf, (*widget.Editor).SetContextMenu),
			// Qt's TextEdit.wrapMode, as a bool: true wraps long lines at the editor's width
			"wrap": setter("an Editor", boolOf, func(e *widget.Editor, v bool) {
				if v {
					e.SetWrap(widget.WrapSoft)
				} else {
					e.SetWrap(widget.WrapNone)
				}
			}),
			// golib's: each line's number in a gutter at the left
			"lineNumbers": setter("an Editor", boolOf, (*widget.Editor).SetLineNumbers),
			// golib's: the text cursor's colour, a theme's accent; the terminal's own otherwise
			"cursorColor": setter("an Editor", colorOf, (*widget.Editor).SetCursorColor),
			// golib's: the line numbers' colour, a theme's dim tone; muted and faint otherwise
			"lineNumberColor": setter("an Editor", colorOf, (*widget.Editor).SetLineNumberColor),
			// Qt's TextEdit.text: setting it replaces the buffer, as a load does.
			"text": setter("an Editor", stringOf, (*widget.Editor).SetValue),
			// golib's: a vertical guide at this column (1-based), where text is meant to wrap; 0 for none
			"ruler": setter("an Editor", func(v qml.SpecValue) (int, error) {
				n, err := numberOf(v)
				return int(n), err
			}, (*widget.Editor).SetRuler),
			// Qt's TextEdit.cursorPosition: characters from the start, a line
			// break one; the cursor goes there and into view.
			"cursorPosition": setter("an Editor", func(v qml.SpecValue) (int, error) {
				n, err := numberOf(v)
				return int(n), err
			}, (*widget.Editor).SetCursorPosition),
		}},
		{Name: "StatusBar", Build: buildStatusBar, restyle: restyleStatusBar, adopt: adoptStatusBarChild, Setters: map[string]Setter{
			"left": textSetter("a StatusBar", statusSegment((*widget.StatusBar).SetLeft),
				statusMessage((*widget.StatusBar).SetLeftMessage)),
			"center": textSetter("a StatusBar", statusSegment((*widget.StatusBar).SetCenter),
				statusMessage((*widget.StatusBar).SetCenterMessage)),
			"right": textSetter("a StatusBar", statusSegment((*widget.StatusBar).SetRight),
				statusMessage((*widget.StatusBar).SetRightMessage)),
		}},
		{Name: "MenuBar", Build: buildMenuBar, Ctor: []string{"vimNavigation"}, restyle: restyleMenuBar,
			Setters: map[string]Setter{"autoHide": setter("a MenuBar", boolOf, (*menuBarNode).setAutoHide)}},
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
		{Name: "FolderDialog", Build: buildFolderDialog,
			Ctor:    []string{"title", "helpText", "dim", "preview"},
			restyle: restyleDialog,
			Setters: map[string]Setter{
				"currentFolder":  setter("a FolderDialog", stringOf, (*dialogNode).setFolder),
				"selectedFolder": setter("a FolderDialog", stringOf, (*dialogNode).setSelected),
			},
			Methods:   dialogMethods,
			Signals:   map[string][]string{"accepted": {"selectedFolder"}},
			Destroyed: releaseDialog},
		{Name: "Dialog", Build: buildDialog,
			Ctor:    []string{"title", "helpText", "dim", "width", "standardButtons", "defaultButton"},
			restyle: restyleDialog,
			Setters: map[string]Setter{
				"title":    textSetter("a Dialog", (*dialogNode).setTitle, (*dialogNode).setTitleMessage),
				"helpText": textSetter("a Dialog", (*dialogNode).setHelp, (*dialogNode).setHelpMessage),
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
	// `StatusBar.permanent` is written on a StatusBar's child and read by the bar: false puts it
	// at the left end (Qt's addWidget), true (the default) at the right (addPermanentWidget).
	RegisterAttached(r, "StatusBar", "permanent")
	Honour(r, "StatusBar", "StatusBar")
}

var dockEdges = enum[tui.DockEdge]{values: map[string]tui.DockEdge{
	"Top":    tui.DockTop,
	"Bottom": tui.DockBottom,
	"Left":   tui.DockLeft,
	"Right":  tui.DockRight,
}}

// drawerEdges are where a Drawer opens: an edge, or golib's Center.
var drawerEdges = enum[tui.DockEdge]{values: map[string]tui.DockEdge{
	"Top":    tui.DockTop,
	"Bottom": tui.DockBottom,
	"Left":   tui.DockLeft,
	"Right":  tui.DockRight,
	"Center": tui.DockCenter,
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
	var title UIText
	consumed, err := readProps(b.Props, map[string]field{"title": into(&title, textOf)})
	if err != nil {
		return nil, nil, err
	}
	var opts []widget.BoxOption
	if title.Plain != "" {
		opts = append(opts, widget.WithTitle(title.Plain))
	}
	box := widget.NewBox(b.Children[0], opts...)
	if title.IsMessage() {
		box.SetTitleMessage(title.Message)
	}
	return box, consumed, nil
}

// ---------------------------------------------------------------- Editor

// EditorOf returns the editor an `Editor` declaration built, for a host that
// reaches it through [decl.Tree.NodeByID] and [Adapter.Component].
func EditorOf(c tui.Component) (*widget.Editor, bool) {
	e, ok := c.(*widget.Editor)
	return e, ok
}

// buildEditor builds a widget.Editor and connects its notifications to the
// document's signals, `modeChanged`, `textChanged` and `cursorPositionChanged`.
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
		widget.WithOnCursorPositionChange(b.Emitter("cursorPositionChanged")),
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

// buildTerminal makes a Terminal. command is the program (the user's $SHELL
// when empty), dir its working directory, scrollback its history's length;
// vimKeys and themeColors are settable while it runs. start() runs the
// program, stop() hangs it up, focus() gives it the keyboard.
func buildTerminal(b Build) (tui.Component, []string, error) {
	var command, dir string
	scrollback := -1.0
	vimKeys, themeColors := false, true
	consumed, err := readProps(b.Props, map[string]field{
		"command":     into(&command, stringOf),
		"dir":         into(&dir, stringOf),
		"scrollback":  into(&scrollback, numberOf),
		"vimKeys":     into(&vimKeys, boolOf),
		"themeColors": into(&themeColors, boolOf),
	})
	if err != nil {
		return nil, nil, err
	}
	exited, titled, moded := b.EmitterWith("exited"), b.EmitterWith("titleChanged"), b.EmitterWith("modeChanged")
	opts := []widget.TerminalOption{
		widget.WithVimKeys(vimKeys),
		widget.WithThemeColors(themeColors),
		widget.WithOnExit(func(code int) {
			exited(qml.SpecValue{Kind: qml.SpecValueNumber, Raw: strconv.Itoa(code)})
		}),
		widget.WithOnTitle(func(title string) { titled(strValue(title)) }),
		widget.WithOnMode(func(m widget.TerminalMode) { moded(strValue(strings.ToLower(m.String()))) }),
	}
	if command != "" {
		opts = append(opts, widget.WithCommand(command))
	}
	if dir != "" {
		opts = append(opts, widget.WithDir(dir))
	}
	if scrollback >= 0 {
		opts = append(opts, widget.WithScrollback(int(scrollback)))
	}
	return widget.NewTerminal(opts...), consumed, nil
}

// ---------------------------------------------------------------- StatusBar

// buildStatusBar makes the bar; its children are its widgets (Quick Controls 1's StatusBar held
// its items as children): each at its own width, in order, the segments sharing the rest of the
// row. A child is a permanent widget (Qt's QStatusBar.addPermanentWidget), at the bar's right end,
// unless it says StatusBar.permanent: false: then it is a normal widget (addWidget), at the left end.
func buildStatusBar(b Build) (tui.Component, []string, error) {
	consumed, err := readProps(b.Props, map[string]field{})
	if err != nil {
		return nil, nil, err
	}
	sb := widget.NewStatusBar()
	for i, c := range b.Children {
		if err := adoptStatusBarChild(sb, c, b.ChildAttached[i]); err != nil {
			return nil, nil, fmt.Errorf("%w (at %s)", err, b.Pos)
		}
	}
	return sb, consumed, nil
}

// adoptStatusBarChild adds one child to a StatusBar: permanent, at the right end, unless its
// StatusBar.permanent is false.
func adoptStatusBarChild(parent, child tui.Component, attached map[string]qml.SpecValue) error {
	sb := parent.(*widget.StatusBar)
	permanent := true
	if v, ok := attached["StatusBar.permanent"]; ok {
		var err error
		if permanent, err = boolOf(v); err != nil {
			return fmt.Errorf("StatusBar.permanent: %w", err)
		}
	}
	if permanent {
		sb.Add(child)
	} else {
		sb.AddWidget(child)
	}
	return nil
}

// statusSegment adapts one of the StatusBar's segment setters, which take an
// optional per-segment style this vocabulary does not set: the bar's palette
// colours every segment.
func statusSegment(set func(*widget.StatusBar, string, ...style.Style)) func(*widget.StatusBar, string) {
	return func(sb *widget.StatusBar, text string) { set(sb, text) }
}

// statusMessage is statusSegment for a catalog message.
func statusMessage(set func(*widget.StatusBar, tui.Message, ...style.Style)) func(*widget.StatusBar, tui.Message) {
	return func(sb *widget.StatusBar, m tui.Message) { set(sb, m) }
}

// dialogMethods are what a handler can call on any dialog by its id.
var dialogMethods = map[string]Method{
	"open":  method("a dialog", (*dialogNode).open),
	"close": method("a dialog", (*dialogNode).close),
}
