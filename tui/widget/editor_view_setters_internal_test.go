package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// The view setters store what they are given, and setting the same value again changes nothing:
// the line numbers' colour, the gutter, the wrap, and whether a text set keeps the reader's place.
func TestTheEditorsViewSettersStoreTheirValue(t *testing.T) {
	e := NewEditor()
	dim := style.Color{}
	e.SetLineNumberColor(dim)
	if !e.cells.numberColored || e.cells.numberColor != dim {
		t.Error("SetLineNumberColor did not take the colour")
	}
	e.SetLineNumbers(true)
	e.SetLineNumbers(true)
	if !e.cells.numbers {
		t.Error("SetLineNumbers(true), twice: no gutter")
	}
	e.cells.left = 3
	e.SetWrap(WrapNone) // already WrapNone: the horizontal scroll stays
	if e.cells.left != 3 {
		t.Errorf("SetWrap to the wrap it has reset the scroll to %d", e.cells.left)
	}
	if e.SetKeepPlace(true); !e.KeepsPlace() {
		t.Error("SetKeepPlace(true): KeepsPlace false")
	}
}
