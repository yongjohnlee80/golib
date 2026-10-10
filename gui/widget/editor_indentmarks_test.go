package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// The native editor draws the cursor line's indentation in Raw mode: "·" centred in each leading
// space's cell, "→" at the left edge of a leading tab. The marks are the text calls a paint adds
// over the same paint with the marks off. The Rendered view draws none.
func TestIndentMarksDrawOnTheCursorLineInRawOnly(t *testing.T) {
	h := startEditor(t, 60, 8, WithRenderer(NewMarkdownRenderer()),
		WithCore(tuiwidget.CoreInitialText("  a b\n\tc")))
	texts := func(rc *gui.RecordingCanvas) []gui.PaintCall {
		var out []gui.PaintCall
		for _, c := range rc.Calls {
			if c.Op == "DrawText" {
				out = append(out, c)
			}
		}
		return out
	}
	// marks are the text calls painted with marks on that the same paint with marks off lacks
	marks := func(m tuiwidget.IndentMarks) []gui.PaintCall {
		h.onLoop(func() { h.e.core.SetIndentMarks(tuiwidget.IndentMarksOff) })
		base := texts(h.paint())
		h.onLoop(func() { h.e.core.SetIndentMarks(m) })
		with := texts(h.paint())
		seen := map[gui.Rect]int{}
		for _, c := range base {
			seen[c.Rect]++
		}
		var out []gui.PaintCall
		for _, c := range with {
			if seen[c.Rect] > 0 {
				seen[c.Rect]--
				continue
			}
			out = append(out, c)
		}
		return out
	}
	var x0, cell float32
	h.onLoop(func() { cell = h.e.layout.monoCell(); x0 = h.e.layout.textX() })

	got := marks(tuiwidget.IndentMarksCursorLine)
	if len(got) != 2 {
		t.Fatalf("cursor on line 0 (two leading spaces): %d marks, want 2: %+v", len(got), got)
	}
	for k, c := range got {
		if lo, hi := x0+float32(k)*cell, x0+float32(k+1)*cell; c.Rect.X < lo-0.01 || c.Rect.X+c.Rect.W > hi+0.01 {
			t.Errorf("mark %d at x %v..%v, want inside column %d (%v..%v)", k, c.Rect.X, c.Rect.X+c.Rect.W, k, lo, hi)
		}
	}

	h.onLoop(func() { h.e.core.SetLine(1, 1) })
	got = marks(tuiwidget.IndentMarksCursorLine)
	if len(got) != 1 || got[0].Rect.X != x0 {
		t.Fatalf("cursor on line 1 (a leading tab): marks %+v, want one at the tab's left edge x %v", got, x0)
	}

	if got = marks(tuiwidget.IndentMarksAll); len(got) != 3 {
		t.Errorf("all lines: %d marks, want 3 (two spaces and a tab)", len(got))
	}

	h.onLoop(func() { h.e.SetMode(Rendered) })
	if got = marks(tuiwidget.IndentMarksAll); len(got) != 0 {
		t.Errorf("the Rendered view drew %d marks, want none", len(got))
	}
}
