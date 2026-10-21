package widget

import (
	"image/color"
	"testing"
)

// A view picks an adaptive colour's dark or light leaf as tui/style judges darkness: a mid grey
// #777777 is light, #6e6e6e dark.
func TestIsDarkFollowsStyle(t *testing.T) {
	if isDark(color.NRGBA{0x77, 0x77, 0x77, 0xff}) {
		t.Error("#777777 read dark; style.Dark reads it light")
	}
	if !isDark(color.NRGBA{0x6e, 0x6e, 0x6e, 0xff}) {
		t.Error("#6e6e6e read light; style.Dark reads it dark")
	}
}
