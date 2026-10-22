package widget

import "github.com/yongjohnlee80/golib/tui"

// Deprecated declarations: kept so a consumer still builds, NOT maintained, and removed in the next
// minor version. Each names its replacement.

// Minimizable did collapse the panel to its one-row title bar, and back.
//
// Deprecated: a Panel no longer collapses. Minimizing is the window behaviour's: pass
// WithWindowCapabilities(tuiwidget.WithWindowMinimize(collector)), and the minimize control hands
// the panel to that collector (a taskbar or a dock), which hides it until restored. Minimizable now
// does nothing: a panel given only it shows no minimize control. Not maintained; removed in the
// next minor version.
func Minimizable() PanelOption { return func(*Panel) {} }

// TitleLeading puts ws at the title bar's left.
//
// Deprecated: use WithTitleLeading. Not maintained; removed in the next minor version.
func TitleLeading(ws ...tui.Component) PanelOption { return WithTitleLeading(ws...) }

// TitleTrailing puts ws at the title bar's right, before the window controls.
//
// Deprecated: use WithTitleTrailing. Not maintained; removed in the next minor version.
func TitleTrailing(ws ...tui.Component) PanelOption { return WithTitleTrailing(ws...) }

// TitleLeadingShare caps the leading widgets at share of the bar's width.
//
// Deprecated: use WithTitleLeadingShare. Not maintained; removed in the next minor version.
func TitleLeadingShare(share float64) PanelOption { return WithTitleLeadingShare(share) }

// Movable lets a drag of the title bar move the panel.
//
// Deprecated: use WithMovable. Not maintained; removed in the next minor version.
func Movable() PanelOption { return WithMovable() }

// Resizable lets a drag of the right edge, the bottom edge or their corner resize the panel.
//
// Deprecated: use WithResizable. Not maintained; removed in the next minor version.
func Resizable() PanelOption { return WithResizable() }

// Closable shows a close control that calls onClose.
//
// Deprecated: use WithClosable. Not maintained; removed in the next minor version.
func Closable(onClose func()) PanelOption { return WithClosable(onClose) }

// Maximizable shows a control, and takes a title-bar double-click, that fills the Float's area.
//
// Deprecated: use WithMaximizable. Not maintained; removed in the next minor version.
func Maximizable() PanelOption { return WithMaximizable() }
