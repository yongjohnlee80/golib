package mergeview_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/diff/merge"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
	"github.com/yongjohnlee80/golib/tui/widget/mergeview"
)

// source is a fake MergeSource: Load's answer, the results Apply was given, an Apply error, and a
// Changed signal the test sends.
type source struct {
	mu       sync.Mutex
	in       merge.Input
	loadErr  error
	applyErr error
	hold     chan struct{} // when set, Apply waits for it
	loads    int
	applied  []string
	changed  chan struct{}
}

func (s *source) Load(context.Context) (merge.Input, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	return s.in, s.loadErr
}

func (s *source) Apply(_ context.Context, result string) error {
	s.mu.Lock()
	hold := s.hold
	s.mu.Unlock()
	if hold != nil {
		<-hold
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applied = append(s.applied, result)
	return s.applyErr
}

func (s *source) failLoads(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadErr = err
}

func (s *source) Changed() <-chan struct{} {
	if s.changed == nil {
		return nil
	}
	return s.changed
}

func (s *source) counts() (loads int, applied []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loads, append([]string(nil), s.applied...)
}

var (
	buffer = merge.Origin{Kind: merge.Buffer, Location: "notes.md", Revision: "7", Label: "Your unsaved edits"}
	disk   = merge.Origin{Kind: merge.File, Location: "/work/notes.md", Revision: "2026-11-13T10:00:00Z", Label: "On disk"}
	remote = merge.Origin{Kind: merge.Remote, Location: "gs://bucket/notes.md", Revision: "1699999999123456"}
	commit = merge.Origin{Kind: merge.Git, Location: "notes.md", Revision: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"}
)

func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

// threeWay is a merge of a left change, a right change and a conflict, the result to the buffer
// on the right: Compare's shape, the merge starting from the buffer's own text.
func threeWay() merge.Input {
	return merge.Input{
		HasBase:  true,
		Base:     lines("alpha", "beta", "gamma", "delta", "x", "omega"),
		Left:     lines("alpha", "BETA", "gamma", "delta", "x", "OMEGA-L"),
		Right:    lines("alpha", "beta", "gamma", "DELTA", "x", "omega-r"),
		BaseFrom: disk, LeftFrom: disk, RightFrom: buffer, ResultTo: buffer, ResultSide: merge.Right,
	}
}

type rig struct {
	t      *testing.T
	h      *widgettest.Harness
	v      *mergeview.MergeView
	src    *source
	mu     sync.Mutex
	closed []bool
}

func start(t *testing.T, src *source, opts mergeview.Options, w, h int, appOpts ...tui.AppOption) *rig {
	t.Helper()
	r := &rig{t: t, src: src}
	r.v = mergeview.NewMergeView(src, opts)
	r.v.OnClose(func(applied bool) { r.mu.Lock(); r.closed = append(r.closed, applied); r.mu.Unlock() })
	if len(appOpts) > 0 {
		r.h = startWith(t, r.v, w, h, appOpts...)
	} else {
		r.h = widgettest.StartApp(t, r.v, w, h)
	}
	t.Cleanup(r.h.Stop)
	return r
}

// startWith is widgettest.StartApp with App options (a theme).
func startWith(t *testing.T, root tui.Component, w, h int, opts ...tui.AppOption) *widgettest.Harness {
	tb := tui.NewTestBackend(w, h)
	app := tui.NewApp(root, append([]tui.AppOption{tui.WithBackend(tb), tui.WithMinFrameInterval(0)}, opts...)...)
	ctx, cancel := context.WithCancel(context.Background())
	ih := &widgettest.Harness{T: t, App: app, TB: tb, Done: make(chan error, 1), Cancel: cancel}
	go func() { ih.Done <- app.Run(ctx) }()
	ih.Sync()
	return ih
}

// screen waits for the screen to satisfy ok.
func (r *rig) screen(what string, ok func(string) bool) string {
	r.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		r.h.Sync()
		s := r.h.TB.String()
		if ok(s) {
			return s
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s:\n%s", what, s)
		}
		time.Sleep(time.Millisecond)
	}
}

func (r *rig) shows(what string, texts ...string) string {
	r.t.Helper()
	return r.screen(what, func(s string) bool {
		for _, x := range texts {
			if !strings.Contains(s, x) {
				return false
			}
		}
		return true
	})
}

func (r *rig) loaded() {
	r.t.Helper()
	r.screen("the merge loaded", func(string) bool {
		var ok bool
		r.h.OnLoop(func() { ok = r.v.Model() != nil })
		return ok
	})
}

// until waits for cond, read on the loop: an injected key reaches the loop in its own lane, so a
// check after it waits for its effect, never reads once.
func (r *rig) until(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var ok bool
		r.h.OnLoop(func() { ok = cond() })
		if ok {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s:\n%s", what, r.h.TB.String())
		}
		time.Sleep(time.Millisecond)
	}
}

