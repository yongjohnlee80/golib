package decl_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/diff/merge"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
	"github.com/yongjohnlee80/golib/tui/widget/mergeview"
)

// mergeSource is a MergeSource of fixed texts that keeps what Apply was given.
type mergeSource struct {
	in      merge.Input
	applied chan string
}

func (m *mergeSource) Load(context.Context) (merge.Input, error) { return m.in, nil }
func (m *mergeSource) Apply(_ context.Context, r string) error   { m.applied <- r; return nil }
func (m *mergeSource) Changed() <-chan struct{}                  { return nil }

const mergeViewDoc = "import tui 1.0\nimport demo 1.0\nWindow {\n" +
	" MergeView { id: mv; palette.diffConflict: \"red\"; onClosed: App.log(applied) } }"

// A declared MergeView is its host's to give a source (MergeViewOf, SetSource): it shows the
// merge, wears the palette's diff roles on a conflict's lines, and says it closed, applied, in
// closed(applied).
func TestADeclaredMergeViewTakesItsSourceFromItsHost(t *testing.T) {
	src := &mergeSource{applied: make(chan string, 1), in: merge.Input{
		Left: "same\nleft side\n", Right: "same\nright side\n",
		LeftFrom:   merge.Origin{Kind: merge.File, Label: "On disk"},
		RightFrom:  merge.Origin{Kind: merge.Buffer, Label: "Your edits"},
		ResultTo:   merge.Origin{Kind: merge.Buffer, Label: "Your edits"},
		ResultSide: merge.Right,
	}}
	rec := &recorder{}
	give := func(p *tuidecl.Program) error {
		c, _ := p.Find("mv")
		v, ok := tuidecl.MergeViewOf(c)
		if !ok {
			t.Errorf("MergeViewOf(%T) is not a MergeView", c)
			return nil
		}
		v.SetSource(src)
		return nil
	}
	s := decltest.RunWith(t, 120, 12, give,
		tuidecl.LayoutSource("main.qml", []byte(mergeViewDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "left side")
	s.WaitForText(t, "File On disk")
	// The conflict's line in the left pane wears palette.diffConflict.
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
	s.Program.App().Update(func() {
		c, _ := s.Program.Find("mv")
		v, _ := tuidecl.MergeViewOf(c)
		v.AcceptLeft()
		v.Apply()
	})
	if got := <-src.applied; got != "same\nleft side\n" {
		t.Fatalf("Apply was given %q", got)
	}
	s.WaitFor(t, "closed(true)", func(string) bool {
		all := rec.all()
		return len(all) == 1 && all[0].Raw == "true"
	})
}

// A style can make a MergeView's panes its own way: BuildMergeViewWith gives its Build the view's
// options, and RestyleMergeViewMarks dresses the lines' marks in the palette's diff roles.
func TestAStyleMakesAMergeViewsPanesItsOwnWay(t *testing.T) {
	src := &mergeSource{applied: make(chan string, 1), in: merge.Input{
		Left: "same\nleft side\n", Right: "same\nright side\n", ResultSide: merge.Right}}
	var mu sync.Mutex
	var made []bool // each pane's Result, as asked
	mv, _ := tuidecl.StandardType("MergeView")
	mv.Build = tuidecl.BuildMergeViewWith(mergeview.Options{NewPane: func(spec mergeview.PaneSpec) mergeview.Pane {
		mu.Lock()
		made = append(made, spec.Result)
		mu.Unlock()
		return editor.NewEditor(editor.WithCore(spec.Core...), editor.WithEditorWrap(core.WrapNone),
			editor.WithEditorLineStyle(spec.LineStyle), editor.WithEditorOnScroll(spec.OnScroll))
	}})
	mv.Restyle = func(c tui.Component, p tuidecl.Palette) {
		if v, ok := tuidecl.MergeViewOf(c); ok {
			tuidecl.RestyleMergeViewMarks(v, p)
		}
	}
	give := func(p *tuidecl.Program) error {
		c, _ := p.Find("mv")
		if v, ok := tuidecl.MergeViewOf(c); ok {
			v.SetSource(src)
		}
		return nil
	}
	s := decltest.RunWith(t, 120, 12, give,
		tuidecl.LayoutSource("main.qml", []byte(mergeViewDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": (&recorder{}).handler}),
		tuidecl.WithStyle(tuidecl.Style{Name: "own panes", Types: []tuidecl.Type{mv}}))
	s.WaitForText(t, "left side")
	mu.Lock()
	got := append([]bool(nil), made...)
	mu.Unlock()
	if len(got) != 3 || got[0] || !got[1] || got[2] {
		t.Fatalf("the style's factory made panes %v, want the left, the result, the right", got)
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
