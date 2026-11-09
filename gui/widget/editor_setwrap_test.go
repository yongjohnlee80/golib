package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/core"
)

// SetWrap normalizes a mode it does not know to WrapNone, as WithWrap does at construction: a
// runtime setter clamps rather than panics.
func TestSetWrapNormalizesAnUnknownMode(t *testing.T) {
	e := NewEditor(WithWrap(core.WrapSoft))
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SetWrap of an unknown mode panicked: %v", r)
			}
		}()
		e.SetWrap(core.WrapMode(9))
	}()
	if e.wrap != core.WrapNone {
		t.Errorf("SetWrap(9) left the wrap at %v, want WrapNone", e.wrap)
	}
	e.SetWrap(core.WrapSoft)
	if e.wrap != core.WrapSoft {
		t.Errorf("SetWrap(WrapSoft) left the wrap at %v", e.wrap)
	}
}
