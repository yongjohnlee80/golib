package widget

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// pngURI is a data: URI of a w × h PNG.
func pngURI(t *testing.T, w, h int) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

// pageRun is an HTMLView of html on a w × h test terminal with caps, running.
type pageRun struct {
	t   *testing.T
	tb  *tui.TestBackend
	app *tui.App
	v   *HTMLView
}

func runPage(t *testing.T, html string, w, h int, caps tui.Capabilities) *pageRun {
	t.Helper()
	v := NewHTMLView()
	v.SetHTML([]byte(html))
	tb := tui.NewTestBackend(w, h, tui.WithTestCapabilities(caps))
	app := tui.NewApp(v, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = app.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return &pageRun{t, tb, app, v}
}

// waitFor waits, up to five seconds, for ok of the screen and the images placed.
func (p *pageRun) waitFor(what string, ok func(screen string, imgs map[uint32]tui.ImagePlacement) bool) {
	p.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if p.tb.Flushes() > 0 && ok(p.tb.String(), p.tb.Images()) {
			return
		}
	}
	p.t.Fatalf("%s never came: images %+v\n%s", what, p.tb.Images(), p.tb.String())
}

// onLoop runs fn on the App's loop and waits for it.
func (p *pageRun) onLoop(fn func()) {
	p.t.Helper()
	ran := make(chan struct{})
	p.app.Update(func() { defer close(ran); fn() })
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		p.t.Fatal("the loop did not run")
	}
}

var kitty = tui.Capabilities{ColorProfile: tui.ProfileTrueColor, KittyGraphics: tui.TriYes}

// lineOf is the screen row holding s, -1 for none.
func lineOf(screen, s string) int {
	for i, l := range strings.Split(screen, "\n") {
		if strings.Contains(l, s) {
			return i
		}
	}
	return -1
}

// A paragraph of one image, on a terminal that draws kitty's graphics, is the picture: its rows
// reserved, blank, between the text before and after it, and its image placed over them at its own
// size in cells (80 × 32 pixels, cells 8 × 16: 10 × 2), never its alt text.
func TestAnImageIsDrawnOverItsRowsOnAKittyTerminal(t *testing.T) {
	p := runPage(t, `<p>before</p><p><img alt="gull" src="`+pngURI(t, 80, 32)+`"></p><p>after</p>`, 40, 12, kitty)
	p.waitFor("the picture placed", func(sc string, imgs map[uint32]tui.ImagePlacement) bool { return len(imgs) == 1 })
	sc, imgs := p.tb.String(), p.tb.Images()
	var pl tui.ImagePlacement
	for _, pl = range imgs {
	}
	if pl.Cols != 10 || pl.Rows != 2 || pl.X != 0 || len(pl.PNG) == 0 {
		t.Fatalf("placed %+v; want 10 × 2 at the left", pl)
	}
	before, after := lineOf(sc, "before"), lineOf(sc, "after")
	if strings.Contains(sc, "[gull]") || before < 0 || after < 0 || !(before < pl.Y && pl.Y+pl.Rows <= after) {
		t.Fatalf("the picture at rows %d-%d, the text at %d and %d:\n%s", pl.Y, pl.Y+pl.Rows-1, before, after, sc)
	}
	for _, l := range strings.Split(sc, "\n")[pl.Y : pl.Y+pl.Rows] {
		if strings.TrimSpace(l) != "" {
			t.Fatalf("a reserved row shows %q:\n%s", l, sc)
		}
	}
}

