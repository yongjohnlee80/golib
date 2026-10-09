package widget

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// nativeTB is a TestBackend that hosts native views: it records each frame's placements.
type nativeTB struct {
	*tui.TestBackend
	mu     sync.Mutex
	placed []tui.NativePlacement
}

func (n *nativeTB) PlaceNatives(ps []tui.NativePlacement) {
	n.mu.Lock()
	n.placed = append([]tui.NativePlacement(nil), ps...)
	n.mu.Unlock()
}

type edHarness struct {
	t    *testing.T
	app  *tui.App
	tb   *tui.TestBackend
	sh   *shell
	e    *Editor
	cell gui.Size
}

// shell is the test's root: it holds the widget under test and counts the barrier key, which no
// widget consumes, so its arrival says every event before it was handled.
type shell struct {
	child    tui.Component
	ctx      *tui.Context
	barriers atomic.Int64
}

const barrierKey = tui.KeyF12

func (s *shell) Init(ctx *tui.Context) { s.ctx = ctx; ctx.Mount(s.child) }
func (s *shell) Render(tui.Surface)    {}
func (s *shell) Layout(c tui.Constraints) tui.Size {
	if !s.ctx.MountedComponent(s.child) {
		return c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH})
	}
	sz := s.ctx.LayoutChild(s.child, tui.Tight(tui.Size{W: c.MaxW, H: c.MaxH}))
	s.ctx.PlaceChild(s.child, tui.Rect{W: sz.W, H: sz.H})
	return c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH})
}
func (s *shell) HandleEvent(ev tui.Event) bool {
	if k, ok := ev.(tui.KeyEvent); ok && k.Code == barrierKey {
		s.barriers.Add(1)
		return true
	}
	return false
}

// barrier waits until every event injected before it was handled.
func (h *edHarness) barrier() {
	h.t.Helper()
	want := h.sh.barriers.Load() + 1
	if err := h.tb.Inject(tui.KeyEvent{Code: barrierKey}); err != nil {
		h.t.Fatal(err)
	}
	h.until("the input barrier", func() bool { return h.sh.barriers.Load() >= want })
}

const cellW, cellH = 8, 16

func startEditor(t *testing.T, w, h int, opts ...EditorOption) *edHarness {
	t.Helper()
	e := NewEditor(opts...)
	tb := tui.NewTestBackend(w, h, tui.WithTestCapabilities(tui.Capabilities{NativeViews: true}))
	sh := &shell{child: e}
	app := tui.NewApp(sh, tui.WithBackend(&nativeTB{TestBackend: tb}))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go app.Run(ctx)
	hh := &edHarness{t: t, app: app, tb: tb, sh: sh, e: e, cell: gui.Size{W: cellW, H: cellH}}
	hh.until("a frame", func() bool { return tb.Flushes() > 0 })
	hh.onLoop(func() { app.FocusInto(sh) })
	hh.barrier()
	hh.paint()
	return hh
}

func (h *edHarness) onLoop(fn func()) {
	h.t.Helper()
	done := make(chan struct{})
	h.app.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		h.t.Fatal("the loop did not run the update")
	}
}

func (h *edHarness) until(what string, cond func() bool) {
	h.t.Helper()
	for range 300 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s; screen:\n%s", what, h.tb.String())
}

// paint paints the body's native view on a recording canvas the body's size, on the loop, as a
// gui backend would: the pixel layout then has a shaper and a width.
func (h *edHarness) paint() *gui.RecordingCanvas {
	h.t.Helper()
	var rc *gui.RecordingCanvas
	h.onLoop(func() {
		rc = gui.NewRecordingCanvas(gui.Size{W: float32(h.e.body.w) * cellW, H: float32(h.e.body.h) * cellH}, h.cell)
		v, _ := h.e.body.NativeView()
		v.(gui.View).Paint(rc)
	})
	return rc
}

func (h *edHarness) keys(ks ...tui.KeyEvent) {
	h.t.Helper()
	for _, k := range ks {
		if err := h.tb.Inject(k); err != nil {
			h.t.Fatal(err)
		}
	}
	h.barrier()
}

