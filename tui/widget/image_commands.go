package widget

import "github.com/yongjohnlee80/golib/tui/keymap"

// A scrollable Image's key commands (commands.go declares cmd and the shared helpers).

// ImageScope is a scrollable Image's keys.
const ImageScope keymap.Scope = "tui.image"

var (
	ImageDown     = cmd("tui.image.down", ImageScope, "Down", "J")
	ImageUp       = cmd("tui.image.up", ImageScope, "Up", "K")
	ImageRight    = cmd("tui.image.right", ImageScope, "Right", "L")
	ImageLeft     = cmd("tui.image.left", ImageScope, "Left", "H")
	ImagePageDown = cmd("tui.image.pageDown", ImageScope, "PgDown", "]", "Space")
	ImagePageUp   = cmd("tui.image.pageUp", ImageScope, "PgUp", "[")
	ImageBottom   = cmd("tui.image.bottom", ImageScope, "End", "Shift+G")
	ImageTop      = cmd("tui.image.top", ImageScope, "Home", "G")

	imageCommands = []keymap.Command{ImageDown, ImageUp, ImageRight, ImageLeft, ImagePageDown, ImagePageUp,
		ImageBottom, ImageTop}
)
