package widget

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