// resultIs waits for the result to be want.
func (r *rig) resultIs(what, want string) {
	r.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := r.result()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("%s: result %q, want %q\n%s", what, got, want, r.h.TB.String())
		}
		time.Sleep(time.Millisecond)
	}
}

func (r *rig) result() (s string) {
	r.h.OnLoop(func() { s = r.v.Result() })
	return s
}

func (r *rig) counts() (ch, co int) {
	r.h.OnLoop(func() { ch, co = r.v.Counts() })
	return
}

func (r *rig) keys(evs ...tui.Event) {
	r.t.Helper()
	if err := r.h.TB.Inject(evs...); err != nil {
		r.t.Fatal(err)
	}
	r.h.Sync()
	r.h.Sync()
}

func key(c rune) tui.KeyEvent {
	return tui.KeyEvent{Kind: tui.KeyPress, Code: c, Text: string(c)}
}

// click presses and releases the left button at (x, y).
func (r *rig) click(x, y int) {
	r.keys(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
}

// find is where text first shows on the screen.
func find(screen, text string) (x, y int, ok bool) {
	for y, row := range strings.Split(screen, "\n") {
		if i := strings.Index(row, text); i >= 0 {
			return len([]rune(row[:i])), y, true
		}
	}
	return 0, 0, false
}

// focus puts the focus in pane (0 left, 1 result, 2 right), its cursor on line.
func (r *rig) focus(pane, line int) {
	r.h.OnLoop(func() {
		l, res, rt := r.v.Panes()
		ed := [3]*editor.Editor{l, res, rt}[pane]
		r.h.App.FocusInto(ed)
		ed.Core().SetLine(line, 0)
	})
	r.h.Sync()
}

// The view shows each side and the result, every pane headed by its origin: a buffer, a file, a
// gs:// object and a git commit, each kind's word, its title and its short revision.
func TestEachPaneIsHeadedByItsOrigin(t *testing.T) {
	in := threeWay()
	in.LeftFrom, in.RightFrom = commit, remote
	in.ResultTo = buffer
	r := start(t, &source{in: in}, mergeview.Options{}, 150, 14)
	s := r.shows("the panes and their headers", "Git notes.md @a1b2c3d", "Remote gs://bucket/notes.md @1699999999123456",
		"Result → Buffer Your unsaved edits @7", "BETA", "DELTA", "alpha")
	if !strings.Contains(s, "changes unresolved") {
		t.Fatalf("no counts in the bar:\n%s", s)
	}
	in.LeftFrom, in.RightFrom, in.ResultTo = disk, buffer, buffer
	r = start(t, &source{in: in}, mergeview.Options{}, 150, 14)
	r.shows("a file's header", "File On disk @2026-11-13T10:00:00Z", "Buffer Your unsaved edits @7")
	r.keys(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF2})
	r.shows("the details row", "/work/notes.md 2026-11-13T10:00:00Z", "notes.md 7")
}

