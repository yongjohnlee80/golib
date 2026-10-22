package widget

import (
	"testing"

	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// SetWrap normalizes a mode it does not know to WrapNone, as WithWrap does at construction: a
// runtime setter clamps rather than panics.
func TestSetWrapNormalizesAnUnknownMode(t *testing.T) {
	e := NewEditor(WithWrap(tuiwidget.WrapSoft))
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SetWrap of an unknown mode panicked: %v", r)
			}
		}()
		e.SetWrap(tuiwidget.WrapMode(9))
	}()
	if e.wrap != tuiwidget.WrapNone {
		t.Errorf("SetWrap(9) left the wrap at %v, want WrapNone", e.wrap)
	}
	e.SetWrap(tuiwidget.WrapSoft)
	if e.wrap != tuiwidget.WrapSoft {
		t.Errorf("SetWrap(WrapSoft) left the wrap at %v", e.wrap)
	}
}
