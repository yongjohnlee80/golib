package widget

import tuiwidget "github.com/yongjohnlee80/golib/tui/widget"

// Deprecated declarations: kept so a consumer still builds, NOT maintained, and removed in the next
// minor version. Each names its replacement. The editor's core is reached through Core(), and
// configured with WithCore; its command line through CommandLine(), and its menu through
// ContextMenu() (editor-D17).

// SetCommandLineEnabled turns the ':' prompt on or off; off closes it if it is open.
//
// Deprecated: use e.CommandLine().SetEnabled. Not maintained; removed in the next minor version.
func (e *Editor) SetCommandLineEnabled(on bool) { e.cmdline.SetEnabled(on) }

// CommandLineEnabled reports whether ':' opens the prompt.
//
// Deprecated: use e.CommandLine().Enabled. Not maintained; removed in the next minor version.
func (e *Editor) CommandLineEnabled() bool { return e.cmdline.Enabled() }

// CommandLineOpen reports whether the prompt is showing.
//
// Deprecated: use e.CommandLine().IsOpen. Not maintained; removed in the next minor version.
func (e *Editor) CommandLineOpen() bool { return e.cmdline.IsOpen() }

// OpenCommandLine opens the prompt with initial in its field, as ':' does. False when the prompt
// is off or there is nowhere to open it.
//
// Deprecated: use e.CommandLine().Open. Not maintained; removed in the next minor version.
func (e *Editor) OpenCommandLine(initial string) bool { return e.cmdline.Open(initial) }

// WithCommandLine turns the ':' prompt on (the default) or off. Off, ':' reaches the application
// and RunCommand still runs lines from code.
//
// Deprecated: use e.CommandLine().SetEnabled(on) after NewEditor. Not maintained; removed in the next minor version.
func WithCommandLine(on bool) EditorOption { return func(c *editorConfig) { c.noCmd = !on } }

// Commands is the editor's command registry, to add to; nil when it has none.
//
// Deprecated: use e.Core().Commands. Not maintained; removed in the next minor version.
func (e *Editor) Commands() *tuiwidget.Commands { return e.core.Commands() }

// RunCommand runs one command line, as the prompt does.
//
// Deprecated: use e.Core().RunCommand. Not maintained; removed in the next minor version.
func (e *Editor) RunCommand(line string) error { return e.core.RunCommand(line) }
