package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// DIALOG BUTTON BOX — Qt's DialogButtonBox: a dialog's own answers.
//
//	Dialog {
//	    Text { text: App.question }
//	    DialogButtonBox {
//	        Button { text: "&Save";    DialogButtonBox.buttonRole: DialogButtonBox.AcceptRole }
//	        Button { text: "&Discard"; DialogButtonBox.buttonRole: DialogButtonBox.DestructiveRole
//	                 onClicked: App.discard() }
//	        Button { text: "S&tay";    DialogButtonBox.buttonRole: DialogButtonBox.RejectRole }
//	    }
//	}
//
// Its Buttons become the dialog's, in order, each running its own onClicked
// and then its ROLE's answer, as Qt defines them: AcceptRole accepts
// (`accepted`, and the dialog closes); RejectRole rejects (`rejected`, and it
// closes — and Escape presses it); DestructiveRole is its button's `clicked`
// only, and the dialog closes without an answer — `closed`, neither accepted
// nor rejected.
//
// NO DEFAULT: Enter answers only through the button that has the keyboard,
// which is the first — so an irreversible choice is never one stray Enter away
// when the safe answer is listed first.

var buttonRoles = Enum{Scope: "DialogButtonBox", Values: []string{"AcceptRole", "RejectRole", "DestructiveRole"}}

// buttonBoxNode is a declared DialogButtonBox, before its Dialog takes it.
type buttonBoxNode struct {
	widget.Base
	buttons []*widget.Button
	roles   []string
}

func (*buttonBoxNode) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (*buttonBoxNode) Render(tui.Surface)                {}

func buildButtonBox(b Build) (tui.Component, []string, error) {
	n := &buttonBoxNode{}
	for i, c := range b.Children {
		btn, ok := c.(*widget.Button)
		if !ok {
			return nil, nil, fmt.Errorf("a DialogButtonBox holds Buttons only (at %s)", b.Pos)
		}
		v, ok := b.ChildAttached[i]["DialogButtonBox.buttonRole"]
		if !ok {
			return nil, nil, fmt.Errorf("a DialogButtonBox's Button needs its DialogButtonBox.buttonRole (at %s)", b.Pos)
		}
		role, err := buttonRoles.read(v)
		if err != nil {
			return nil, nil, fmt.Errorf("DialogButtonBox.buttonRole: %w", err)
		}
		n.buttons = append(n.buttons, btn)
		n.roles = append(n.roles, role)
	}
	if len(n.buttons) == 0 {
		return nil, nil, fmt.Errorf("a DialogButtonBox needs at least one Button (at %s)", b.Pos)
	}
	return n, nil, nil
}

// answer gives each of the box's buttons its role's answer, after its own.
func (n *buttonBoxNode) answer(d *dialogNode) []*widget.Button {
	for i, btn := range n.buttons {
		own := btn.OnActivate()
		then := func() {}
		switch n.roles[i] {
		case "AcceptRole":
			btn.SetRole(widget.ButtonRoleNormal) // no default: Enter is the focused button's
			then = d.accept
		case "RejectRole":
			btn.SetRole(widget.ButtonRoleCancel) // Escape presses it
			then = func() { d.modal.Dismiss(widget.DismissCancel) }
		case "DestructiveRole":
			btn.SetRole(widget.ButtonRoleNormal)
			then = func() { _ = d.close() }
		}
		btn.SetOnActivate(func() {
			if own != nil {
				own()
			}
			then()
		})
	}
	return n.buttons
}

var buttonBoxType = Type{
	Name:  "DialogButtonBox",
	Build: buildButtonBox,
	Enums: []Enum{buttonRoles},
}
