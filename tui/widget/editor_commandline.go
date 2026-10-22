package widget

// CommandLine is the editor's ':' prompt: on unless turned off (CommandLine().SetEnabled(false)),
// opened by ':' or Open, with the core's command registry (Core().Commands).
func (e *Editor) CommandLine() *CommandLine { return e.cmdline }
