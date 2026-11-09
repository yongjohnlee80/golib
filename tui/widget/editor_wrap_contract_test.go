package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
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
	var e *editor.Editor
	noPanic("WithEditorWrap(9)", func() { e = editor.NewEditor(editor.WithEditorWrap(core.WrapMode(9))) })
	if got := e.Wrap(); got != core.WrapNone {
		t.Fatalf("WithEditorWrap(9): the wrap is %v, want WrapNone", got)
	}
	e.SetWrap(core.WrapSoft)
	noPanic("SetWrap(9)", func() { e.SetWrap(core.WrapMode(9)) })
	if got := e.Wrap(); got != core.WrapNone {
		t.Fatalf("SetWrap(9) after WrapSoft: the wrap is %v, want WrapNone", got)
	}
	e.SetWrap(core.WrapSoft)
	if got := e.Wrap(); got != core.WrapSoft {
		t.Fatalf("SetWrap(WrapSoft): the wrap is %v", got)
	}
	// Truncate is a TextArea's mode, not an editor's: WrapNone too
	noPanic("SetWrap(Truncate)", func() { e.SetWrap(core.Truncate) })
	if got := e.Wrap(); got != core.WrapNone {
		t.Fatalf("SetWrap(Truncate) after WrapSoft: the wrap is %v, want WrapNone", got)
	}
}
