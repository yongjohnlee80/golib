package htmlview

import "testing"

// An HTML view never laid out has no links showing to shape.
func TestAnHTMLViewNeverLaidOutShapesNothing(t *testing.T) {
	v := NewHTMLView()
	v.SetHTML([]byte(`<a href="b.md">a link</a>`))
	if got := v.PointerShapes(nil); len(got) != 0 {
		t.Errorf("a view never laid out: %+v", got)
	}
}

// pixelLinks is a bound layout the backend paints: it tells its links, but is no cell layout.
type pixelLinks struct{}

func (pixelLinks) SetSource([]byte) {}

func (pixelLinks) Height() float32 { return 4 }

func (pixelLinks) At(float32, float32) DocPos { return DocPos{} }

func (pixelLinks) Rects(DocPos, DocPos) []Rect32 { return nil }

func (pixelLinks) Text(DocPos, DocPos) string { return "" }

func (pixelLinks) LinkAt(float32, float32) string { return "b.md" }

func (pixelLinks) BlockAt(float32) (int, float32) { return -1, 0 }

func (pixelLinks) BlockTop(int) (float32, bool) { return 0, false }

func (pixelLinks) LinkRects(_, _ float32, dst []Rect32) []Rect32 {
	return append(dst, Rect32{W: 4, H: 1})
}

// A bound layout's links are its painter's to shape, after it paints (gui.PointerRegionsView):
// the view gives none of its own for them, its layout's geometry being the frame before's.
func TestAViewWithABoundLayoutLeavesItsLinksToItsPainter(t *testing.T) {
	v := NewHTMLView()
	v.BindLayout(pixelLinks{})
	v.w, v.h = 20, 4
	if got := v.PointerShapes(nil); len(got) != 0 {
		t.Errorf("a bound layout's links: %+v", got)
	}
}