// Each key resolves the change at the cursor: < the left's lines, > the right's, + both, x as it
// is; ] moves to the next change. The bar's counts follow.
func TestTheKeysResolveTheChangeAtTheCursor(t *testing.T) {
	r := start(t, &source{in: threeWay()}, mergeview.Options{}, 120, 14)
	r.loaded()
	if ch, co := r.counts(); ch != 3 || co != 1 {
		t.Fatalf("counts %d, %d at the start", ch, co)
	}
	r.focus(0, 1) // the left pane, on BETA
	r.keys(key('<'))
	r.until("< taking the left's change", func() bool { return r.v.Model().Changes()[0].State == merge.TookLeft })
	r.resultIs("< on the left's change", lines("alpha", "BETA", "gamma", "DELTA", "x", "omega-r"))
	r.focus(2, 3) // the right pane, on DELTA
	r.keys(key('>'))
	// The buffer's DELTA is in the result already: wait for the take itself, not the text.
	r.until("> taking the right's change", func() bool { return r.v.Model().Changes()[1].State == merge.TookRight })
	r.resultIs("> on the right's change", lines("alpha", "BETA", "gamma", "DELTA", "x", "omega-r"))
	r.focus(2, 5) // the conflict
	r.keys(key('+'))
	r.until("+ taking both of the conflict", func() bool { return r.v.Model().Changes()[2].State == merge.TookBoth })
	r.resultIs("+ on the conflict", lines("alpha", "BETA", "gamma", "DELTA", "x", "OMEGA-L", "omega-r"))
	if ch, co := r.counts(); ch != 0 || co != 0 {
		t.Fatalf("counts %d, %d with every change taken", ch, co)
	}
	r.shows("the bar says every change is resolved", "Every change is resolved")

	r = start(t, &source{in: threeWay()}, mergeview.Options{}, 120, 14)
	r.loaded()
	r.focus(0, 5)
	r.keys(key('x'))
	r.until("x dismissing the conflict", func() bool { ch, co := r.v.Counts(); return ch == 2 && co == 0 })
	if got := r.result(); got != threeWay().Right {
		t.Fatalf("x changed the result: %q, want it as it started, the buffer's", got)
	}
	r.focus(1, 0)
	r.keys(key(']'))
	r.until("] moving the result's cursor to the first change's line, 1", func() bool {
		_, res, _ := r.v.Panes()
		return res.Core().Cursor().Line == 1
	})
}

// A click on an arrow takes that side's lines; a click on a bar button does its action.
func TestClickingAnArrowAndABarButton(t *testing.T) {
	r := start(t, &source{in: threeWay()}, mergeview.Options{}, 120, 14)
	r.loaded()
	s := r.shows("the arrows", "»", "«")
	x, y, _ := find(s, "»")
	r.click(x, y) // the first », the left's BETA
	r.resultIs("a click on »", lines("alpha", "BETA", "gamma", "DELTA", "x", "omega-r"))
	s = r.shows("the bar", "Accept Right")
	x, y, _ = find(s, "Accept Right")
	r.click(x+1, y)
	r.resultIs("a click on Accept Right", threeWay().Right)
	s = r.shows("the bar", "Accept Left")
	x, y, _ = find(s, "Accept Left")
	r.click(x+1, y)
	r.resultIs("a click on Accept Left", threeWay().Left)
}

// Merge takes every change that is not a conflict; Apply then asks (two changes left: none, one
// conflict) and hands the source the result, closing the view as applied.
func TestApplyAsksThenHandsTheSourceTheResult(t *testing.T) {
	var asked [][2]int
	src := &source{in: threeWay()}
	r := start(t, src, mergeview.Options{ConfirmApply: func(ch, co int, apply func()) {
		asked = append(asked, [2]int{ch, co})
		apply()
	}}, 120, 14)
	r.loaded()
	r.h.OnLoop(r.v.Merge)
	// The conflict left unresolved keeps the buffer's lines: what Apply writes there is the user's.
	want := lines("alpha", "BETA", "gamma", "DELTA", "x", "omega-r")
	if got := r.result(); got != want {
		t.Fatalf("Merge: %q", got)
	}
	r.h.OnLoop(r.v.Apply)
	r.screen("the apply", func(string) bool { _, a := src.counts(); return len(a) == 1 })
	if _, a := src.counts(); a[0] != want || len(asked) != 1 || asked[0] != [2]int{1, 1} {
		t.Fatalf("applied %q, asked %v; want the result once, asked with one conflict left", a, asked)
	}
	r.screen("the close", func(string) bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.closed) == 1 && r.closed[0] })
}

// An Apply error keeps the view open and is said in the bar; Cancel closes it without applying.
func TestAnApplyErrorStaysInTheBar(t *testing.T) {
	src := &source{in: threeWay(), applyErr: errors.New("the disk is full")}
	r := start(t, src, mergeview.Options{ConfirmApply: func(_, _ int, apply func()) { apply() }}, 160, 14)
	r.loaded()
	r.h.OnLoop(r.v.Apply)
	r.shows("the error in the bar", "Could not apply: the disk is full")
	r.mu.Lock()
	n := len(r.closed)
	r.mu.Unlock()
	if n != 0 {
		t.Fatal("the view closed on an Apply error")
	}
	r.h.OnLoop(r.v.Cancel)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.closed) != 1 || r.closed[0] {
		t.Fatalf("Cancel closed %v, want once, not applied", r.closed)
	}
	if _, a := src.counts(); len(a) != 1 {
		t.Fatalf("Cancel applied: %q", a)
	}
	r.mu.Unlock()
	r.h.OnLoop(r.v.Apply) // after Cancel: nothing
	r.h.OnLoop(func() { r.v.Take(0, merge.Left) })
	r.h.Sync()
	r.mu.Lock()
	if _, a := src.counts(); len(a) != 1 {
		t.Fatalf("Apply after Cancel applied: %q", a)
	}
	if got := r.result(); got != threeWay().Right {
		t.Fatalf("Take after Cancel changed the result: %q", got)
	}
}

