package widget

import "github.com/yongjohnlee80/golib/tui"

// Each widget's accessible role (tui.RoleReporter): what it is, for a native style choosing its
// painter and for assistive technology. Widget-internal parts report nothing.

func (b *Button) AccessibleRole() tui.AccessibleRole      { return tui.RolePushButton }
func (t *TextInput) AccessibleRole() tui.AccessibleRole   { return tui.RoleEditableText }
func (t *TextArea) AccessibleRole() tui.AccessibleRole    { return tui.RoleTextArea }
func (s *Select[T]) AccessibleRole() tui.AccessibleRole   { return tui.RoleComboBox }
func (p *ProgressBar) AccessibleRole() tui.AccessibleRole { return tui.RoleProgressBar }
func (t *Tabs) AccessibleRole() tui.AccessibleRole        { return tui.RolePageTabList }
func (m *MenuBar) AccessibleRole() tui.AccessibleRole     { return tui.RoleMenuBar }
func (m *Menu) AccessibleRole() tui.AccessibleRole        { return tui.RolePopupMenu }
func (s *StatusBar) AccessibleRole() tui.AccessibleRole   { return tui.RoleStatusBar }
func (x *Box) AccessibleRole() tui.AccessibleRole         { return tui.RoleGrouping }
func (s *Split) AccessibleRole() tui.AccessibleRole       { return tui.RoleSplitter }
func (r *Resizable) AccessibleRole() tui.AccessibleRole   { return tui.RoleSplitter }
func (m *Modal) AccessibleRole() tui.AccessibleRole       { return tui.RoleDialog }
func (t *Toasts) AccessibleRole() tui.AccessibleRole      { return tui.RoleNotification }
func (l *List[T]) AccessibleRole() tui.AccessibleRole     { return tui.RoleList }
func (t *Table[T]) AccessibleRole() tui.AccessibleRole    { return tui.RoleTable }
func (t *Tree) AccessibleRole() tui.AccessibleRole        { return tui.RoleTree }
func (t *Text) AccessibleRole() tui.AccessibleRole        { return tui.RoleStaticText }
func (m *Image) AccessibleRole() tui.AccessibleRole       { return tui.RoleImage }
func (t *Terminal) AccessibleRole() tui.AccessibleRole    { return tui.RoleTerminal }

var (
	_ tui.RoleReporter = (*Button)(nil)
	_ tui.RoleReporter = (*TextInput)(nil)
	_ tui.RoleReporter = (*TextArea)(nil)
	_ tui.RoleReporter = (*Select[string])(nil)
	_ tui.RoleReporter = (*ProgressBar)(nil)
	_ tui.RoleReporter = (*Tabs)(nil)
	_ tui.RoleReporter = (*MenuBar)(nil)
	_ tui.RoleReporter = (*Menu)(nil)
	_ tui.RoleReporter = (*StatusBar)(nil)
	_ tui.RoleReporter = (*Box)(nil)
	_ tui.RoleReporter = (*Split)(nil)
	_ tui.RoleReporter = (*Resizable)(nil)
	_ tui.RoleReporter = (*Modal)(nil)
	_ tui.RoleReporter = (*Toasts)(nil)
	_ tui.RoleReporter = (*List[string])(nil)
	_ tui.RoleReporter = (*Table[string])(nil)
	_ tui.RoleReporter = (*Tree)(nil)
	_ tui.RoleReporter = (*Text)(nil)
	_ tui.RoleReporter = (*Image)(nil)
	_ tui.RoleReporter = (*Terminal)(nil)
)
