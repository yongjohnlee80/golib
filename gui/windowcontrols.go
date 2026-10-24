package gui

import (
	"image/color"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// WindowControlColor is a window control's circle colour by what it does, macOS's: close red,
// maximize green, minimize amber. A panel's title bar and a tab strip's controls (paintTabs) both
// paint their controls in it.
func WindowControlColor(op widget.WindowOperation) color.NRGBA {
	switch op {
	case widget.WindowMaximize:
		return color.NRGBA{R: 0x28, G: 0xc8, B: 0x40, A: 0xff}
	case widget.WindowMinimize:
		return color.NRGBA{R: 0xfe, G: 0xbc, B: 0x2e, A: 0xff}
	}
	return color.NRGBA{R: 0xff, G: 0x5f, B: 0x57, A: 0xff}
}