// bodyOrigin is the body's top-left cell on the screen: under the title row.
func (h *edHarness) bodyOrigin() (int, int) { return 0, 1 }

// clickAt presses and releases at a point of the body's view, in logical pixels.
func (h *edHarness) clickAt(x, y float32) {
	h.t.Helper()
	ox, oy := h.bodyOrigin()
	cx, cy := int(x/cellW), int(y/cellH)
	m := tui.MouseEvent{Button: tui.MouseLeft, X: ox + cx, Y: oy + cy, SubX: x/cellW - float32(cx), SubY: y/cellH - float32(cy)}
	m.Kind = tui.MousePress
	_ = h.tb.Inject(m)
	m.Kind = tui.MouseRelease
	_ = h.tb.Inject(m)
	h.barrier()
}

func (h *edHarness) line() (int, int) {
	var ln, col int
	h.onLoop(func() { ln, col = h.e.core.Line() })
	return ln, col
}

// caretPoint is the middle of the caret's place at (line, col), in the body's view pixels.
func (h *edHarness) caretPoint(line, col int) (float32, float32) {
	var x, y float32
	h.onLoop(func() {
		r, ok := h.e.layout.caretAt(line, col)
		if !ok {
			h.t.Errorf("no caret at %d:%d", line, col)
		}
		x, y = h.e.layout.textX()+r.X+0.5, padY+r.Y-h.e.layout.scroll+r.H/2
	})
	return x, y
}

// key is a printable key as a backend sends it: its code and the text it types.
func key(r rune) tui.KeyEvent  { return tui.KeyEvent{Code: r, Text: string(r)} }
func ctrl(r rune) tui.KeyEvent { return tui.KeyEvent{Code: r, Mods: tui.ModCtrl} }
func code(c rune) tui.KeyEvent { return tui.KeyEvent{Code: c} }
func text(s string) (out []tui.KeyEvent) {
	for _, r := range s {
		out = append(out, key(r))
	}
	return out
}

// One key script gives the same text, cursor and mode on the tui Editor and the gui Editor in Raw.
func TestKeyScriptMatchesTheTuiEditor(t *testing.T) {
	initial := "alpha beta\ngamma\ndelta epsilon zeta\nx"
	script := append([]tui.KeyEvent{}, text("ji")...)
	script = append(script, code(tui.KeyEscape))
	script = append(script, text("llvlly")...)
	script = append(script, text("Gp")...)
	script = append(script, key('u'), ctrl('r'))
	script = append(script, text("gg2jdd")...)
	script = append(script, text("kkllljjk")...)
	script = append(script, text("3x")...)
	script = append(script, key('i'))
	script = append(script, text("ok")...)
	script = append(script, code(tui.KeyEscape))

	g := startEditor(t, 60, 12, WithCore(tuiwidget.CoreInitialText(initial), tuiwidget.CoreSelection(true), tuiwidget.CoreYank(true), tuiwidget.CoreUndo(true)))
	g.keys(script...)

	te := tuiwidget.NewEditor(tuiwidget.WithInitialText(initial), tuiwidget.WithSelection(true), tuiwidget.WithYank(true), tuiwidget.WithUndo(true))
	tb := tui.NewTestBackend(60, 12)
	tsh := &shell{child: te}
	app := tui.NewApp(tsh, tui.WithBackend(tb))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.Run(ctx)
	th := &edHarness{t: t, app: app, tb: tb, sh: tsh}
	th.until("a frame", func() bool { return tb.Flushes() > 0 })
	th.onLoop(func() { app.FocusInto(tsh) })
	th.barrier()
	th.keys(script...)

	var gv, tv string
	var gl, gc, tl, tc int
	var gm, tm tuiwidget.EditorMode
	g.onLoop(func() { gv, gm = g.e.core.Value(), g.e.core.Mode(); gl, gc = g.e.core.Line() })
	th.onLoop(func() { tv, tm = te.Value(), te.Mode(); tl, tc = te.Line() })
	if gv == initial {
		t.Fatalf("the script changed nothing: %q", gv)
	}
	t.Logf("both editors: %q at %d:%d", gv, gl, gc)
	if gv != tv || gl != tl || gc != tc || gm != tm {
		t.Fatalf("gui Editor: %q at %d:%d mode %v\ntui Editor: %q at %d:%d mode %v", gv, gl, gc, gm, tv, tl, tc, tm)
	}
}

