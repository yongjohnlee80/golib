package tui

// AccessibleRole is what a component is: for a native style choosing its painter, and for
// assistive technology. The names follow QAccessible::Role.
type AccessibleRole uint8

const (
	RoleNone AccessibleRole = iota // reports nothing
	RolePushButton
	RoleCheckBox
	RoleRadioButton
	RoleEditableText // a single line
	RoleTextArea     // several lines
	RoleComboBox
	RoleProgressBar
	RolePageTabList // a tab bar
	RoleMenuBar
	RolePopupMenu
	RoleStatusBar
	RoleGrouping // a framed, titled box
	RoleSplitter
	RoleDialog
	RoleNotification
	RoleList
	RoleTable
	RoleTree
	RoleStaticText
	RoleImage
	RoleTerminal
)

// RoleReporter is a component that says what it is. Optional. The method is not named Role: a
// widget may already have a Role of its own (widget.Button's dialog-button role).
type RoleReporter interface {
	Component
	AccessibleRole() AccessibleRole
}

// RoleOf is c's accessible role, or RoleNone.
func RoleOf(c Component) AccessibleRole {
	if r, ok := c.(RoleReporter); ok {
		return r.AccessibleRole()
	}
	return RoleNone
}
