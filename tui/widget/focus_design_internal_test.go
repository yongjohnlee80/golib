package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// A widget that is NEVER a focus stop does not implement tui.Focusable: the
// absence of the capability is what says so (tutorial ch. 9), and what
// tui.App.HoldsFocusable reads. A constant `AcceptsFocus() bool { return false }`
// would claim the capability and deny it, and read as focusable by design.
func TestNeverFocusableWidgetsDoNotClaimFocusable(t *testing.T) {
	for name, c := range map[string]tui.Component{
		"FileList":     (*FileList)(nil),
		"FilePreview":  (*FilePreview)(nil),
		"FileOpenView": (*FileOpenView)(nil),
		"FileSaveView": (*FileSaveView)(nil),
		"MenuBar":      (*MenuBar)(nil),
		"menuPopup":    (*menuPopup)(nil),
		"modalCard":    (*modalCard)(nil),
		"scrimLayer":   (*scrimLayer)(nil),
	} {
		if _, ok := c.(tui.Focusable); ok {
			t.Errorf("%s implements tui.Focusable, but is never a focus stop", name)
		}
	}
}
