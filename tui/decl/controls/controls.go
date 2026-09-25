// Package controls adds Qt Quick Controls' TextField and Popup to the
// golib/tui QML vocabulary.
//
//	p, err := tuidecl.NewProgram(tuidecl.Types(controls.Types()...), …)
//
//	Popup {                                  // a command prompt
//	    id: prompt; modal: true
//	    TextField {
//	        placeholderText: "command"
//	        onAccepted: App.run(text)
//	    }
//	}
//	Shortcut { sequence: ":"; onActivated: prompt.open() }
//
// It is written the way a program adds its own widgets — with nothing but
// tuidecl's exported contract ([tuidecl.Type] and its helpers), in a package
// of its own so the compiler holds it to that. Whatever the two types need
// that a consumer could not write is a gap in the contract, and was fixed
// there: enumerations under their own scope (TextInput.Password), a way for a
// Window to hand its overlay to what opens over it ([tuidecl.Overlaid]), and
// palette roles a consumer type can wear ([tuidecl.Type.Restyle]).
package controls

import (
	"errors"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Types are TextField and Popup, for [tuidecl.Types] or [tuidecl.WithTypes].
func Types() []tuidecl.Type { return []tuidecl.Type{TextField, Popup} }

// ---------------------------------------------------------------- TextField

// EchoMode is TextInput's echoMode enumeration, spelled as Qt spells it:
// `echoMode: TextInput.Password`.
var EchoMode = tuidecl.Enum{Scope: "TextInput", Values: []string{"Normal", "Password"}}

// TextField is Qt Quick Controls' single-line text field, over
// widget.TextInput:
//
//	text             the value; set it, bind it — typing does not write back
//	placeholderText  shown while the value is empty
//	echoMode         TextInput.Normal or TextInput.Password
//	accepted(text)   Enter
//	textEdited(text) every edit the user makes
//
// A handler reads `text` as it would in Qt, where a handler runs in its
// object's scope: `onAccepted: App.run(text)`.
//
// It wears `base` and `text` for the field, `highlight`/`highlightedText`
// for a selection.
var TextField = tuidecl.Type{
	Name:  "TextField",
	Build: buildTextField,
	Ctor:  []string{"placeholderText", "echoMode"},
	Setters: map[string]tuidecl.Setter{
		"text": tuidecl.StringSetter((*widget.TextInput).SetValue),
	},
	Signals: map[string][]string{"accepted": {"text"}, "textEdited": {"text"}},
	Enums:   []tuidecl.Enum{EchoMode},
	Restyle: func(c tui.Component, p tuidecl.Palette) {
		st := widget.TextInputStyles{Text: p.Look(tuidecl.RoleBase, tuidecl.RoleText)}
		if _, ok := p.Color(tuidecl.RoleHighlight); ok {
			st.Selection = p.Look(tuidecl.RoleHighlight, tuidecl.RoleHighlightedText).Reverse(false)
		}
		c.(*widget.TextInput).WithStyles(st)
	},
}

func buildTextField(b tuidecl.Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, errors.New("a TextField takes no children")
	}
	var placeholder, echo string
	consumed, err := tuidecl.ReadProps(b.Props, map[string]tuidecl.Field{
		"placeholderText": tuidecl.StringField(&placeholder),
		"echoMode":        tuidecl.EnumField(&echo, EchoMode),
	})
	if err != nil {
		return nil, nil, err
	}
	accepted, edited := b.EmitterWith("accepted"), b.EmitterWith("textEdited")
	raise := func(emit func(...qml.SpecValue)) func(string) {
		return func(text string) {
			if v, err := tuidecl.Value(text); err == nil {
				emit(v)
			}
		}
	}
	opts := []widget.TextInputOption{
		widget.WithOnSubmit(raise(accepted)),
		widget.WithOnEdit(raise(edited)),
	}
	if placeholder != "" {
		opts = append(opts, widget.WithPlaceholder(placeholder))
	}
	if echo == "Password" {
		opts = append(opts, widget.WithMask('•'))
	}
	return widget.NewTextInput(opts...), consumed, nil
}

// ---------------------------------------------------------------- Popup

