package flow

import (
	"math"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// drawnEnd is where what a paragraph draws ends: its whole runs, its piece runs at their edges.
func drawnEnd(p *Para) float32 {
	var end float32
	for _, l := range p.Lines {
		for _, fr := range l.Frags {
			if fr.Layout != nil {
				end = max(end, fr.X+fr.Layout.Width)
			}
			for k, r := range fr.runs {
				if r != nil {
					end = max(end, fr.X+fr.xs[fr.runEdge[k]]+r.Width)
				}
			}
		}
	}
	return end
}

// What a frag draws ends where its edges, and so the caret, say. Pieces are measured alone, but
// a frag shaped whole can be narrower: a space between Hangul words takes the CJK fallback font's
// space. Drawn as one run, the text ended short of its edges by that much per space, and the
// caret sat right of the text by a gap growing along the line. Drawn piece by piece, each word is
// still shaped whole.
func TestAFragDrawsWhereItsEdgesAre(t *testing.T) {
	ts := gui.NewTextShaper(1)
	f := gui.Font{Family: gui.CellFamily, Size: 16}
	text := "안녕 한글이 유명해지곤 있지만 반면 어떻게할수있을까"
	whole := ts.Measure(text, f)
	var pieces float32
	words := strings.Split(text, " ")
	for i, w := range words {
		if i > 0 {
			sp := ts.Measure(" ", f)
			pieces += sp.X[len(sp.X)-1]
		}
		m := ts.Measure(w, f)
		pieces += m.X[len(m.X)-1]
	}
	if math.Abs(float64(pieces-whole.X[len(whole.X)-1])) < 1.0/64 {
		t.Skip("no CJK fallback font here shapes a space among Hangul differently: the case cannot occur")
	}
	p := Lay([]Span{{Text: text, Font: f}}, Options{WhiteSpace: PreWrap}, ts)
	caret := p.Caret(Pos{Span: 0, Offset: len(text)})
	if end := drawnEnd(p); math.Abs(float64(caret.X-end)) > 1.0/64 {
		t.Errorf("the caret after the text is at x %.2f, the drawn text ends at %.2f: a %.2f px gap", caret.X, end, caret.X-end)
	}
	for _, l := range p.Lines {
		for _, fr := range l.Frags {
			if len(fr.glyphs) > 0 {
				t.Errorf("frag %q drawn grapheme by grapheme; its words must stay whole", fr.display)
			}
			drawn := 0
			for _, r := range fr.runs {
				if r != nil {
					drawn++
				}
			}
			if fr.Layout == nil && drawn != len(words) {
				t.Errorf("frag %q drawn as %d runs, want one per word (%d)", fr.display, drawn, len(words))
			}
		}
	}
}

// Right-to-left text keeps its whole run: a word's letters join only shaped together, and a
// phrase's visual order is the run's. Measured right-to-left edges never end at the drawn width,
// so a width check alone would split it.
func TestRightToLeftTextStaysOneRun(t *testing.T) {
	ts := gui.NewTextShaper(1)
	f := gui.Font{Family: gui.CellFamily, Size: 16}
	for _, text := range []string{"سلام", "سلام عليكم يا صديقي", "שלום עולם"} {
		p := Lay([]Span{{Text: text, Font: f}}, Options{WhiteSpace: PreWrap}, ts)
		for _, l := range p.Lines {
			for _, fr := range l.Frags {
				if fr.Layout == nil || len(fr.runs) > 0 || len(fr.glyphs) > 0 {
					t.Errorf("%q: frag %q not drawn as one run", text, fr.display)
				}
			}
		}
	}
}

// Text whose pieces shape the same alone and whole stays one run: plain Latin keeps its
// ligatures and kerning.
func TestAFragThatAgreesStaysOneRun(t *testing.T) {
	ts := gui.NewTextShaper(1)
	p := Lay([]Span{{Text: "abcd efgh ijkl", Font: gui.Font{Family: gui.CellFamily, Size: 16}}}, Options{WhiteSpace: PreWrap}, ts)
	for _, l := range p.Lines {
		for _, fr := range l.Frags {
			if fr.Layout == nil || len(fr.runs) > 0 || len(fr.glyphs) > 0 {
				t.Errorf("frag %q not drawn as one run", fr.display)
			}
		}
	}
}
