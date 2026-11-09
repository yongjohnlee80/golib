package widgettest

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
)

// pngURI is a data: URI of a w × h PNG.
func PNGURI(t TB, w, h int) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

var Kitty = tui.Capabilities{ColorProfile: tui.ProfileTrueColor, KittyGraphics: tui.TriYes}

// lineOf is the screen row holding s, -1 for none.
func LineOf(screen, s string) int {
	for i, l := range strings.Split(screen, "\n") {
		if strings.Contains(l, s) {
			return i
		}
	}
	return -1
}
