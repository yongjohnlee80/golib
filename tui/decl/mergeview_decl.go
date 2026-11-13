package decl

import (
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/mergeview"
)

// MergeViewOf is a declared MergeView, for its host to give it the source it merges (SetSource)
// and to act on it. Look it up again after a reload, as with EditorOf: a rebuilt view is another.
func MergeViewOf(c tui.Component) (*mergeview.MergeView, bool) {
	v, ok := c.(*mergeview.MergeView)
	return v, ok
}

func buildMergeView(b Build) (tui.Component, []string, error) {
	consumed, err := b.ReadProps(map[string]field{})
	if err != nil {
		return nil, nil, err
	}
	v := mergeview.NewMergeView(nil, mergeview.Options{})
	closed := b.EmitterWith("closed")
	v.OnClose(func(applied bool) { closed(boolValue(applied)) })
	return v, consumed, nil
}

// restyleMergeView gives the panes the Editor's looks (base and text, the selection in highlight)
// and the lines' marks the palette's diff roles: a role nothing sets keeps its tint.
func restyleMergeView(c tui.Component, p palette) {
	v := c.(*mergeview.MergeView)
	st, _ := p.editorStyles()
	v.SetPaneStyles(st)
	var ms mergeview.Style
	for _, r := range []struct {
		role Role
		to   *style.Style
	}{{roleDiffAdded, &ms.Added}, {roleDiffDeleted, &ms.Deleted}, {roleDiffChanged, &ms.Changed}, {roleDiffConflict, &ms.Conflict}} {
		if col, ok := p[r.role]; ok {
			*r.to = style.New().Background(col)
		}
	}
	v.SetStyle(ms)
}

var mergeViewType = Type{
	Name: "MergeView", Children: NoChildren().Because("its panes and bar are its own"),
	Build:   buildMergeView,
	restyle: restyleMergeView,
	Getters: map[string]Getter{
		"changes": getter(func(v *mergeview.MergeView) (qml.SpecValue, error) {
			n, _ := v.Counts()
			return numberValue(n), nil
		}),
		"conflicts": getter(func(v *mergeview.MergeView) (qml.SpecValue, error) {
			_, n := v.Counts()
			return numberValue(n), nil
		}),
		"result": getter(func(v *mergeview.MergeView) (qml.SpecValue, error) {
			return strValue(v.Result()), nil
		}),
	},
	Methods: map[string]Method{
		"acceptLeft":  method("a MergeView", func(v *mergeview.MergeView) error { v.AcceptLeft(); return nil }),
		"acceptRight": method("a MergeView", func(v *mergeview.MergeView) error { v.AcceptRight(); return nil }),
		"merge":       method("a MergeView", func(v *mergeview.MergeView) error { v.Merge(); return nil }),
		"apply":       method("a MergeView", func(v *mergeview.MergeView) error { v.Apply(); return nil }),
		"cancel":      method("a MergeView", func(v *mergeview.MergeView) error { v.Cancel(); return nil }),
		"reload":      method("a MergeView", func(v *mergeview.MergeView) error { v.Reload(); return nil }),
	},
	Signals: map[string][]string{"closed": {"applied"}},
}
