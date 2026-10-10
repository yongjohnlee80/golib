package decl

import (
	"fmt"
	"github.com/yongjohnlee80/golib/indent"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// WHAT AN EDITOR DECLARATION MEANS, for any editor widget: the standard Editor and a style's
// read the same declaration the same way, so a document cannot tell them apart.

// EditorViews are the Editor's `view`: Editor.Raw, the source as written, and Editor.Rendered,
// drawn by the Editor's renderer. An editor with no Rendered view stays Raw.
var EditorViews = Enum{Scope: "Editor", Values: []string{"Raw", "Rendered"}}

// EditorDecl is an Editor declaration as an editor widget's builder needs it.
type EditorDecl struct {
	Text       string       // `text`, the buffer the editor starts with
	Wrap       bool         // `wrap`, long lines wrapped at the editor's width
	AutoIndent bool         // `autoIndent`, source-language typing indentation
	IndentUnit string       // `indentUnit`, optional whitespace override
	Renderer   RendererSpec // the renderer child, nil when none is declared
	Document   DocumentSpec // the document view child (HTMLDocumentView), nil when none is declared
	// Consumed are the constructor properties read: what the builder returns as consumed.
	Consumed []string

	highlighters []*syntaxNode
	modeChanged  func()
	textChanged  func()
	cursorMoved  func()
}

// ReadEditor reads an Editor declaration: its constructor properties, its children (at most one
// SyntaxHighlighter and one renderer) and its signals' emitters.
func ReadEditor(b Build) (EditorDecl, error) {
	var d EditorDecl
	consumed, err := readProps(b.Props, map[string]field{
		"text":       into(&d.Text, stringOf),
		"wrap":       into(&d.Wrap, boolOf),
		"autoIndent": into(&d.AutoIndent, boolOf),
		"indentUnit": into(&d.IndentUnit, stringOf),
	})
	if err != nil {
		return d, err
	}
	d.Consumed = consumed
	if !indent.Whitespace(d.IndentUnit) {
		return d, fmt.Errorf("indentUnit: only spaces and tabs are allowed (at %s)", b.Pos)
	}
	if d.highlighters, d.Renderer, d.Document, err = editorChildren(b); err != nil {
		return d, err
	}
	d.modeChanged = b.Emitter("modeChanged")
	d.textChanged = b.Emitter("textChanged")
	d.cursorMoved = b.Emitter("cursorPositionChanged")
	return d, nil
}

// CoreOptions are the declaration's behaviour for the editor's core: its first text, and the
// listeners that raise modeChanged, textChanged and cursorPositionChanged.
func (d EditorDecl) CoreOptions() []widget.CoreOption {
	modeChanged := d.modeChanged
	return []widget.CoreOption{
		widget.CoreOnModeChange(func(widget.EditorMode) { modeChanged() }),
		widget.CoreOnChange(d.textChanged),
		widget.CoreOnCursorPositionChange(d.cursorMoved),
		widget.CoreInitialText(d.Text),
		widget.CoreAutoIndent(d.AutoIndent),
		widget.CoreIndentUnit(d.IndentUnit),
	}
}

// Attach binds the declared SyntaxHighlighter, if any, to the editor's core.
func (d EditorDecl) Attach(core *widget.EditorCore) {
	for _, h := range d.highlighters {
		h.attach(core)
	}
}

// The behaviour an Editor's runtime properties set, as any editor widget offers it.
type (
	keysetter    interface{ SetKeyset(widget.Keyset) }
	readOnlyer   interface{ SetReadOnly(bool) }
	valueSetter  interface{ SetValue(string) }
	cursorPlacer interface{ SetCursorPosition(int) }
	coreEditor   interface{ Core() *widget.EditorCore }
)

// EditorSetters are the Editor's behaviour properties, for any editor widget with the methods
// they call: `keyset` (SetKeyset), `readOnly` (SetReadOnly), `text` (SetValue, replacing the
// buffer as a load does) and `cursorPosition` (SetCursorPosition: characters from the start, a
// line break one). Each widget adds its own view properties beside them.
func EditorSetters() map[string]Setter {
	return map[string]Setter{
		"keyset":     setter("an Editor", keysets.read, keysetter.SetKeyset),
		"readOnly":   setter("an Editor", boolOf, readOnlyer.SetReadOnly),
		"text":       setter("an Editor", stringOf, valueSetter.SetValue),
		"autoIndent": setter("an Editor", boolOf, func(e coreEditor, v bool) { e.Core().SetAutoIndent(v) }),
		"indentUnit": func(c tui.Component, v qml.SpecValue) error {
			unit, err := stringOf(v)
			if err != nil {
				return err
			}
			e, ok := c.(coreEditor)
			if !ok {
				return fmt.Errorf("indentUnit needs an Editor")
			}
			return e.Core().SetIndentUnit(unit)
		},
		"indentMarks": setter("an Editor", indentMarksOf, func(e coreEditor, m widget.IndentMarks) { e.Core().SetIndentMarks(m) }),
		"cursorPosition": setter("an Editor", func(v qml.SpecValue) (int, error) {
			n, err := numberOf(v)
			return int(n), err
		}, cursorPlacer.SetCursorPosition),
	}
}

// indentMarksOf reads `indentMarks`: which lines show their indentation as dimmed marks.
func indentMarksOf(v qml.SpecValue) (widget.IndentMarks, error) {
	s, err := stringOf(v)
	if err != nil {
		return 0, err
	}
	switch s {
	case "off":
		return widget.IndentMarksOff, nil
	case "cursorLine":
		return widget.IndentMarksCursorLine, nil
	case "all":
		return widget.IndentMarksAll, nil
	}
	return 0, fmt.Errorf("indentMarks is \"off\", \"cursorLine\" or \"all\", not %q", s)
}

// editorViewSetters are the standard Editor's view properties: its cell layout's.
func editorViewSetters() map[string]Setter {
	return map[string]Setter{
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
		// golib's: a vertical guide at this column (1-based), where text is meant to wrap; 0 for none
		"ruler": setter("an Editor", func(v qml.SpecValue) (int, error) {
			n, err := numberOf(v)
			return int(n), err
		}, (*widget.Editor).SetRuler),
		// golib's: Editor.Raw or Editor.Rendered. The cell Editor has no Rendered view: it
		// accepts the value, so one document runs under any style, and stays Raw.
		"view": EnumSetter(EditorViews, func(*widget.Editor, string) {}),
		// golib's: whether the Rendered view is available, for the document shown (Markdown's
		// renderer and a Go file: false). The cell Editor has none: it accepts the value.
		"renderedEnabled": setter("an Editor", boolOf, func(*widget.Editor, bool) {}),
	}
}

// editorSetters are the standard Editor's runtime properties: behaviour and view.
func editorSetters() map[string]Setter {
	s := EditorSetters()
	for name, set := range editorViewSetters() {
		s[name] = set
	}
	return s
}

// editorType is the standard Editor.
var editorType = Type{Name: "Editor", Build: buildEditor, Ctor: []string{"text", "wrap", "autoIndent", "indentUnit"}, restyle: restyleEditor,
	Setters: editorSetters(), Enums: []Enum{EditorViews}}

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
	d, err := ReadEditor(b)
	if err != nil {
		return nil, nil, err
	}
	// The cell Editor has no Rendered view: a renderer spec is carried, not drawn.
	opts := []widget.EditorOption{widget.WithCore(d.CoreOptions()...)}
	if d.Wrap {
		opts = append(opts, widget.WithEditorWrap(widget.WrapSoft))
	}
	e := widget.NewEditor(opts...)
	d.Attach(e.Core())
	return e, d.Consumed, nil
}
