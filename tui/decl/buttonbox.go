package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// DIALOG BUTTON BOX — Qt's DialogButtonBox: declarative button containers for dialogs.
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
// # Architectural Role in Dialogs
//
// A DialogButtonBox serves as a declaration-only node within a [Dialog]. When the parent
// Dialog constructs its underlying [widget.Modal], it extracts the child Buttons from the
// DialogButtonBox, assigns each button its declared role, and installs them into the modal card.
//
// # Button Roles and Lifecycle Signals
//
// Each button's attached property `DialogButtonBox.buttonRole` determines its dismissal behavior:
//   - AcceptRole: Emits the Button's own `clicked` signal, followed by the Dialog's `accepted`
//     signal, and finally dismisses the dialog with `closed`.
//   - RejectRole: Emits the Button's own `clicked` signal, followed by the Dialog's `rejected`
//     signal, and dismisses the dialog with `closed`. The Escape key is wired to activate this role.
//   - DestructiveRole: Emits the Button's own `clicked` signal and immediately dismisses the dialog
//     with `closed` (neither `accepted` nor `rejected` is emitted).
//   - ActionRole: Emits the Button's own `clicked` signal only; the dialog stays open, as Qt's
//     ActionRole — an action on what the dialog shows (a manager's Add, Edit, Delete).
//
// # Enter and Focus Semantics
//
// A DialogButtonBox declares no default button:
//   - Enter answers nothing, regardless of which button currently holds focus: in a Dialog, Enter
//     reaches the dialog only when the focused control leaves it unclaimed, and answers only the
//     button explicitly named by `defaultButton`. No standard button is a default by being one, and
//     a DialogButtonBox declares none.
//   - Buttons answer by Space while holding focus, by their mnemonic shortcut, or by click.
//   - An irreversible or destructive answer is never one stray Enter away, whichever order
//     answers are listed in.

var buttonRoles = Enum{Scope: "DialogButtonBox", Values: []string{"AcceptRole", "RejectRole", "DestructiveRole", "ActionRole"}}

// boxRoles are Qt's roles, as the widget has them: the Modal answers for each.
var boxRoles = map[string]widget.ButtonRole{
	"AcceptRole":      widget.ButtonRoleAccept,
	"RejectRole":      widget.ButtonRoleReject,
	"DestructiveRole": widget.ButtonRoleDestructive,
	"ActionRole":      widget.ButtonRoleAction,
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
