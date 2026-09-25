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
// A DECLARATION: its Buttons become the dialog's, in order, each carrying its
// role, and the widget's Modal answers for them (widget ANSWERS) exactly as it
// does for a native dialog — each button's own onClicked, then its role's
// answer: AcceptRole accepts (`accepted`, and the dialog closes); RejectRole
// rejects (`rejected`, and it closes — and Escape presses it); DestructiveRole
// closes without an answer (`closed`, neither accepted nor rejected).
//
// NO DEFAULT: a box declares none, and in a dialog Enter is the dialog's — its
// default button's — so Enter answers nothing, whichever button has focus. A
// button answers by Space while it has focus, by its mnemonic, or by a click.
// An irreversible answer is never one stray Enter away, whatever order the
// answers are listed in.

var buttonRoles = Enum{Scope: "DialogButtonBox", Values: []string{"AcceptRole", "RejectRole", "DestructiveRole"}}

// boxRoles are Qt's roles, as the widget has them: the Modal answers for each.
var boxRoles = map[string]widget.ButtonRole{
	"AcceptRole":      widget.ButtonRoleAccept,
	"RejectRole":      widget.ButtonRoleReject,
	"DestructiveRole": widget.ButtonRoleDestructive,
}

// buttonBoxNode is a declared DialogButtonBox, before its Dialog takes it: its
// Buttons, each carrying its role.
type buttonBoxNode struct {
	widget.Base
	buttons []*widget.Button
}

func (*buttonBoxNode) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (*buttonBoxNode) declarationOnly()                  {}
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
		btn.SetRole(boxRoles[role])
		n.buttons = append(n.buttons, btn)
	}
	if len(n.buttons) == 0 {
		return nil, nil, fmt.Errorf("a DialogButtonBox needs at least one Button (at %s)", b.Pos)
	}
	return n, nil, nil
}

var buttonBoxType = Type{
	Name:  "DialogButtonBox",
	Build: buildButtonBox,
	Enums: []Enum{buttonRoles},
}