// A side changing under the view offers a reload; reloading an untouched result loads at once,
// one the user changed asks first.
func TestAChangedSideOffersAReloadThatAsksOverATouchedResult(t *testing.T) {
	src := &source{in: threeWay(), changed: make(chan struct{}, 1)}
	asked := 0
	r := start(t, src, mergeview.Options{ConfirmReload: func(reload func()) { asked++; reload() }}, 160, 14)
	r.loaded()
	src.changed <- struct{}{}
	s := r.shows("the reload offer", "A side changed under the merge", "Reload")
	x, y, _ := find(s, "Reload")
	r.click(x+1, y)
	r.screen("the second load", func(string) bool { n, _ := src.counts(); return n == 2 })
	if asked != 0 {
		t.Fatal("a reload over an untouched result asked")
	}
	r.loaded()
	r.h.OnLoop(r.v.AcceptLeft)
	r.h.OnLoop(r.v.Reload)
	r.screen("the third load", func(string) bool { n, _ := src.counts(); return n == 3 })
	if asked != 1 {
		t.Fatalf("a reload over a touched result asked %d times, want once", asked)
	}
}

// A source's text that is not text is said in the view.
func TestNotTextIsSaidInTheView(t *testing.T) {
	r := start(t, &source{loadErr: fmt.Errorf("reading: %w", merge.ErrNotText)}, mergeview.Options{}, 80, 10)
	r.shows("not text", "Not text: there is nothing to merge by lines")
}

// An undo in the result pane undoes a take: the result, and the change unresolved again; a redo
// takes it again.
func TestAnUndoInTheResultUndoesATake(t *testing.T) {
	r := start(t, &source{in: threeWay()}, mergeview.Options{}, 120, 14)
	r.loaded()
	r.h.OnLoop(func() { r.v.Take(0, merge.Left) })
	if ch, _ := r.counts(); ch != 2 {
		t.Fatalf("after a take: %d changes", ch)
	}
	r.focus(1, 0)
	r.keys(key('u'))
	r.resultIs("u undoing the take", threeWay().Right)
	if ch, _ := r.counts(); ch != 3 {
		t.Fatalf("after u: %d changes unresolved, want 3", ch)
	}
	// A redo brings the take back as a take, not as a hand edit: its revision was settled.
	r.keys(tui.KeyEvent{Kind: tui.KeyPress, Code: 'r', Mods: tui.ModCtrl})
	r.resultIs("Ctrl+R redoing the take", lines("alpha", "BETA", "gamma", "DELTA", "x", "omega-r"))
	r.until("the redone change taken from the left", func() bool {
		return r.v.Model().Changes()[0].State == merge.TookLeft
	})
}

// Scrolling one pane brings the others to the same place: the same line of the same hunk.
// scrollInput is 80 lines, the left with three lines added after the tenth, the right with its
// fiftieth changed.
func scrollInput() merge.Input {
	var base, left, right []string
	for i := range 80 {
		base = append(base, fmt.Sprintf("line %02d", i))
	}
	left = append(append(append([]string{}, base[:10]...), "added 1", "added 2", "added 3"), base[10:]...)
	right = append([]string{}, base...)
	right[50] = "changed"
	return merge.Input{HasBase: true, Base: lines(base...), Left: lines(left...), Right: lines(right...),
		LeftFrom: disk, RightFrom: buffer, ResultTo: buffer, ResultSide: merge.Right}
}

func TestScrollingOnePaneScrollsTheOthers(t *testing.T) {
	r := start(t, &source{in: scrollInput()}, mergeview.Options{}, 120, 14)
	r.loaded()
	r.h.OnLoop(func() { _, _, rt := r.v.Panes(); rt.SetTopLine(40) })
	var tops [3]int
	r.h.OnLoop(func() {
		l, res, rt := r.v.Panes()
		tops = [3]int{l.TopLine(), res.TopLine(), rt.TopLine()}
	})
	if tops != [3]int{43, 40, 40} {
		t.Fatalf("the right pane at 40: tops %v, want the left 3 lines lower (its added lines) and the result with the right", tops)
	}
}

