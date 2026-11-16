package decl_test

import (
	"context"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/diff/merge"
	guidecl "github.com/yongjohnlee80/golib/gui/decl"
	"github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

type fixedMerge struct{ in merge.Input }

func (m fixedMerge) Load(context.Context) (merge.Input, error) { return m.in, nil }
func (m fixedMerge) Apply(context.Context, string) error       { return nil }
func (m fixedMerge) Changed() <-chan struct{}                  { return nil }

// The native style gives a declared MergeView native Editors as its panes, and still dresses the
// lines' marks in the palette's diff roles: a conflict's line wears palette.diffConflict.
func TestTheNativeStylesMergeViewHasNativePanesAndTheDiffRoles(t *testing.T) {
	src := fixedMerge{merge.Input{Left: "same\nleft side\n", Right: "same\nright side\n", ResultSide: merge.Right}}
	give := func(p *tuidecl.Program) error {
		c, _ := p.Find("mv")
		if v, ok := tuidecl.MergeViewOf(c); ok {
			v.SetSource(src)
		}
		return nil
	}
	s := decltest.RunWith(t, 120, 12, give,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow {\n MergeView { id: mv; palette.diffConflict: \"red\" } }")),
		tuidecl.WithStyle(guidecl.Native()))
	s.WaitForText(t, "left side")
	var kinds [3]string
	onLoop(t, s, func() {
		c, _ := s.Program.Find("mv")
		v, _ := tuidecl.MergeViewOf(c)
		l, res, r := v.Panes()
		for i, p := range []any{l, res, r} {
			if _, ok := p.(*widget.Editor); ok {
				kinds[i] = "native"
			}
		}
	})
	if kinds != [3]string{"native", "native", "native"} {
		t.Fatalf("panes %v, want gui's Editor for each", kinds)
	}
	var x, y int
	for row, line := range strings.Split(s.String(), "\n") {
		if i := strings.Index(line, "left side"); i >= 0 {
			x, y = len([]rune(line[:i])), row
			break
		}
	}
	if bg := s.Backend.Snapshot()[y][x].Attrs.BG; bg != (tui.CellColor{Kind: tui.CellColorANSI, Index: 1}) {
		t.Fatalf("the conflict's line: background %+v, want palette.diffConflict's red", bg)
	}
}
