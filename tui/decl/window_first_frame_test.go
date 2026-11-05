package decl_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// window_first_frame_test.go: a themed program's first frame wears its theme. The root Window's
// palette is read when the document is mounted, before there is an App, and the App starts with it:
// before, it was queued after the App was built, so the first frame painted in the default theme and
// only the next one corrected it.

// The first frame, held on screen by a loop that is busy from its first batch, shows the page's text in
// the root Window's palette.
func TestAThemedProgramsFirstFrameWearsItsTheme(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	s := decltest.RunWith(t, 30, 3, func(p *tuidecl.Program) error {
		p.Post(func() { <-release }) // the loop's first batch holds it, so no second frame is drawn
		return nil
	}, tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\n"+
		"Window { palette.highlight: \"#8a5a2b\"; palette.base: \"#e6d9b9\"; palette.text: \"#5b4636\"\n"+
		" HTMLView { html: \"<p>hello</p>\" } }")))
	s.WaitForText(t, "hello")
	want := tui.CellColor{Kind: tui.CellColorRGB, R: 0x5b, G: 0x46, B: 0x36}
	for _, row := range s.Backend.Snapshot() {
		for _, c := range row {
			if c.Content == "h" {
				if c.Attrs.FG != want {
					t.Fatalf("the first frame's text is %+v, want the palette's text %+v", c.Attrs.FG, want)
				}
				return
			}
		}
	}
	t.Fatal("no text on the first frame")
}
