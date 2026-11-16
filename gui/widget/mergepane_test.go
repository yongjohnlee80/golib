package widget

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/diff/merge"
	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/mergeview"
)

// mergeSrc is a MergeSource of fixed texts.
type mergeSrc struct{ in merge.Input }

func (s mergeSrc) Load(context.Context) (merge.Input, error) { return s.in, nil }
func (s mergeSrc) Apply(context.Context, string) error       { return nil }
func (s mergeSrc) Changed() <-chan struct{}                  { return nil }

// scrollMerge is 80 lines, the left with three lines added after the tenth, the right with its
// fiftieth changed.
func scrollMerge() merge.Input {
	var base, left, right []string
	for i := range 80 {
		base = append(base, fmt.Sprintf("line %02d", i))
	}
	left = append(append(append([]string{}, base[:10]...), "added 1", "added 2", "added 3"), base[10:]...)
	right = append([]string{}, base...)
	right[50] = "changed"
	text := func(ls []string) string { return strings.Join(ls, "\n") + "\n" }
	return merge.Input{HasBase: true, Base: text(base), Left: text(left), Right: text(right), ResultSide: merge.Right}
}

// mergeRig is a merge view with window panes (MergePane) in an App whose backend paints native
// views, as a window's does: paint draws each pane as the window would.
type mergeRig struct {
	t   *testing.T
	app *tui.App
	tb  *tui.TestBackend
	v   *mergeview.MergeView
}

func startMerge(t *testing.T) *mergeRig {
	t.Helper()
	r := &mergeRig{t: t, v: mergeview.NewMergeView(mergeSrc{scrollMerge()}, mergeview.Options{NewPane: MergePane})}
	r.tb = tui.NewTestBackend(120, 16, tui.WithTestCapabilities(tui.Capabilities{NativeViews: true}))
	r.app = tui.NewApp(r.v, tui.WithBackend(&nativeTB{TestBackend: r.tb}), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go r.app.Run(ctx)
	for i := 0; ; i++ {
		var loaded bool
		r.onLoop(func() { loaded = r.v.Model() != nil })
		if loaded {
			break
		}
		if i == 300 {
			t.Fatal("the merge never loaded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.paint()
	return r
}

func (r *mergeRig) onLoop(fn func()) {
	r.t.Helper()
	done := make(chan struct{})
	r.app.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		r.t.Fatal("the loop did not run the update")
	}
}

func (r *mergeRig) panes() [3]*Editor {
	l, res, rt := r.v.Panes()
	return [3]*Editor{l.(*Editor), res.(*Editor), rt.(*Editor)}
}

// paint draws each pane natively, as the window does each frame.
func (r *mergeRig) paint() {
	r.onLoop(func() {
		for _, e := range r.panes() {
			rc := gui.NewRecordingCanvas(gui.Size{W: float32(e.body.w) * cellW, H: float32(e.body.h) * cellH}, gui.Size{W: cellW, H: cellH})
			v, _ := e.body.NativeView()
			v.(gui.View).Paint(rc)
		}
	})
}

// settled is the panes' tops once the tops they tell, after each draw, have been taken.
func (r *mergeRig) settled() (tops [3]int) {
	for range 6 {
		r.paint()
	}
	r.onLoop(func() {
		for i, e := range r.panes() {
			tops[i] = e.TopLine()
		}
	})
	return tops
}

// A window's merge view has native panes in the cells' pitch: no title bar takes a row of the
// view's, so each pane has all the rows between the headers and the bar.
func TestAWindowsMergePanesAreNativeEditorsInCellPitch(t *testing.T) {
	r := startMerge(t)
	var rows [3]int
	var pitch, unwrapped [3]bool
	r.onLoop(func() {
		for i, e := range r.panes() {
			rows[i], pitch[i], unwrapped[i] = e.body.h, e.cellPitch, e.wrap == core.WrapNone
		}
	})
	if pitch != [3]bool{true, true, true} || rows != [3]int{14, 14, 14} {
		t.Fatalf("panes in cell pitch %v, with %v rows of their text, want all, each with the 14 rows between the headers and the bar", pitch, rows)
	}
	if unwrapped != [3]bool{true, true, true} {
		t.Fatalf("panes unwrapped %v, want all: a wrapped line breaks the rows the connectors mark", unwrapped)
	}
}

// A window's merge panes are over the view's cores: the sides read-only, and the text typed into
// the result pane the merge's result, so Apply writes it.
func TestTextTypedInAWindowsResultPaneIsTheMergesResult(t *testing.T) {
	r := startMerge(t)
	var ro [3]bool
	var before string
	r.onLoop(func() {
		for i, e := range r.panes() {
			ro[i] = e.Core().ReadOnly()
		}
		before = r.v.Result()
		res := r.panes()[1]
		r.app.FocusInto(res)
		res.Core().SetLine(0, 0)
	})
	if ro != [3]bool{true, false, true} {
		t.Fatalf("read-only %v, want the sides only", ro)
	}
	// The pane starts in Normal mode, as an editor does: i inserts, and X is then typed.
	if err := r.tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 'i', Text: "i"}, tui.KeyEvent{Kind: tui.KeyPress, Code: 'X', Text: "X"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		var got string
		r.onLoop(func() { got = r.v.Result() })
		if got == "X"+before {
			break
		}
		if i == 300 {
			first, _, _ := strings.Cut(got, "\n")
			t.Fatalf("i, then X typed at the result pane's start: the merge's result begins %q, want %q", first, "Xline 00")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A wheel step in a window's pane moves each pane once: the others follow it after it is drawn,
// and their following never moves it back. The left pane is wheeled from 9 into its added lines,
// where the others have none: they go to the hunk's place, and the left stays at 12.
func TestAWheelStepInAWindowsPaneMovesEachPaneOnce(t *testing.T) {
	r := startMerge(t)
	r.onLoop(func() { r.panes()[0].SetTopLine(9) })
	if tops := r.settled(); tops != [3]int{9, 9, 9} {
		t.Fatalf("the left pane at 9: tops %v, want every pane at 9", tops)
	}
	if err := r.tb.Inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 5, Y: 5}); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		var top int
		r.onLoop(func() { top = r.panes()[0].TopLine() })
		if top != 9 {
			break
		}
		if i == 300 {
			t.Fatal("the wheel step never reached the left pane")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if tops := r.settled(); tops != [3]int{12, 10, 10} {
		t.Fatalf("a wheel step in the left pane from 9: tops %v, want the left at 12, in its added lines, the others at 10", tops)
	}
}