// A click lands on the cluster under it, in pixels: mid-line, at a line's end, on a wrapped row.
func TestClicksLandOnTheirClustersInRaw(t *testing.T) {
	long := strings.Repeat("word ", 30)
	h := startEditor(t, 40, 12, WithWrap(tuiwidget.WrapSoft), WithCore(tuiwidget.CoreInitialText("hello world\n"+long+"\nend")))
	// In Normal mode a click past a line's end lands on its last cluster, as the tui Editor's does.
	for _, c := range []struct{ line, col, want int }{{0, 3, 3}, {0, 11, 10}, {1, 0, 0}, {2, 1, 1}, {2, 3, 2}} {
		x, y := h.caretPoint(c.line, c.col)
		h.clickAt(x, y)
		if ln, col := h.line(); ln != c.line || col != c.want {
			t.Errorf("click at the caret place of %d:%d landed on %d:%d, want %d:%d", c.line, c.col, ln, col, c.line, c.want)
		}
	}
	// the long line wraps: its second row starts at a cluster past the first row's end
	var rows int
	h.onLoop(func() { rows = len(h.e.layout.lay(h.e.layout.blockOf(1), nil).bl.Lines[0].Para.Lines) })
	if rows < 2 {
		t.Fatalf("the long line did not wrap: %d rows", rows)
	}
	var wrapCol int
	h.onLoop(func() {
		p := h.e.layout.lay(h.e.layout.blockOf(1), nil).bl.Lines[0].Para
		wrapCol = clusters(long[:p.Lines[1].Frags[0].From])
	})
	x, y := h.caretPoint(1, wrapCol+2)
	h.clickAt(x, y)
	if ln, col := h.line(); ln != 1 || col != wrapCol+2 {
		t.Errorf("a click on the wrapped row landed on %d:%d, want 1:%d", ln, col, wrapCol+2)
	}
}

func spansOfLine(h *edHarness, ln int) []flow.Span {
	var out []flow.Span
	h.onLoop(func() {
		lb := h.e.layout.lay(h.e.layout.blockOf(ln), nil)
		for _, ll := range lb.bl.Lines {
			if len(ll.Spans) > 0 && ll.Spans[0].Line == ln {
				out = ll.Spans
			}
		}
	})
	return out
}

func TestRenderedHidesMarksOffTheCursorAndRevealsThemOnIt(t *testing.T) {
	h := startEditor(t, 60, 14, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText("first\n## Title here\nplain *em* **strong** `code` [link](x)\n```go\nx := 1\n```\nlast")))
	h.paint()

	head := spansOfLine(h, 1)
	if len(head) < 2 || !head[0].Hidden || head[0].Text != "## " {
		t.Fatalf("heading marks not hidden: %+v", head)
	}
	if head[1].Font.Size <= h.e.layout.th.Prose.Size || !head[1].Font.Bold {
		t.Errorf("h2 at %v, text at %v: want larger and bold", head[1].Font.Size, h.e.layout.th.Prose.Size)
	}

	in := spansOfLine(h, 2)
	var em, strong, codeSp, link, hiddenStars bool
	for _, s := range in {
		switch {
		case s.Hidden && strings.Trim(s.Text, "*`[]()x") == "":
			hiddenStars = true
		case s.Text == "em" && s.Font.Italic:
			em = true
		case s.Text == "strong" && s.Font.Bold:
			strong = true
		case s.Text == "code" && s.Background.A > 0:
			codeSp = true
		case s.Text == "link" && s.Underline:
			link = true
		}
	}
	if !em || !strong || !codeSp || !link || !hiddenStars {
		t.Errorf("inline styles: em %v strong %v code %v link %v marks hidden %v\n%+v", em, strong, codeSp, link, hiddenStars, in)
	}

	var fence BlockLayout
	h.onLoop(func() { fence = h.e.layout.lay(h.e.layout.blockOf(4), nil).bl })
	if fence.Background.A == 0 || len(fence.Lines) != 3 {
		t.Errorf("the fenced block: background %v, %d lines", fence.Background, len(fence.Lines))
	}

	// a click at the heading's left edge lands past its hidden marks, on its text
	x, y := h.caretPoint(1, 3)
	h.clickAt(padX+0.5, y)
	if ln, col := h.line(); ln != 1 || col != 3 {
		t.Errorf("click at the heading's start landed on %d:%d, want 1:3 (x %v)", ln, col, x)
	}
	// the cursor's line now shows its source: no hidden spans, monospace
	h.paint()
	for _, s := range spansOfLine(h, 1) {
		if s.Hidden || s.Font != h.e.layout.th.Mono {
			t.Fatalf("the cursor's line is not Raw: %+v", s)
		}
	}
	// and the line it left is rendered again
	h.keys(key('j'))
	h.paint()
	if s := spansOfLine(h, 1); len(s) < 2 || !s[0].Hidden {
		t.Errorf("the heading left by the cursor shows its marks: %+v", s)
	}
}

