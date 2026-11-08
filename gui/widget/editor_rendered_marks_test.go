package widget

import (
	"image/color"
	"slices"
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
	red, blue := color.NRGBA{R: 200, A: 0x66}, color.NRGBA{B: 200, A: 0x66} // the tint's opacity, markAlpha
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

// A find's tints sit between what lies under a line's text and the selection: a match in a table's
// header is tinted over the header's opaque background, and one in an inline code span over the
// span's chip, not under them; the current match's selection is laid over its tint, as the terminal
// lays the selection over its marks; the glyphs are drawn over all of it. On a sepia page and on a
// dark one.
func TestTheWindowsMarksSitBetweenTheBlockAndTheSelection(t *testing.T) {
	h := startEditor(t, 60, 10, WithRenderer(NewMarkdownRenderer()),
		WithCore(tuiwidget.CoreInitialText("| alpha | b |\n| --- | --- |\n| c | d |\n\nalpha beta `alpha`")))
	red := color.NRGBA{R: 200, A: 0x66}
	var st tuiwidget.SyntaxStyles
	st[highlight.Alert] = style.New().Foreground(style.RGB(200, 0, 0))
	h.onLoop(func() {
		h.e.core.SetSyntaxStyles(st)
		h.e.SetView(tuiwidget.ViewRendered)
		h.e.core.SetFind("alpha", tuiwidget.FindOptions{})
		h.e.core.SetCurrentMatch(tuiwidget.TextRange{From: tuiwidget.TextPos{Line: 4}, To: tuiwidget.TextPos{Line: 4, Col: 5}})
	})
	covers := func(a, b gui.Rect) bool {
		const e = 0.5
		return a.X <= b.X+e && a.Y <= b.Y+e && a.X+a.W >= b.X+b.W-e && a.Y+a.H >= b.Y+b.H-e
	}
	for _, page := range []struct {
		name    string
		ink, bg color.NRGBA
	}{
		{"sepia", color.NRGBA{R: 0x30, G: 0x28, B: 0x20, A: 0xff}, color.NRGBA{R: 0xe8, G: 0xdc, B: 0xc0, A: 0xff}},
		{"dark", color.NRGBA{R: 0xd0, G: 0xd0, B: 0xd0, A: 0xff}, color.NRGBA{R: 0x1c, G: 0x1c, B: 0x1c, A: 0xff}},
	} {
		var calls []gui.PaintCall
		var sel color.NRGBA
		h.onLoop(func() {
			rc := gui.NewRecordingCanvas(gui.Size{W: float32(h.e.body.w) * cellW, H: float32(h.e.body.h) * cellH}, h.cell)
			rc.Colors = func(int, int) (color.NRGBA, color.NRGBA) { return page.ink, page.bg }
			v, _ := h.e.body.NativeView()
			v.(gui.View).Paint(rc)
			calls, sel = rc.Calls, selColor(h.e.layout.th)
		})
		var tints []int
		for i, c := range calls {
			if c.Op == "FillRect" && c.Brush.Color == red {
				tints = append(tints, i)
			}
		}
		if len(tints) != 3 {
			t.Fatalf("%s: %d red tints, want three (the header's alpha, the prose's, the code span's)", page.name, len(tints))
		}
		selected := false
		for _, i := range tints {
			tint := calls[i].Rect
			if !slices.ContainsFunc(calls[i+1:], func(c gui.PaintCall) bool {
				return c.Op == "DrawText" && c.Rect.X < tint.X+tint.W && tint.X < c.Rect.X+c.Rect.W && c.Rect.Y < tint.Y+tint.H && tint.Y < c.Rect.Y+c.Rect.H
			}) {
				t.Errorf("%s: no text drawn after the tint at %+v: the glyphs must lie over it", page.name, calls[i].Rect)
			}
			for _, c := range calls[i+1:] {
				if c.Op != "FillRect" || !covers(c.Rect, calls[i].Rect) {
					continue
				}
				if c.Brush.Color == sel {
					selected = true // the current match's selection, over its tint
					continue
				}
				if c.Brush.Color.A == 0xff {
					t.Errorf("%s: an opaque fill %v at %+v is laid over the tint at %+v", page.name, c.Brush.Color, c.Rect, calls[i].Rect)
				}
			}
		}
		if !selected {
			t.Errorf("%s: no selection laid over the current match's tint", page.name)
		}
	}
}
