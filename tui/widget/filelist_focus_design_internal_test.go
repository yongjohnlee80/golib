package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// TestNeverFocusableFileViewsDoNotClaimFocusable: the file views are never a focus stop themselves,
// so none implements tui.Focusable: TestNeverFocusableWidgetsDoNotClaimFocusable's rule, for the file
// views.
func TestNeverFocusableFileViewsDoNotClaimFocusable(t *testing.T) {
	for name, c := range map[string]tui.Component{
		"FileList":     (*FileList)(nil),
		"FilePreview":  (*FilePreview)(nil),
		"FileOpenView": (*FileOpenView)(nil),
		"FileSaveView": (*FileSaveView)(nil),
	} {
		if _, ok := c.(tui.Focusable); ok {
			t.Errorf("%s implements tui.Focusable, but is never a focus stop", name)
		}
	}
}
