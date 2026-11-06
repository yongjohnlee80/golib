package vegalite

import "testing"

// Every mark type a spec may name has its kind's definition, how it is read and scaled: a mark
// added to MarkTypes without one fails here, not on a chart.
func TestEveryMarkTypeHasItsDefinition(t *testing.T) {
	for name, mt := range MarkTypes {
		if _, ok := marks[mt.Kind]; !ok {
			t.Errorf("mark %q: kind %v has no definition in marks", name, mt.Kind)
		}
	}
}
