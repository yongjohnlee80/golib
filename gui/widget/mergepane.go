package widget

import "github.com/yongjohnlee80/golib/tui/widget/mergeview"

// MergePane is a merge view's pane in a window (mergeview.Options.NewPane): the native Editor in
// the cells' pitch (WithEditorCellPitch), so the connectors the view draws in cells beside it mark
// its lines.
func MergePane(spec mergeview.PaneSpec) mergeview.Pane {
	return NewEditor(WithCore(spec.Core...), WithEditorCellPitch(),
		WithEditorLineStyle(spec.LineStyle), WithEditorOnScroll(spec.OnScroll))
}

var _ mergeview.Pane = (*Editor)(nil)
