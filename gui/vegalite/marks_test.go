package vegalite

import "testing"

// Every mark type a spec may name has its kind's definition, and a kind read as a mark layer can
// draw: a mark added to markTypes without one fails here, not on a chart.
func TestEveryMarkTypeHasItsDefinition(t *testing.T) {
	for name, mt := range markTypes {
		def, ok := marks[mt.kind]
		switch {
		case !ok:
			t.Errorf("mark %q: kind %v has no definition in marks", name, mt.kind)
		case def.layer == nil && def.draw == nil:
			t.Errorf("mark %q: a mark layer with nothing to draw it", name)
		}
	}
}
