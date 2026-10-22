package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// The editors' wrap contract, the same as gui's Editor: a mode neither WrapNone nor WrapSoft is
// WrapNone, at construction (WithEditorWrap) and while the editor runs (SetWrap), never a panic.
func TestAnUnknownWrapModeIsWrapNone(t *testing.T) {
	noPanic := func(what string, fn func()) {
		t.Helper()
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("%s panicked: %v", what, r)
			}
		}()
		fn()
	}
	var e *widget.Editor
	noPanic("WithEditorWrap(9)", func() { e = widget.NewEditor(widget.WithEditorWrap(widget.WrapMode(9))) })
	if got := e.Wrap(); got != widget.WrapNone {
		t.Fatalf("WithEditorWrap(9): the wrap is %v, want WrapNone", got)
	}
	e.SetWrap(widget.WrapSoft)
	noPanic("SetWrap(9)", func() { e.SetWrap(widget.WrapMode(9)) })
	if got := e.Wrap(); got != widget.WrapNone {
		t.Fatalf("SetWrap(9) after WrapSoft: the wrap is %v, want WrapNone", got)
	}
	e.SetWrap(widget.WrapSoft)
	if got := e.Wrap(); got != widget.WrapSoft {
		t.Fatalf("SetWrap(WrapSoft): the wrap is %v", got)
	}
	// Truncate is a TextArea's mode, not an editor's: WrapNone too
	noPanic("SetWrap(Truncate)", func() { e.SetWrap(widget.Truncate) })
	if got := e.Wrap(); got != widget.WrapNone {
		t.Fatalf("SetWrap(Truncate) after WrapSoft: the wrap is %v, want WrapNone", got)
	}
}
