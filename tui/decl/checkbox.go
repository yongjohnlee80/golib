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
// It holds its Button rather than embedding one, so nothing makes a Button an embedded base.
type checkBoxNode struct {
	tui.MultiChild
	ctx     *tui.Context
	btn     *widget.Button
	text    UIText
	shown   string // text as last resolved: a message in the App's language
	checked bool
	toggled func()
	clicked func()
}

// buildCheckBox builds an unchecked box with no text; the text and checked travel the runtime
// path, as a Button's label does.
func buildCheckBox(b Build) (tui.Component, []string, error) {
	n := &checkBoxNode{toggled: b.Emitter("toggled"), clicked: b.Emitter("clicked")}
	n.btn = widget.NewButton("", widget.WithButtonDecoration("", ""), widget.WithOnActivate(n.activated),
		widget.WithButtonStyle(checkBoxStyle(palette{})))
	n.Label("CheckBox")
	n.Add(n.btn)
	n.relabel()
	return n, nil, nil
}

func (n *checkBoxNode) Init(ctx *tui.Context) {
	n.ctx = ctx
	n.MultiChild.Init(ctx)
}

// Layout is the Button's: the box and its text. A message is looked up here, as every widget
// looks one up as it is laid out, so the box follows App.SetLanguage.
func (n *checkBoxNode) Layout(c tui.Constraints) tui.Size {
	if n.text.IsMessage() {
		if s := n.ctx.Translate(n.text.Message); s != n.shown {
			n.shown = s
			n.relabel()
		}
	}
	sz := n.ctx.LayoutChild(n.btn, c)
	n.ctx.PlaceChild(n.btn, tui.Rect{W: sz.W, H: sz.H})
	return sz
}

func (n *checkBoxNode) Render(tui.Surface) {}

// HandleEvent leaves every event to the Button.
func (n *checkBoxNode) HandleEvent(tui.Event) bool { return false }

// AccessibleRole: a QML CheckBox is a checkbox, though a widget.Button draws it on a terminal.
func (n *checkBoxNode) AccessibleRole() tui.AccessibleRole { return tui.RoleCheckBox }

// Checked reports whether the box is checked.
func (n *checkBoxNode) Checked() bool { return n.checked }

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
	label, key, _ := tui.ParseMnemonic(n.shown)
	n.btn.SetLabel(box + label)
	n.btn.SetMnemonic(key)
}

func (n *checkBoxNode) setText(s string) {
	n.text, n.shown = UIText{Plain: s}, s
	n.relabel()
}

// setTextMessage shows m beside the box, in the App's language from the next layout.
func (n *checkBoxNode) setTextMessage(m tui.Message) {
	n.text = UIText{Message: m}
	if n.ctx != nil {
		n.shown = n.ctx.Translate(m)
	} else {
		n.shown = ""
	}
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
	c.(*checkBoxNode).btn.WithStyle(checkBoxStyle(p))
}

var checkBoxType = Type{
	Name:    "CheckBox",
	Build:   buildCheckBox,
	restyle: restyleCheckBox,
	Setters: map[string]Setter{
		"text":    textSetter("a CheckBox", (*checkBoxNode).setText, (*checkBoxNode).setTextMessage),
		"checked": setter("a CheckBox", boolOf, (*checkBoxNode).setChecked),
		"enabled": setter("a CheckBox", boolOf, func(n *checkBoxNode, v bool) { n.btn.SetEnabled(v) }),
	},
}
