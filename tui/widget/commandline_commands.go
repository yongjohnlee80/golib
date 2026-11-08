package widget

import "github.com/yongjohnlee80/golib/tui/keymap"

// The command line's key commands (commands.go declares cmd and the shared helpers).

// CommandLineScope is the command-line prompt's own keys: Tab and Shift+Tab complete, and with the
// candidates' dropdown open, Down and Up move through it as they do. Each with any other modifier too.
const CommandLineScope keymap.Scope = "tui.cmdline"

var (
	noShift   = func(shift, _, _ bool) bool { return !shift }
	withShift = func(shift, _, _ bool) bool { return shift }

	CmdlineComplete     = cmd("tui.cmdline.complete", CommandLineScope, mods("Tab", noShift)...)
	CmdlineCompletePrev = cmd("tui.cmdline.completePrev", CommandLineScope, mods("Tab", withShift)...)
	CmdlineNext         = cmd("tui.cmdline.next", CommandLineScope, mods("Down", anyCombo)...) // the dropdown open
	CmdlinePrev         = cmd("tui.cmdline.prev", CommandLineScope, mods("Up", anyCombo)...)

	cmdlineCommands = []keymap.Command{CmdlineComplete, CmdlineCompletePrev, CmdlineNext, CmdlinePrev}
)

// CommandLineReplaceScope is the keys of a command's replace question (:s with c): Y, N and Q, either
// case, with any modifier, as its handler took them.
const CommandLineReplaceScope keymap.Scope = "tui.cmdline.replace"

var (
	CmdlineReplaceYes  = cmd("tui.cmdline.replace.yes", CommandLineReplaceScope, mods("Y", anyCombo)...)
	CmdlineReplaceNo   = cmd("tui.cmdline.replace.no", CommandLineReplaceScope, mods("N", anyCombo)...)
	CmdlineReplaceQuit = cmd("tui.cmdline.replace.quit", CommandLineReplaceScope, mods("Q", anyCombo)...)

	cmdlineReplaceCommands = []keymap.Command{CmdlineReplaceYes, CmdlineReplaceNo, CmdlineReplaceQuit}
)