// One wheel step in a pane moves each pane once: the others follow it, and their following never
// moves it back. The left pane is wheeled into its added lines, where the result and the right have
// none: they stay on the hunk's place, and the left stays where the wheel put it.
func TestAWheelStepMovesEachPaneOnce(t *testing.T) {
	r := start(t, &source{in: scrollInput()}, mergeview.Options{}, 120, 14)
	r.loaded()
	r.h.OnLoop(func() { l, _, _ := r.v.Panes(); l.SetTopLine(10) })
	r.keys(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 5, Y: 5})
	r.until("the wheel step reaching the left pane", func() bool { l, _, _ := r.v.Panes(); return l.TopLine() != 10 })
	var tops [3]int
	r.h.OnLoop(func() {
		l, res, rt := r.v.Panes()
		tops = [3]int{l.TopLine(), res.TopLine(), rt.TopLine()}
	})
	if tops[0] <= 10 || tops[0] >= 13 || tops[1] != 10 || tops[2] != 10 {
		t.Fatalf("tops %v after a wheel step in the left pane from 10: want the left inside its added lines 10-12, the others at 10", tops)
	}
}

// The lines' tints read in a sepia page and a dark one: each tint is not the page, and the text
// keeps its contrast on it. The look of each pane's marked line is logged as the dump.
func TestTheTintsReadInSepiaAndDark(t *testing.T) {
	themes := map[string]style.Theme{
		"sepia": style.NewTheme(style.RGB(0x8a, 0x73, 0x50), style.WithDark(false),
			style.WithToken(style.TokenBackground, style.RGB(0xe6, 0xd9, 0xb9)),
			style.WithToken(style.TokenForeground, style.RGB(0x5b, 0x46, 0x36))),
		"dark": style.NewTheme(style.RGB(0x5f, 0x87, 0xd7), style.WithDark(true),
			style.WithToken(style.TokenBackground, style.RGB(0x1c, 0x1c, 0x1c)),
			style.WithToken(style.TokenForeground, style.RGB(0xd0, 0xd0, 0xd0))),
	}
	pages := map[string][3]uint8{"sepia": {0xe6, 0xd9, 0xb9}, "dark": {0x1c, 0x1c, 0x1c}}
	inks := map[string][3]uint8{"sepia": {0x5b, 0x46, 0x36}, "dark": {0xd0, 0xd0, 0xd0}}
	for name, th := range themes {
		t.Run(name, func(t *testing.T) {
			r := start(t, &source{in: threeWay()}, mergeview.Options{}, 120, 14, tui.WithTheme(&th))
			r.loaded()
			s := r.shows("the panes", "BETA", "OMEGA-L")
			snap := r.h.TB.Snapshot()
			for _, text := range []string{"BETA", "OMEGA-L", "omega-r", "DELTA"} {
				x, y, _ := find(s, text)
				c := snap[y][x]
				fg, bg := c.Attrs.FG, c.Attrs.BG
				t.Logf("%s %q: fg %02x%02x%02x bg %02x%02x%02x", name, text, fg.R, fg.G, fg.B, bg.R, bg.G, bg.B)
				if bg.Kind != tui.CellColorRGB {
					t.Fatalf("%q's background is not in RGB: %+v", text, c.Attrs)
				}
				if fg.Kind == tui.CellColorDefault { // the text in the theme's own foreground
					fg = tui.CellColor{Kind: tui.CellColorRGB, R: inks[name][0], G: inks[name][1], B: inks[name][2]}
				}
				ratio, _ := style.ContrastRatio(style.RGB(fg.R, fg.G, fg.B), style.RGB(bg.R, bg.G, bg.B))
				if ratio < 4.5 {
					t.Errorf("%q reads at %.1f:1 on its tint, under 4.5:1", text, ratio)
				}
				if p := pages[name]; bg.R == p[0] && bg.G == p[1] && bg.B == p[2] {
					t.Errorf("%q's line is not tinted: the page's own %02x%02x%02x", text, p[0], p[1], p[2])
				}
			}
		})
	}
}

