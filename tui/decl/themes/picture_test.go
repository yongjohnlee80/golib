package themes_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
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
// is placed over its rows, and those rows, reserved, wear the page's own background, the one its text
// sits on (once the theme's palette has reached the frame: its text no longer on the terminal's own
// colours), so no stripe of another colour shows round the picture or through it where the image is
// clear.
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
			if _, _, _, ok := mustColor(t, vals["document.text"]).RGBValues(); !ok {
				t.Fatalf("%s's text is the terminal's own colour: nothing to wait for", name)
			}
			// the test terminal's whole profile (a truecolor terminal on a known dark page, as
			// decltest.Run's), with kitty's graphics: a theme's colours resolve against the terminal's own
			caps := tui.Capabilities{ColorProfile: tui.ProfileTrueColor, KittyKeyboard: true, SyncOutput: true, InBandResize: true,
				UnicodeCore: true, BracketedPaste: true, Mouse: tui.TriYes, Undercurl: true, DarkBackground: true,
				DefaultFG: tui.ProbedColor{R: 229, G: 229, B: 229, Known: true}, DefaultBG: tui.ProbedColor{Known: true},
				KittyGraphics: tui.TriYes}
			tb := tui.NewTestBackend(30, 8, tui.WithTestCapabilities(caps))
			s := decltest.RunOn(t, tb, nil, tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport tui.theme."+name+" 1.0\n"+
				"Window { palette.highlight: Theme.document.highlight; palette.base: Theme.document.base; palette.text: Theme.document.text\n"+
				" palette.window: Theme.document.base; palette.windowText: Theme.document.text\n"+
				" palette.mid: Theme.app.mid; palette.accent: Theme.menu.accent; palette.documentAccent: Theme.document.accent\n"+
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
			// the page's background: under the a of "above", once the theme colours its text
			pageBG := func(rows [][]tui.Cell) (tui.CellColor, bool) {
				for _, row := range rows {
					for _, c := range row {
						if c.Content == "a" {
							return c.Attrs.BG, c.Attrs.FG.Kind == tui.CellColorRGB
						}
					}
				}
				return tui.CellColor{}, false
			}
			var seen string
			t.Cleanup(func() {
				if t.Failed() {
					t.Logf("%s: %s (document.base %s, document.text %s)", name, seen, vals["document.base"], vals["document.text"])
				}
			})
			s.WaitFor(t, name+"'s page under the picture", func(string) bool {
				rows := tb.Snapshot()
				page, themed := pageBG(rows)
				if !themed {
					seen = "the page's text is not yet on the theme's colours"
					return false
				}
				for y := pl.Y; y < pl.Y+pl.Rows; y++ {
					for x := pl.X; x < pl.X+pl.Cols; x++ {
						if c := rows[y][x]; c.Content != " " || c.Attrs.BG != page {
							seen = fmt.Sprintf("the cell at %d,%d is %q on %+v; the page's text is on %+v", x, y, c.Content, c.Attrs.BG, page)
							return false
						}
					}
				}
				return true
			})
		})
	}
}
