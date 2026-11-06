package vegalite

import (
	"testing"

	pvl "github.com/yongjohnlee80/golib/parse/vegalite"
)

// Every mark type a spec may name is drawn: a mark layer's kind by its drawing here, a rule as a
// layer of its own (paint). A mark parse/vegalite reads with no drawing here fails, not a chart.
func TestEveryMarkTypeIsDrawn(t *testing.T) {
	for name, mt := range pvl.MarkTypes {
		if def, ok := marks[mt.Kind]; mt.Kind != pvl.MarkRule && (!ok || def.draw == nil) {
			t.Errorf("mark %q: kind %v has no drawing in marks", name, mt.Kind)
		}
	}
}
