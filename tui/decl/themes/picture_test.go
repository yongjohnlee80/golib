package themes_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/decl/themes"
)

// A page's picture reads in sepia and in dark: on a terminal that draws kitty's graphics, its image
// is placed over its rows, and those rows, reserved, wear the theme's own page (document.base), so
// no stripe of another colour shows round the picture or through it where the image is clear.
func TestAPagesPictureWearsThePageInSepiaAndDark(t *testing.T) {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 80, 32))); err != nil {
		t.Fatal(err)
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	for _, name := range []string{"sepia", "dark"} {
		t.Run(name, func(t *testing.T) {
			vals, err := themes.Values(name)
			if err != nil {
				t.Fatal(err)
			}
			r, g, bl, ok := mustColor(t, vals["document.base"]).RGBValues()
			if !ok {
				t.Fatalf("%s's page is the terminal's own colour: nothing to compare", name)
			}
			page := tui.CellColor{Kind: tui.CellColorRGB, R: r, G: g, B: bl}
			tb := tui.NewTestBackend(30, 8, tui.WithTestCapabilities(tui.Capabilities{ColorProfile: tui.ProfileTrueColor, KittyGraphics: tui.TriYes}))
			s := decltest.RunOn(t, tb, nil, tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport tui.theme."+name+" 1.0\n"+
				"Window { palette.base: Theme.document.base; palette.text: Theme.document.text\n"+
				" palette.window: Theme.document.base; palette.windowText: Theme.document.text\n"+
				" HTMLView { id: page; focus: true; html: \"<p>above</p><p><img alt=\\\"gull\\\" src=\\\""+uri+"\\\"></p><p>below</p>\" } }")), themes.Offer())
			s.WaitForText(t, "below")
			var pl tui.ImagePlacement
			for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
				imgs := tb.Images()
				if len(imgs) == 1 {
					for _, pl = range imgs {
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("%s: no picture placed:\n%s", name, s.String())
				}
			}
			s.WaitFor(t, name+"'s page under the picture", func(string) bool {
				rows := tb.Snapshot()
				for y := pl.Y; y < pl.Y+pl.Rows; y++ {
					for x := pl.X; x < pl.X+pl.Cols; x++ {
						if c := rows[y][x]; c.Content != " " || c.Attrs.BG != page {
							return false
						}
					}
				}
				return true
			})
		})
	}
}
