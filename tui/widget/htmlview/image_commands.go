package htmlview

import (
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// A scrollable Image's key commands (declared through internal/kit's Cmd and its helpers).

// ImageScope is a scrollable Image's keys.
const ImageScope keymap.Scope = "tui.image"

var (
	ImageDown     = kit.Cmd("tui.image.down", ImageScope, "Down", "J")
	ImageUp       = kit.Cmd("tui.image.up", ImageScope, "Up", "K")
	ImageRight    = kit.Cmd("tui.image.right", ImageScope, "Right", "L")
	ImageLeft     = kit.Cmd("tui.image.left", ImageScope, "Left", "H")
	ImagePageDown = kit.Cmd("tui.image.pageDown", ImageScope, "PgDown", "]", "Space")
	ImagePageUp   = kit.Cmd("tui.image.pageUp", ImageScope, "PgUp", "[")
	ImageBottom   = kit.Cmd("tui.image.bottom", ImageScope, "End", "Shift+G")
	ImageTop      = kit.Cmd("tui.image.top", ImageScope, "Home", "G")

	imageCommands = []keymap.Command{ImageDown, ImageUp, ImageRight, ImageLeft, ImagePageDown, ImagePageUp,
		ImageBottom, ImageTop}
)