func TestModeSwitching(t *testing.T) {
	h := startEditor(t, 60, 8, WithRenderer(NewMarkdownRenderer()), WithCore(tuiwidget.CoreInitialText("# hi")))
	mode := func() EditorMode {
		var m EditorMode
		h.onLoop(func() { m = h.e.Mode() })
		return m
	}
	if mode() != Raw {
		t.Fatal("the default mode is not Raw")
	}
	h.keys(ctrl('t'))
	if mode() != Rendered {
		t.Fatal("Ctrl+T did not switch to Rendered")
	}
	// the title bar's switch: "[ Raw " starts at column 1
	click := func(x int) {
		_ = h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: 0})
		h.barrier()
	}
	click(1 + 2)
	if mode() != Raw {
		t.Fatal("a click on Raw did not switch")
	}
	click(1 + len(switchRaw) + 3)
	if mode() != Rendered {
		t.Fatal("a click on Rendered did not switch")
	}

	plain := startEditor(t, 60, 8, WithCore(tuiwidget.CoreInitialText("x")))
	plain.keys(ctrl('t'))
	var m EditorMode
	plain.onLoop(func() { m = plain.e.Mode() })
	if m != Raw {
		t.Fatal("Rendered without a renderer: the mode must stay Raw")
	}
}

func TestNativePaintReportsTheCaret(t *testing.T) {
	h := startEditor(t, 40, 6, WithCore(tuiwidget.CoreInitialText("abc\ndef")))
	h.keys(key('j'), key('l'))
	rc := h.paint()
	texts := 0
	for _, c := range rc.Calls {
		if c.Op == "DrawText" {
			texts++
		}
	}
	if texts < 2 {
		t.Errorf("drew %d texts, want both lines", texts)
	}
	var r gui.Rect
	var ok bool
	h.onLoop(func() { r, _, ok = bodyView{h.e.body}.Caret() })
	wx, wy := h.caretPoint(1, 1)
	if !ok || r.Y > wy || r.Y+r.H < wy || r.X > wx || r.X+r.W+1 < wx {
		t.Errorf("caret %+v ok %v, want it over %v,%v", r, ok, wx, wy)
	}
}

func TestNativeCaretAdvancesAfterSpaces(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode EditorMode
	}{{"raw", Raw}, {"rendered", Rendered}} {
		t.Run(tc.name, func(t *testing.T) {
			d := markdown.Definition()
			h := startEditor(t, 40, 6, WithMode(tc.mode), WithRenderer(NewMarkdownRenderer()),
				WithCore(tuiwidget.CoreInitialText(""), tuiwidget.CoreAutoIndent(true),
					tuiwidget.CoreSourceFactory(func() highlight.Source { return d.NewSource(nil) })))
			h.keys(key('i'))
			var previousX float32
			for step := 0; step < 3; step++ {
				h.keys(key(' '))
				h.paint()
				var caret gui.Rect
				var ok bool
				h.onLoop(func() { caret, _, ok = bodyView{h.e.body}.Caret() })
				if !ok || caret.X <= previousX {
					t.Fatalf("after %d spaces caret = %+v, visible = %v, previous x = %v", step+1, caret, ok, previousX)
				}
				previousX = caret.X
			}
		})
	}
}

