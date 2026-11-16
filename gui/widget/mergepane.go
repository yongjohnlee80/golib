package widget

import (
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/mergeview"
)

// MergePane is a merge view's pane in a window (mergeview.Options.NewPane): the native Editor over
// the view's core, so the result typed into it is the merge's and the sides stay read-only, in the
// cells' pitch (WithEditorCellPitch), so the connectors the view draws in cells beside it mark its
// lines. Its lines never wrap: a wrapped line would break the panes' rows apart.
func MergePane(spec mergeview.PaneSpec) mergeview.Pane {
	return NewEditor(WithCore(spec.Core...), WithEditorCellPitch(), WithWrap(core.WrapNone),
		WithEditorLineStyle(spec.LineStyle), WithEditorOnScroll(spec.OnScroll))
}

var _ mergeview.Pane = (*Editor)(nil)