// With nowhere to ask (no dialog host, no Confirm option), Apply with changes left and Reload over a
// touched result are not done, and the bar says so: an unasked one could drop the user's work.
func TestWithNowhereToAskApplyAndReloadAreNotDone(t *testing.T) {
	src := &source{in: threeWay()}
	r := start(t, src, mergeview.Options{}, 160, 14)
	r.loaded()
	r.h.OnLoop(r.v.Apply)
	r.shows("the refusal", "Not done: it needs a confirmation, and there is nowhere to ask")
	if _, a := src.counts(); len(a) != 0 {
		t.Fatalf("Apply with changes left and nowhere to ask applied %q", a)
	}
	r.h.OnLoop(func() { r.v.Take(0, merge.Left) })
	r.h.OnLoop(r.v.Reload)
	r.h.Sync()
	if n, _ := src.counts(); n != 1 {
		t.Fatalf("Reload over a touched result, nowhere to ask: %d loads, want the first alone", n)
	}
	if got := r.result(); got != lines("alpha", "BETA", "gamma", "DELTA", "x", "omega-r") {
		t.Fatalf("the result after the refused reload: %q", got)
	}
}

// A reload that fails keeps the merge shown, its result and Apply: the error is in the bar, and
// Reload is offered again.
func TestAFailedReloadKeepsTheMergeAndApply(t *testing.T) {
	src := &source{in: threeWay()}
	r := start(t, src, mergeview.Options{
		ConfirmApply:  func(_, _ int, apply func()) { apply() },
		ConfirmReload: func(reload func()) { reload() },
	}, 160, 14)
	r.loaded()
	r.h.OnLoop(func() { r.v.Take(0, merge.Left) })
	src.failLoads(errors.New("the disk is gone"))
	r.h.OnLoop(func() { r.v.Reload() })
	r.shows("the failed reload in the bar, the merge still shown", "Could not load: the disk is gone", "BETA", "OMEGA-L", "Reload", "Apply")
	want := lines("alpha", "BETA", "gamma", "DELTA", "x", "omega-r")
	if got := r.result(); got != want {
		t.Fatalf("after the failed reload: %q, want the result kept", got)
	}
	r.h.OnLoop(r.v.Apply)
	r.screen("the apply", func(string) bool { _, a := src.counts(); return len(a) == 1 })
	if _, a := src.counts(); a[0] != want {
		t.Fatalf("applied %q, want the kept result", a[0])
	}
}

// The result is read-only while an Apply runs, so nothing is typed that the applied text lacks; an
// Apply that fails gives it back.
func TestTheResultIsReadOnlyWhileItIsApplied(t *testing.T) {
	src := &source{in: threeWay(), hold: make(chan struct{}), applyErr: errors.New("refused")}
	r := start(t, src, mergeview.Options{ConfirmApply: func(_, _ int, apply func()) { apply() }}, 160, 14)
	r.loaded()
	r.h.OnLoop(r.v.Apply)
	readOnly := func() (ro bool) {
		r.h.OnLoop(func() { _, res, _ := r.v.Panes(); ro = res.Core().ReadOnly() })
		return ro
	}
	if !readOnly() {
		t.Fatal("the result is editable while it is applied")
	}
	close(src.hold)
	r.shows("the refusal", "Could not apply: refused")
	if readOnly() {
		t.Fatal("the result stayed read-only after the Apply failed")
	}
}

// ] moves to a change starting on the line of the one it is on: two changes can, once the lines
// between them are gone.
func TestNextChangeReachesAChangeOnTheSameLine(t *testing.T) {
	in := merge.Input{HasBase: true,
		Base: lines("a", "b", "c", "d", "e"), Left: lines("a", "c", "d", "E"), Right: lines("a", "b", "c", "d", "e"),
		LeftFrom: disk, RightFrom: buffer, ResultTo: buffer}
	r := start(t, &source{in: in}, mergeview.Options{}, 120, 14)
	r.loaded()
	r.h.OnLoop(func() { r.v.Take(0, merge.Left) }) // b deleted: the first change holds no lines, at line 1
	r.h.OnLoop(func() {                            // c and d deleted by hand: the second change starts at line 1 too
		_, res, _ := r.v.Panes()
		_, _ = res.Core().Replace(editor.TextRange{From: editor.TextPos{Line: 1}, To: editor.TextPos{Line: 3}}, "")
	})
	r.until("the result a, e", func() bool { return r.v.Result() == lines("a", "e") })
	r.focus(1, 0)
	r.keys(key(']'))
	r.until("] at the first change, line 1", func() bool { _, res, _ := r.v.Panes(); return res.Core().Cursor().Line == 1 })
	r.keys(key(']'))
	r.keys(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight, Mods: tui.ModCtrl | tui.ModShift}) // take the left's
	r.resultIs("the second ] then taking the left: the second change's E", lines("a", "E"))
}