// A click on a wrapped row of a Rendered line, with hidden marks before it, lands on the cluster
// under it: the Rendered layout maps its frags one to one back to source clusters.
func TestClickOnAWrappedRenderedRow(t *testing.T) {
	long := "*lead* " + strings.Repeat("word ", 30)
	h := startEditor(t, 40, 12, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText("top\n"+long+"\nend")))
	h.paint()
	if s := spansOfLine(h, 1); len(s) == 0 || !s[0].Hidden {
		t.Fatalf("line 1 is not Rendered off the cursor: %+v", s)
	}
	var wrapCol int
	h.onLoop(func() {
		p := h.e.layout.lay(h.e.layout.blockOf(1), nil).bl.Lines[0]
		if len(p.Para.Lines) < 2 {
			t.Fatalf("the Rendered line did not wrap: %d rows", len(p.Para.Lines))
		}
		f := p.Para.Lines[1].Frags[0]
		wrapCol = p.Spans[f.Span].Col + clusters(p.Spans[f.Span].Text[:f.From])
	})
	x, y := h.caretPoint(1, wrapCol+2)
	h.clickAt(x, y)
	if ln, col := h.line(); ln != 1 || col != wrapCol+2 {
		t.Errorf("a click on the Rendered line's wrapped row landed on %d:%d, want 1:%d", ln, col, wrapCol+2)
	}
}

// Ctrl+T toggles through the keymap's ActToggleRendered, so a keymap can move it: here to
// Ctrl+G, with Ctrl+T unbound. With no renderer the action is not consumed.
func TestToggleRenderedIsTheKeymapsAndRebindable(t *testing.T) {
	ctrlKey := func(r rune) tui.KeyEvent { return tui.KeyEvent{Code: r, Mods: tui.ModCtrl} }
	e := NewEditor(WithRenderer(NewMarkdownRenderer()))
	if !e.Core().HandleKey(ctrlKey('t')) || e.Mode() != Rendered {
		t.Fatalf("Ctrl+T: mode %v, want Rendered", e.Mode())
	}
	moved := NewEditor(WithRenderer(NewMarkdownRenderer()), WithCore(tuiwidget.CoreKeymap(tuiwidget.Keymap{
		{Mode: tuiwidget.ModeNormal, Code: 'g', Ctrl: true}: tuiwidget.ActToggleRendered,
		{Mode: tuiwidget.ModeNormal, Code: 't', Ctrl: true}: tuiwidget.ActUnbound,
	})))
	if moved.Core().HandleKey(ctrlKey('t')) || moved.Mode() != Raw {
		t.Errorf("Ctrl+T unbound still toggled: mode %v", moved.Mode())
	}
	if !moved.Core().HandleKey(ctrlKey('g')) || moved.Mode() != Rendered {
		t.Errorf("Ctrl+G rebound: mode %v, want Rendered", moved.Mode())
	}
	if NewEditor().Core().HandleKey(ctrlKey('t')) {
		t.Error("an editor with no renderer consumed Ctrl+T")
	}
}

// A host turns the Rendered view off for a document its renderer does not read, and on again: off,
// the view is Raw, SetMode(Rendered) stays Raw and Ctrl+T is not consumed; the renderer is kept.
func TestRenderedEnabledSwitch(t *testing.T) {
	h := startEditor(t, 60, 12, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText("# Title\nbody")))
	mode := func() EditorMode {
		var m EditorMode
		h.onLoop(func() { m = h.e.Mode() })
		return m
	}
	if mode() != Rendered {
		t.Fatal("did not start Rendered")
	}
	h.onLoop(func() { h.e.SetRenderedEnabled(false) })
	if mode() != Raw {
		t.Fatal("turning the Rendered view off left it Rendered")
	}
	h.onLoop(func() { h.e.SetMode(Rendered) })
	if mode() != Raw {
		t.Fatal("SetMode(Rendered) with the view off is not Raw")
	}
	h.keys(ctrl('t'))
	if mode() != Raw {
		t.Fatal("Ctrl+T toggled to Rendered with the view off")
	}
	var on bool
	h.onLoop(func() { h.e.SetRenderedEnabled(true); on = h.e.RenderedEnabled() })
	h.keys(ctrl('t'))
	if !on || mode() != Rendered {
		t.Fatalf("turned back on (enabled %v): Ctrl+T gives %v, want Rendered", on, mode())
	}
}

