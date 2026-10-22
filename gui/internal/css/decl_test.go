package css

import (
	"slices"
	"testing"
)

// Decls splits a block at the semicolons outside parentheses, lower-cases the property, takes
// !important off the value, and skips what is no declaration.
func TestDeclsReadsADeclarationBlock(t *testing.T) {
	got := Decls(`Fill: url(data:image/png;base64,AA) ; stroke:red !important;;bad; :x; y:`)
	want := []Decl{{Prop: "fill", Value: "url(data:image/png;base64,AA)"}, {Prop: "stroke", Value: "red", Important: true}}
	if !slices.Equal(got, want) {
		t.Errorf("Decls = %+v, want %+v", got, want)
	}
	if got := SplitTop("a(b,c),d", ','); !slices.Equal(got, []string{"a(b,c)", "d"}) {
		t.Errorf("SplitTop = %q", got)
	}
}
