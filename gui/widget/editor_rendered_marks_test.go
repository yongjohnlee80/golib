package widget

import (
	"image/color"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui/style"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// The window's Rendered view lays the core's overlay under its text, as Raw's highlighting shows
// it: a find tints every match, not only the current one, and the host's marks
// (SetHighlightOverlay) show too. On a sepia page and on a dark one.
func TestTheWindowsRenderedViewMarksEveryMatch(t *testing.T) {
	h := startEditor(t, 60, 8, WithRenderer(NewMarkdownRenderer()),
		WithCore(tuiwidget.CoreInitialText("# Head\n\nalpha beta alpha")))
	red, blue := color.NRGBA{R: 200, A: markAlpha}, color.NRGBA{B: 200, A: markAlpha}
	var st tuiwidget.SyntaxStyles
	st[highlight.Alert] = style.New().Foreground(style.RGB(200, 0, 0))
	st[highlight.Keyword] = style.New().Foreground(style.RGB(0, 0, 200))
	h.onLoop(func() {
		h.e.core.SetSyntaxStyles(st)
		h.e.SetView(tuiwidget.ViewRendered)
	})
	for _, page := range []struct {
		name    string
		ink, bg color.NRGBA
	}{
		{"sepia", color.NRGBA{R: 0x30, G: 0x28, B: 0x20, A: 0xff}, color.NRGBA{R: 0xe8, G: 0xdc, B: 0xc0, A: 0xff}},
		{"dark", color.NRGBA{R: 0xd0, G: 0xd0, B: 0xd0, A: 0xff}, color.NRGBA{R: 0x1c, G: 0x1c, B: 0x1c, A: 0xff}},
	} {
		// tints is how many rects of each tint the editor paints, by their left edges
		tints := func() (reds, blues []float32) {
			h.onLoop(func() {
				rc := gui.NewRecordingCanvas(gui.Size{W: float32(h.e.body.w) * cellW, H: float32(h.e.body.h) * cellH}, h.cell)
				rc.Colors = func(int, int) (color.NRGBA, color.NRGBA) { return page.ink, page.bg }
				v, _ := h.e.body.NativeView()
				v.(gui.View).Paint(rc)
				for _, call := range rc.Calls {
					if call.Op != "FillRect" {
						continue
					}
					switch call.Brush.Color {
					case red:
						reds = append(reds, call.Rect.X)
					case blue:
						blues = append(blues, call.Rect.X)
					}
				}
			})
			return reds, blues
		}
		h.onLoop(func() { h.e.core.SetFind("", tuiwidget.FindOptions{}); h.e.core.SetHighlightOverlay(nil) })
		if r, b := tints(); len(r)+len(b) != 0 {
			t.Fatalf("%s, no find: %d red and %d blue tints, want none", page.name, len(r), len(b))
		}
		h.onLoop(func() { h.e.core.SetFind("alpha", tuiwidget.FindOptions{}) })
		r, b := tints()
		if len(r) != 2 || len(b) != 0 || !(r[0] < r[1]) {
			t.Fatalf("%s, a find of alpha: red tints at %v, blue %v; want two red (both matches), none blue", page.name, r, b)
		}
		h.onLoop(func() {
			h.e.core.SetHighlightOverlay(func(line string, spans []highlight.Span) []highlight.Span {
				if i := strings.Index(line, "beta"); i >= 0 {
					spans = append(spans, highlight.Span{Start: i, End: i + len("beta"), Style: highlight.Keyword})
				}
				return spans
			})
		})
		r2, b2 := tints()
		if len(r2) != 2 || len(b2) != 1 || !(r2[0] < b2[0] && b2[0] < r2[1]) {
			t.Errorf("%s, the host's mark on beta: red %v, blue %v; want blue between the two reds", page.name, r2, b2)
		}
	}
}