// A '#' inside a code block is code, never a heading: in a fenced block, an indented block and a
// tilde fence the line keeps its marks, monospace, on the code background. A real heading beside
// them still renders, and an indented line under a paragraph stays the paragraph's.
func TestHashInACodeBlockIsNotAHeading(t *testing.T) {
	doc := "# Real heading\n```\n# fenced hash\n```\n\n    # indented hash\n\n~~~\n## tilde hash\n~~~\npara\n    # not code\nend"
	h := startEditor(t, 60, 20, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText(doc)))
	h.keys(key('G')) // the cursor on the last line, so every block above lays out rendered
	h.paint()
	if s := spansOfLine(h, 0); len(s) < 2 || !s[0].Hidden || !s[1].Font.Bold {
		t.Fatalf("the real heading is not rendered: %+v", s)
	}
	for _, ln := range []int{2, 5, 8} {
		for _, s := range spansOfLine(h, ln) {
			if s.Hidden || s.Font.Bold || s.Font.Family != h.e.layout.th.Mono.Family {
				t.Errorf("line %d (%q) inside a code block is drawn as Markdown: %+v", ln, s.Text, s)
			}
		}
		var bg bool
		h.onLoop(func() { bg = h.e.layout.lay(h.e.layout.blockOf(ln), nil).bl.Background.A > 0 })
		if !bg {
			t.Errorf("line %d's code block has no code background", ln)
		}
	}
	var under BlockLayout
	h.onLoop(func() { under = h.e.layout.lay(h.e.layout.blockOf(11), nil).bl })
	if under.Background.A > 0 {
		t.Error("an indented line right under a paragraph was laid out as a code block")
	}
}

// Off the cursor a fence's characters are hidden and its language kept, muted; the closing fence
// is hidden whole. The lines keep their rows.
func TestFencesHideOffTheCursor(t *testing.T) {
	h := startEditor(t, 60, 20, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText("```go\nx :=\t1\n```\nend")))
	h.keys(key('G'))
	h.paint()
	open := spansOfLine(h, 0)
	if len(open) != 2 || !open[0].Hidden || open[0].Text != "```" || open[1].Hidden || open[1].Text != "go" {
		t.Errorf("opening fence spans %+v: want ``` hidden, go shown", open)
	}
	for _, s := range spansOfLine(h, 2) {
		if !s.Hidden {
			t.Errorf("closing fence shows %q", s.Text)
		}
	}
	h.keys(key('g'), key('g')) // the cursor in the block: its source, every mark shown
	h.paint()
	for _, s := range spansOfLine(h, 0) {
		if s.Hidden {
			t.Errorf("the fence under the cursor hides %q", s.Text)
		}
	}
}