// Popup is Qt Quick Controls' floating surface, over widget.Float: content
// that opens over the Window rather than taking a place in it.
//
//	modal     trap the keyboard while open, and give it back when closed
//	dim       shade the screen behind it
//	open()    close()
//	opened()  closed()
//
// Escape closes it while it has the keyboard, as Qt's default closePolicy
// does. It is centred, as `anchors.centerIn` places a Qt Popup; positioning
// by x and y needs expressions this evaluator does not run.
var Popup = tuidecl.Type{
	Name:  "Popup",
	Build: buildPopup,
	Ctor:  []string{"modal", "dim"},
	Methods: map[string]tuidecl.Method{
		"open":  tuidecl.NoArgMethod((*popupNode).open),
		"close": tuidecl.NoArgMethod((*popupNode).close),
	},
	Destroyed: func(c tui.Component) { c.(*popupNode).detach() },
}

// popupNode is a Popup: the Float, and the overlay it is attached to.
type popupNode struct {
	widget.Base
	float  *widget.Float
	frame  *popupFrame
	host   *widget.OverlayHost
	opened func()
	closed func()
}

var _ tuidecl.Overlaid = (*popupNode)(nil)

func buildPopup(b tuidecl.Build) (tui.Component, []string, error) {
	if len(b.Children) != 1 {
		return nil, nil, errors.New("a Popup holds exactly one child, its content")
	}
	var modal, dim bool
	consumed, err := tuidecl.ReadProps(b.Props, map[string]tuidecl.Field{
		"modal": tuidecl.BoolField(&modal),
		"dim":   tuidecl.BoolField(&dim),
	})
	if err != nil {
		return nil, nil, err
	}
	n := &popupNode{opened: b.Emitter("opened"), closed: b.Emitter("closed")}
	n.frame = newPopupFrame(b.Children[0], n)
	n.float = widget.NewFloat(n.frame, widget.WithModal(modal), widget.WithDimBackground(dim))
	if b.Overlay != nil {
		n.SetOverlay(b.Overlay, nil)
	}
	return n, consumed, nil
}

// SetOverlay implements [tuidecl.Overlaid]: the Popup's Float becomes one of
// the host's layers, hidden until opened.
func (n *popupNode) SetOverlay(host *widget.OverlayHost, _ func()) {
	if n.host == host {
		return
	}
	n.detach()
	n.host = host
	host.Attach(n.float)
}

func (n *popupNode) detach() {
	if n.host != nil {
		n.host.Detach(n.float)
		n.host = nil
	}
}

var errNoOverlay = errors.New("a Popup opens over a Window, and this one is not in one; " +
	"inside a Go program, give the adapter WithOverlay(host)")

func (n *popupNode) open() error {
	if n.host == nil {
		return errNoOverlay
	}
	if n.float.Shown() {
		return nil
	}
	n.float.Show()
	n.opened()
	return nil
}

func (n *popupNode) close() error {
	if n.float.Shown() {
		n.float.Hide()
		n.closed()
	}
	return nil
}

// A Popup takes no place in the layout: it opens over it.
func (n *popupNode) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (n *popupNode) Render(tui.Surface)                {}

// popupFrame is the Popup's content as the Float shows it, closing on Escape
// while the keyboard is inside. A Container — on tui.MultiChild, the base
// golib's containers share — so a modal Popup's first focusable widget is
// found when it opens.
type popupFrame struct {
	tui.MultiChild
	ctx   *tui.Context
	owner *popupNode
}

// Init keeps the context for layout, then mounts the child.
func (f *popupFrame) Init(ctx *tui.Context) {
	f.ctx = ctx
	f.MultiChild.Init(ctx)
}

func newPopupFrame(child tui.Component, owner *popupNode) *popupFrame {
	f := &popupFrame{owner: owner}
	f.Label("Popup")
	f.Add(child)
	return f
}

// Layout gives the one child what the Float offers, at its own size.
func (f *popupFrame) Layout(c tui.Constraints) tui.Size {
	sz := c.Constrain(tui.Size{})
	for _, child := range f.Items() {
		sz = f.ctx.LayoutChild(child, c)
		f.ctx.PlaceChild(child, tui.Rect{W: sz.W, H: sz.H})
	}
	return sz
}

func (f *popupFrame) Render(tui.Surface) {}

func (f *popupFrame) HandleEvent(ev tui.Event) bool {
	k, ok := ev.(tui.KeyEvent)
	if ok && k.Kind == tui.KeyPress && k.Code == tui.KeyEscape && k.Mods.Chord() == 0 {
		_ = f.owner.close()
		return true
	}
	return false
}
