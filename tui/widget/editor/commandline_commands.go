package editor

import (
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// The command line's key commands (declared through internal/kit's Cmd and its helpers).

// CommandLineScope is the command-line prompt's own keys: Tab and Shift+Tab complete, and with the
// candidates' dropdown open, Down and Up move through it as they do. Each with any other modifier too.
const CommandLineScope keymap.Scope = "tui.cmdline"

var (
	noShift   = func(shift, _, _ bool) bool { return !shift }
	withShift = func(shift, _, _ bool) bool { return shift }

	CmdlineComplete     = kit.Cmd("tui.cmdline.complete", CommandLineScope, kit.Mods("Tab", noShift)...)
	CmdlineCompletePrev = kit.Cmd("tui.cmdline.completePrev", CommandLineScope, kit.Mods("Tab", withShift)...)
	CmdlineNext         = kit.Cmd("tui.cmdline.next", CommandLineScope, kit.Mods("Down", kit.AnyCombo)...) // the dropdown open
	CmdlinePrev         = kit.Cmd("tui.cmdline.prev", CommandLineScope, kit.Mods("Up", kit.AnyCombo)...)

	cmdlineCommands = []keymap.Command{CmdlineComplete, CmdlineCompletePrev, CmdlineNext, CmdlinePrev}
)

// CommandLineReplaceScope is the keys of a command's replace question (:s with c): Y, N and Q, either
// case, with any modifier, as its handler took them.
const CommandLineReplaceScope keymap.Scope = "tui.cmdline.replace"

var (
	CmdlineReplaceYes  = kit.Cmd("tui.cmdline.replace.yes", CommandLineReplaceScope, kit.Mods("Y", kit.AnyCombo)...)
	CmdlineReplaceNo   = kit.Cmd("tui.cmdline.replace.no", CommandLineReplaceScope, kit.Mods("N", kit.AnyCombo)...)
	CmdlineReplaceQuit = kit.Cmd("tui.cmdline.replace.quit", CommandLineReplaceScope, kit.Mods("Q", kit.AnyCombo)...)

	cmdlineReplaceCommands = []keymap.Command{CmdlineReplaceYes, CmdlineReplaceNo, CmdlineReplaceQuit}
)