// A list item's bullet is a bullet glyph by its level, a number stays, the indentation is room,
// and an indented item after a blank line inside a list is an item, not code.
func TestListItemsDrawBulletsAndNesting(t *testing.T) {
	doc := "- one\n  - two\n    - three\n1. first\n\n    - after blank\nend"
	h := startEditor(t, 60, 20, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText(doc)))
	h.keys(key('G'))
	h.paint()
	find := func(ln int, text string) (flow.Span, bool) {
		for _, s := range spansOfLine(h, ln) {
			if s.Text == text {
				return s, true
			}
		}
		return flow.Span{}, false
	}
	for ln, b := range map[int]string{0: "•", 1: "◦", 2: "▪", 3: "1.", 5: "▪"} {
		s, ok := find(ln, b)
		if !ok {
			t.Errorf("line %d: no %q in %+v", ln, b, spansOfLine(h, ln))
			continue
		}
		if s.Col != leadingSpaces(strings.Split(doc, "\n")[ln]) {
			t.Errorf("line %d: %q at column %d, not the marker's", ln, b, s.Col)
		}
	}
	if s, ok := find(1, "  "); !ok || s.SpaceWidth <= 0 {
		t.Errorf("line 1's indentation is not sized room: %+v", spansOfLine(h, 1))
	}
	var bg bool
	h.onLoop(func() { bg = h.e.layout.lay(h.e.layout.blockOf(5), nil).bl.Background.A > 0 })
	if bg {
		t.Error("an indented item after a blank line in a list was laid out as code")
	}
}

func leadingSpaces(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

// List editing is the Rendered Markdown view's: Raw is the source as typed.
func TestListEditingFollowsTheView(t *testing.T) {
	h := startEditor(t, 60, 8, WithRenderer(NewMarkdownRenderer()), WithCore(tuiwidget.CoreInitialText("- a")))
	on := func() bool {
		var v bool
		h.onLoop(func() { v = h.e.Core().ListEditing() })
		return v
	}
	if on() {
		t.Error("list editing on in Raw")
	}
	h.onLoop(func() { h.e.SetMode(Rendered) })
	if !on() {
		t.Error("list editing off in Rendered Markdown")
	}
	h.onLoop(func() { h.e.SetRenderedEnabled(false) })
	if on() {
		t.Error("list editing on with the Rendered view turned off")
	}
}

// In a code block, fenced or indented, "- x" is code: Enter and Tab there type as ever, while a
// list item in the prose around it continues.
func TestListEditingSkipsCodeBlocks(t *testing.T) {
	doc := "```\n- code\n```\n\npara\n\n    - indented code\n\n- item"
	h := startEditor(t, 60, 20, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText(doc), tuiwidget.CoreKeyset(tuiwidget.KeysetStandard)))
	at := func(ln, col int, k tui.KeyEvent) string {
		var v string
		h.onLoop(func() {
			h.e.Core().SetValue(doc)
			h.e.Core().SetLine(ln, col)
			h.e.Core().HandleKey(k)
			v = h.e.Core().Value()
		})
		return v
	}
	enter, tab := tui.KeyEvent{Code: tui.KeyEnter}, tui.KeyEvent{Code: tui.KeyTab}
	if got := at(1, 6, enter); got != strings.Replace(doc, "- code\n", "- code\n\n", 1) {
		t.Errorf("Enter in a fenced block continued a list:\n%s", got)
	}
	if got := at(1, 0, tab); got != strings.Replace(doc, "- code", "\t- code", 1) {
		t.Errorf("Tab in a fenced block nested an item:\n%s", got)
	}
	if got := at(6, 19, enter); got != strings.Replace(doc, "    - indented code\n", "    - indented code\n\n", 1) {
		t.Errorf("Enter in an indented code block continued a list:\n%s", got)
	}
	if got := at(8, 6, enter); got != doc+"\n- " {
		t.Errorf("Enter on a prose item did not continue the list:\n%s", got)
	}
}

// A table's rows are not a list: Enter on a row is a plain line break, whatever the row holds.
func TestListEditingSkipsTables(t *testing.T) {
	doc := "| - a | b |\n|---|---|\n| - c | d |"
	h := startEditor(t, 60, 20, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText(doc), tuiwidget.CoreKeyset(tuiwidget.KeysetStandard)))
	var got string
	var where bool
	h.onLoop(func() {
		where = h.e.render.(*MarkdownRenderer).InTable(strings.Split(doc, "\n"), 2)
		h.e.Core().SetLine(2, len("| - c | d |"))
		h.e.Core().HandleKey(tui.KeyEvent{Code: tui.KeyEnter})
		got = h.e.Core().Value()
	})
	if !where {
		t.Error("InTable: a body row read as outside its table")
	}
	if got != doc+"\n" {
		t.Errorf("Enter on a table row gave %q", got)
	}
}