// Where the terminal can't draw images (no kitty graphics), or the image can't be read as a raster
// (an SVG), or it sits in running text, it is its alt text, and nothing is placed.
func TestAnImageIsItsAltTextWhereItCantBeDrawn(t *testing.T) {
	svg := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"/>`))
	for name, c := range map[string]struct {
		html string
		caps tui.Capabilities
	}{
		"no kitty graphics": {`<p><img alt="gull" src="` + pngURI(t, 80, 32) + `"></p><p>after</p>`, tui.Capabilities{ColorProfile: tui.ProfileTrueColor}},
		"an SVG":            {`<p><img alt="gull" src="` + svg + `"></p><p>after</p>`, kitty},
		"in running text":   {`<p>a <img alt="gull" src="` + pngURI(t, 80, 32) + `"> in a sentence</p><p>after</p>`, kitty},
	} {
		t.Run(name, func(t *testing.T) {
			p := runPage(t, c.html, 40, 12, c.caps)
			p.waitFor("the alt text", func(sc string, _ map[uint32]tui.ImagePlacement) bool {
				return strings.Contains(sc, "[gull]") && strings.Contains(sc, "after")
			})
			time.Sleep(50 * time.Millisecond) // a load that would land has landed
			p.onLoop(func() {})
			if imgs := p.tb.Images(); len(imgs) != 0 || !strings.Contains(p.tb.String(), "[gull]") {
				t.Fatalf("placed %+v:\n%s", imgs, p.tb.String())
			}
		})
	}
}

// An image wider than the room is scaled down to the room's width, keeping its shape (400 × 100
// pixels in 20 columns: 20 × 3), never panned.
func TestAnImageWiderThanTheRoomFitsIt(t *testing.T) {
	p := runPage(t, `<p><img alt="gull" src="`+pngURI(t, 400, 100)+`"></p>`, 20, 10, kitty)
	p.waitFor("the picture placed", func(_ string, imgs map[uint32]tui.ImagePlacement) bool {
		for _, pl := range imgs {
			return pl.Cols == 20 && pl.Rows == 3
		}
		return false
	})
}

// A picture scrolled partly above the view's top is cut to its rows that show, the same part of
// its image placed over them.
func TestAPictureScrolledPartlyOffIsCropped(t *testing.T) {
	p := runPage(t, `<p><img alt="gull" src="`+pngURI(t, 80, 64)+`"></p><p>a</p><p>b</p><p>c</p><p>d</p><p>e</p><p>f</p>`, 40, 4, kitty)
	p.waitFor("the picture placed", func(_ string, imgs map[uint32]tui.ImagePlacement) bool {
		for _, pl := range imgs {
			return pl.Rows == 4 && pl.Y == 0
		}
		return false
	})
	p.onLoop(func() {
		p.v.scrollBy(2)
		p.v.MarkDirty()
	})
	p.waitFor("the picture cut", func(_ string, imgs map[uint32]tui.ImagePlacement) bool {
		for _, pl := range imgs {
			return pl.Y == 0 && pl.Rows == 2 && pl.Clip == tui.Rect{X: 0, Y: 32, W: 80, H: 32}
		}
		return false
	})
}

// imageCells sizes a picture: its own size where it fits, the room's width where it doesn't, and at
// most cellImageRows tall, its shape kept.
func TestImageCellsKeepTheShape(t *testing.T) {
	for _, c := range []struct{ w, h, room, cols, rows int }{
		{80, 32, 40, 10, 2},
		{400, 100, 20, 20, 3},
		{80, 1600, 40, 4, 40},
		{1, 1, 40, 1, 1},
	} {
		if cols, rows := imageCells(c.w, c.h, c.room, 8, 16); cols != c.cols || rows != c.rows {
			t.Errorf("%d × %d in %d: %d × %d; want %d × %d", c.w, c.h, c.room, cols, rows, c.cols, c.rows)
		}
	}
}

// loneImage: a paragraph of one image and nothing else but space is an image block; one with text,
// or two images, is not.
func TestALoneImageIsAnImageBlock(t *testing.T) {
	for html, want := range map[string]string{
		`<p><img alt="gull" src="g.png"></p>`:                       "g.png",
		`<p> <img src="g.png"> </p>`:                                "g.png",
		`<p>see <img alt="gull" src="g.png"></p>`:                   "",
		`<p><img alt="a" src="a.png"><img alt="b" src="b.png"></p>`: "",
		`<p><em>x</em><img alt="a" src="a.png"></p>`:                "",
	} {
		got := ""
		for _, b := range readHTMLDoc([]byte(html)) {
			if b.image != nil {
				got = b.image.src
			}
		}
		if got != want {
			t.Errorf("%s: image %q; want %q", html, got, want)
		}
	}
}
