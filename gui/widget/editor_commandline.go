package widget

import tuiwidget "github.com/yongjohnlee80/golib/tui/widget"

// THE ':' PROMPT — the terminal Editor's own CommandLine module, opened over this editor's body:
// the same verbs, Tab completion and replace questions, drawn by this backend.

// WithCommandLine turns the ':' prompt on (the default) or off. Off, ':' reaches the application
// and RunCommand still runs lines from code.
func WithCommandLine(on bool) EditorOption { return func(c *editorConfig) { c.noCmd = !on } }

// SetCommandLineEnabled turns the ':' prompt on or off; off closes it if it is open.
func (e *Editor) SetCommandLineEnabled(on bool) { e.cmdline.SetEnabled(on) }

// CommandLineEnabled reports whether ':' opens the prompt.
func (e *Editor) CommandLineEnabled() bool { return e.cmdline.Enabled() }

// CommandLineOpen reports whether the prompt is showing.
func (e *Editor) CommandLineOpen() bool { return e.cmdline.IsOpen() }

// OpenCommandLine opens the prompt with initial in its field, as ':' does. False when the prompt
// is off or there is nowhere to open it.
func (e *Editor) OpenCommandLine(initial string) bool { return e.cmdline.Open(initial) }

// Commands is the editor's command registry, to add to; nil when it has none.
func (e *Editor) Commands() *tuiwidget.Commands { return e.core.Commands() }

// RunCommand runs one command line, as the prompt does.
func (e *Editor) RunCommand(line string) error { return e.core.RunCommand(line) }
