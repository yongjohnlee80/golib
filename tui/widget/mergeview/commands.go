package mergeview

import (
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// Scope is a MergeView's own keys. JetBrains' keys first; then keys every terminal delivers, which
// the side panes leave to the view (the result pane, where they are typed, keeps its own).
const Scope keymap.Scope = "tui.mergeview"

var (
	// NextChange and PrevChange move to the next and the previous change; NextConflict to the next
	// conflict.
	NextChange   = kit.Cmd("tui.mergeview.nextChange", Scope, "F7", "]")
	PrevChange   = kit.Cmd("tui.mergeview.prevChange", Scope, "Shift+F7", "[")
	NextConflict = kit.Cmd("tui.mergeview.nextConflict", Scope, "Alt+F7", "}")
	// TakeLeft and TakeRight resolve the change at the cursor with that side's lines; TakeBoth with
	// the left's, then the right's; Dismiss as it is.
	TakeLeft  = kit.Cmd("tui.mergeview.takeLeft", Scope, "Ctrl+Shift+Right", "<")
	TakeRight = kit.Cmd("tui.mergeview.takeRight", Scope, "Ctrl+Shift+Left", ">")
	TakeBoth  = kit.Cmd("tui.mergeview.takeBoth", Scope, "+")
	Dismiss   = kit.Cmd("tui.mergeview.dismiss", Scope, "x")
	// Details shows or hides each pane's full location and revision under its header.
	Details = kit.Cmd("tui.mergeview.details", Scope, "F2", "i")

	commands = []keymap.Command{NextChange, PrevChange, NextConflict, TakeLeft, TakeRight, TakeBoth, Dismiss, Details}
)
