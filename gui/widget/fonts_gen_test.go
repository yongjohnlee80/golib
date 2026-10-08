package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// A change of the window's families alone, at the same size and colours, lays the editor's text
// out again: its blocks were shaped in the old faces.
func TestTheEditorReshapesOnAFamilyOnlyChange(t *testing.T) {
	h := startEditor(t, 40, 6)
	paint := func(gen uint64) *laidBlock {
		var first *laidBlock
		h.onLoop(func() {
			rc := gui.NewRecordingCanvas(gui.Size{W: float32(h.e.body.w) * cellW, H: float32(h.e.body.h) * cellH}, h.cell)
			rc.SetFonts(gui.Fonts{Prose: "Go", Mono: "Go Mono", Gen: gen})
			v, _ := h.e.body.NativeView()
			v.(gui.View).Paint(rc)
			if len(h.e.layout.laid) > 0 {
				first = h.e.layout.laid[0]
			}
		})
		return first
	}
	a := paint(1)
	if a == nil {
		t.Fatal("nothing laid out")
	}
	if b := paint(1); b != a {
		t.Fatal("the same fonts laid the text out again")
	}
	if c := paint(2); c == a {
		t.Error("a new font generation kept the blocks shaped in the old faces")
	}
}

// HTMLView restyles every block on a family-only change: its look includes the generation.
func TestHTMLViewRestylesOnAFamilyOnlyChange(t *testing.T) {
	_, l, c := pixelView(t, "<p>words</p><pre><code>code</code></pre>", 400, 300)
	look := l.look
	l.Paint(c)
	if l.look != look {
		t.Fatal("the same canvas restyled the page")
	}
	f := c.Fonts()
	f.Prose, f.Gen = "Go", f.Gen+1
	c.SetFonts(f)
	l.Paint(c)
	if l.look == look {
		t.Error("a new font generation kept the old look: blocks stay shaped in the old faces")
	}
}
