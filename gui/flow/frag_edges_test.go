package flow

import (
	"math"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// What a frag draws ends where its edges, and so the caret, say. Pieces are measured alone, but
// a frag shaped whole can be narrower: a space between Hangul words takes the CJK fallback font's
// space. Drawn as one run, the text ended short of its edges by that much per space, and the
// caret sat right of the text by a gap growing along the line.
func TestAFragDrawsWhereItsEdgesAre(t *testing.T) {
	ts := gui.NewTextShaper(1)
	f := gui.Font{Family: gui.CellFamily, Size: 16}
	text := "안녕 한글이 유명해지곤 있지만 반면 어떻게할수있을까"
	whole := ts.Measure(text, f)
	var pieces float32
	for i, w := range strings.Split(text, " ") {
		if i > 0 {
			sp := ts.Measure(" ", f)
			pieces += sp.X[len(sp.X)-1]
		}
		m := ts.Measure(w, f)
		pieces += m.X[len(m.X)-1]
	}
	if math.Abs(float64(pieces-whole.X[len(whole.X)-1])) < 0.01 {
		t.Skip("no CJK fallback font here shapes a space among Hangul differently: the case cannot occur")
	}
	p := Lay([]Span{{Text: text, Font: f}}, Options{WhiteSpace: PreWrap}, ts)
	caret := p.Caret(Pos{Span: 0, Offset: len(text)})
	var drawnEnd float32
	for _, l := range p.Lines {
		for _, fr := range l.Frags {
			switch {
			case fr.Layout != nil:
				drawnEnd = max(drawnEnd, fr.X+fr.Layout.Width)
			case len(fr.glyphs) > 0:
				for k, g := range fr.glyphs {
					if g != nil {
						drawnEnd = max(drawnEnd, fr.X+fr.xs[k]+g.Width)
					}
				}
			}
		}
	}
	if math.Abs(float64(caret.X-drawnEnd)) > 0.01 {
		t.Errorf("the caret after the text is at x %.2f, the drawn text ends at %.2f: a %.2f px gap", caret.X, drawnEnd, caret.X-drawnEnd)
	}
}

// Text whose pieces shape the same alone and whole stays one run: no cluster-by-cluster
// drawing for plain Latin, so ligatures and kerning are kept.
func TestAFragThatAgreesStaysOneRun(t *testing.T) {
	ts := gui.NewTextShaper(1)
	p := Lay([]Span{{Text: "abcd efgh ijkl", Font: gui.Font{Family: gui.CellFamily, Size: 16}}}, Options{WhiteSpace: PreWrap}, ts)
	for _, l := range p.Lines {
		for _, fr := range l.Frags {
			if fr.Layout == nil || len(fr.glyphs) > 0 {
				t.Errorf("frag %q drawn cluster by cluster, want one run", fr.display)
			}
		}
	}
}
