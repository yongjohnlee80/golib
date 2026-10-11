package widget

// WithCommandLine turns the ':' prompt on (the default) or off. Off, ':' reaches the application
// and RunCommand still runs lines from code.
func WithCommandLine(on bool) EditorOption { return func(e *Editor) { e.cmdline.SetEnabled(on) } }

// WithCommands gives the editor a command registry of its own in place of DefaultCommands (nil:
// none, and no prompt). To add to the defaults instead, use Commands().Add after construction.
func WithCommands(s *Commands) EditorOption { return WithCore(CoreCommands(s)) }

// WithOnWrite is how the editor's w, wq and x save (CoreOnWrite).
func WithOnWrite(fn func(quit bool) error) EditorOption { return WithCore(CoreOnWrite(fn)) }

// WithOnQuit is how the editor's q and q! quit (CoreOnQuit).
func WithOnQuit(fn func(force bool) error) EditorOption { return WithCore(CoreOnQuit(fn)) }

// SetCommandLineEnabled turns the ':' prompt on or off; off closes it if it is open.
func (e *Editor) SetCommandLineEnabled(on bool) { e.cmdline.SetEnabled(on) }

// CommandLineEnabled reports whether ':' opens the prompt.
func (e *Editor) CommandLineEnabled() bool { return e.cmdline.Enabled() }

// CommandLineOpen reports whether the prompt is showing.
func (e *Editor) CommandLineOpen() bool { return e.cmdline.IsOpen() }

// OpenCommandLine opens the prompt with initial in its field, as ':' does: a menu's or a key's way
// in. False when the prompt is off or there is nowhere to open it.
func (e *Editor) OpenCommandLine(initial string) bool { return e.cmdline.Open(initial) }

// Commands is the editor's command registry, to add to; nil when it has none.
func (e *Editor) Commands() *Commands { return e.core.Commands() }

// RunCommand runs one command line, as the prompt does (EditorCore.RunCommand).
func (e *Editor) RunCommand(line string) error { return e.core.RunCommand(line) }
