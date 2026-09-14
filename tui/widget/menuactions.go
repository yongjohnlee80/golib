package widget

import "github.com/yongjohnlee80/golib/tui"

// THE MENU'S OWN ACTION VOCABULARY.
//
// A model row is not a node. Hit-testing targets the Menu, so the runtime's
// generic leaf recogniser only ever sees the Menu's NodeID and can neither
// identify nor arm an ItemID. Menu therefore runs its own press-arm /
// release-activate machine — and the way it stays honest is that the machine is
// driven by named actions rather than by raw event handling, so the same
// vocabulary serves the mouse, the keyboard, a UserEvent and DoAction.
//
// The six are deliberately NOT one cancel action with a flag. An earlier design
// mapped motion-outside, release-elsewhere, capture loss, Escape, hide and
// unmount all onto a single cancel, which cannot work: moving off a row must
// KEEP the pointer grabbed so moving back re-arms, releasing elsewhere must END
// the grab but leave the menu open, and Escape must actually close it. Those are
// three incompatible effects.

// MenuArmAction presses a row: select it, remember it as armed, take the
// pointer. Carries the row so the Menu knows which one, since the runtime
// cannot.
type MenuArmAction struct{ ItemID ItemID }

// ActionID returns the stable published name of this action.
func (MenuArmAction) ActionID() tui.ActionID { return "menu.arm" }

// MenuSelectAction moves the selection to a row. It re-arms ONLY when the row
// is the one originally pressed, so dragging across a menu highlights rows
// without making any of them the thing a release would activate.
type MenuSelectAction struct{ ItemID ItemID }

// ActionID returns the stable published name of this action.
func (MenuSelectAction) ActionID() tui.ActionID { return "menu.select" }

// MenuDisarmAction clears the armed row while RETAINING the pointer. Visual
// only: the gesture is still running, and moving back over the pressed row
// re-arms it.
type MenuDisarmAction struct{}

// ActionID returns the stable published name of this action.
func (MenuDisarmAction) ActionID() tui.ActionID { return "menu.disarm" }

// MenuActivateAction runs a row. Shared by mouse release, Enter, Space, a
// mnemonic, a UserEvent and DoAction — there is no pointer-specific activation
// path, which is what makes keyboard and mouse provably equivalent.
type MenuActivateAction struct{ ItemID ItemID }

// ActionID returns the stable published name of this action.
func (MenuActivateAction) ActionID() tui.ActionID { return "menu.activate" }

// MenuCancelAction ends the pointer gesture and closes NOTHING.
//
// Idempotent, and reports true only when it actually ended an active gesture —
// so a caller cannot tell "cancelled a drag" from "there was nothing to cancel"
// by accident, and the runtime's cleanup paths can call it unconditionally.
type MenuCancelAction struct{}

// ActionID returns the stable published name of this action.
func (MenuCancelAction) ActionID() tui.ActionID { return "menu.cancel" }

// MenuCloseAction closes open levels: all of them, or only the deepest.
//
// Separate from cancel because it is usable with no pointer gesture in play —
// from the keyboard, a UserEvent or DoAction — and because cancelling a drag and
// closing a menu are different things that happened to be conflated once.
type MenuCloseAction struct {
	// All closes every level; false closes only the deepest, which is what Left
	// does inside a cascade.
	All bool
}

// ActionID returns the stable published name of this action.
func (MenuCloseAction) ActionID() tui.ActionID { return "menu.close" }

// MenuActivatedEvent reports that a model row was activated.
//
// Model rows need their own event because ControlActivatedEvent identifies a
// NODE, and every row of one Menu shares that node — a subscriber could see that
// "the menu" was activated but never which row.
//
// Emitted EXACTLY ONCE after execution, including when Handled is false: a
// command whose action was nil or refused still happened as far as the user is
// concerned, and a listener counting activations should see it. Disabled,
// hidden, separator and submenu rows emit nothing at all.
type MenuActivatedEvent struct {
	// Owner is the Menu's node.
	Owner tui.NodeID
	// ItemID is the row that was activated.
	ItemID ItemID
	// Origin is the runtime's own record of what produced it.
	Origin tui.ActionOrigin
	// Handled is what the executor returned; false when either the row's Action
	// or the executor was nil. The menu closes only when this is true.
	Handled bool
}

// MenuSelectionChangedEvent reports that the highlighted row moved.
type MenuSelectionChangedEvent struct {
	// Owner is the Menu's node.
	Owner tui.NodeID
	// ItemID is the newly selected row; empty when nothing is selected.
	ItemID ItemID
}
