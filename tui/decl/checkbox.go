package decl

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// CHECKBOX — Qt Quick Controls' CheckBox: a box and its text, checked or not.
//
//	CheckBox { text: "&Lexical"; checked: App.lexical; onToggled: App.toggleLexical() }
//
// It draws as "[x] text" or "[ ] text", in the colours of what it sits on (palette.window and
// windowText, as a Text), with no button's brackets or fill: the box is the affordance. Space,
// Enter or a click toggles it, as Qt's does, and raises toggled() then clicked(). `&` marks the
// mnemonic, as a Button's text does. A host that refuses a change sets checked back.
type checkBoxNode struct {
	*widget.Button
	text    string
	checked bool
	toggled func()
	clicked func()
}

// buildCheckBox builds an unchecked box with no text; the text and checked travel the runtime
// path, as a Button's label does.
func buildCheckBox(b Build) (tui.Component, []string, error) {
	n := &checkBoxNode{toggled: b.Emitter("toggled"), clicked: b.Emitter("clicked")}
	n.Button = widget.NewButton("", widget.WithButtonDecoration("", ""), widget.WithOnActivate(n.activated),
		widget.WithButtonStyle(checkBoxStyle(palette{})))
	n.relabel()
	return n, nil, nil
}

// activated is the box toggled by the user.
func (n *checkBoxNode) activated() {
	n.checked = !n.checked
	n.relabel()
	n.toggled()
	n.clicked()
}

// relabel draws the box and the text, keeping the text's mnemonic.
func (n *checkBoxNode) relabel() {
	box := "[ ] "
	if n.checked {
		box = "[x] "
	}
	label, key, _ := mnemonic(n.text)
	n.SetLabel(box + label)
	n.SetMnemonic(key)
}

func (n *checkBoxNode) setText(s string) {
	n.text = s
	n.relabel()
}

func (n *checkBoxNode) setChecked(v bool) {
	n.checked = v
	n.relabel()
}

// checkBoxStyle is a box in its surroundings' colours: focus reverses them, as a Button's does,
// and a disabled box is faint.
func checkBoxStyle(p palette) *widget.ButtonStyle {
	normal := p.look(roleWindow, roleWindowText)
	focused := normal.Reverse(true).Bold(true)
	return widget.NewButtonStyleFull(normal, focused, normal.Faint(true), focused.Underline(true))
}

func restyleCheckBox(c tui.Component, p palette) {
	c.(*checkBoxNode).WithStyle(checkBoxStyle(p))
}

var checkBoxType = Type{
	Name:    "CheckBox",
	Build:   buildCheckBox,
	restyle: restyleCheckBox,
	Setters: map[string]Setter{
		"text":    setter("a CheckBox", stringOf, (*checkBoxNode).setText),
		"checked": setter("a CheckBox", boolOf, (*checkBoxNode).setChecked),
		"enabled": setter("a CheckBox", boolOf, func(n *checkBoxNode, v bool) { n.SetEnabled(v) }),
	},
}
