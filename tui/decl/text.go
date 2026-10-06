package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
)

// TRANSLATED TEXT — Qt's qsTrId.
//
//	MenuItem { text: qsTrId("editor.file.save"); onTriggered: App.saveFile() }
//	Dialog   { title: qsTrId("editor.quit.title") }
//
// qsTrId(id) is Qt's id-based translation. It yields the MESSAGE, a tui.Message, not its
// text: the widget it reaches looks the message up in the App's language each time it is laid
// out, so App.SetLanguage changes every label with no binding re-evaluated and nothing
// republished. A host does the same by publishing a tui.Message as a source value. A property
// that shows UI text takes either a string, shown as written, or a message; a property holding
// document content (an Editor's text) takes a string only.

// UIText is what a UI text property holds: Plain text, shown as written, or a catalog
// Message, shown in the App's language. A custom widget reads one with [UITextField] or
// [UITextSetter], so qsTrId works on its properties as on the built-in ones.
type UIText struct {
	Plain   string
	Message tui.Message
}

// IsMessage reports whether the text is a catalog message rather than plain text.
func (t UIText) IsMessage() bool { return t.Message != (tui.Message{}) }

// Empty reports whether nothing was written: no message, and no text.
func (t UIText) Empty() bool { return !t.IsMessage() && t.Plain == "" }

// UITextField reads a constructor property holding UI text into dst: a string, or
// qsTrId("id").
func UITextField(dst *UIText) Field { return into(dst, textOf) }

// UITextSetter is a runtime property holding UI text: plain text goes to plain, and a message
// to msg, which shows it in the App's language.
func UITextSetter[W any](plain func(W, string), msg func(W, tui.Message)) Setter {
	return textSetter(widgetName[W](), plain, msg)
}

// textOf reads UI text: a string, or the message object qsTrId produced, as colorOf reads a
// colour or the colour object a binding produced.
func textOf(v qml.SpecValue) (UIText, error) {
	if m, ok := v.Obj.(tui.Message); v.Kind == qml.SpecValueObject && ok {
		return UIText{Message: m}, nil
	}
	if v.Kind != qml.SpecValueString {
		return UIText{}, fmt.Errorf("want a string, got %s (at %s); a catalog message is written qsTrId(\"id\")", v.Kind, v.Pos)
	}
	return UIText{Plain: v.Raw}, nil
}

// textSetter is [setter] for UI text: a message to msg, plain text to plain.
func textSetter[W any](what string, plain func(W, string), msg func(W, tui.Message)) Setter {
	return setter(what, textOf, func(w W, t UIText) {
		if t.IsMessage() {
			msg(w, t.Message)
			return
		}
		plain(w, t.Plain)
	})
}

// qsTrId is Qt's id-based translation function, as a document calls it. It returns the
// message rather than its text, so its value depends on its argument alone, as a pure
// function's must; the widget resolves it.
func qsTrId(args []qml.SpecValue) (qml.SpecValue, error) {
	if len(args) != 1 || args[0].Kind != qml.SpecValueString || args[0].Raw == "" {
		return qml.SpecValue{}, fmt.Errorf("qsTrId takes one string, a message id: qsTrId(\"editor.menu.file\")")
	}
	return qml.SpecValue{Kind: qml.SpecValueObject, Obj: tui.Msg(args[0].Raw)}, nil
}
