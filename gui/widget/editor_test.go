package widget

import (
	"context"
	"strconv"
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
	// in Normal the cursor's line stays rendered; typing (i) shows its source: no hidden
	// spans, monospace
	h.paint()
	if s := spansOfLine(h, 1); len(s) == 0 || !s[0].Hidden {
		t.Fatalf("the cursor's line in Normal is not rendered: %+v", s)
	}
	h.keys(key('i'))
	h.paint()
	for _, s := range spansOfLine(h, 1) {
		if s.Hidden || s.Font != h.e.layout.th.Mono {
			t.Fatalf("the cursor's line in Insert is not Raw: %+v", s)
		}
	}
	// and the line it left, by j after Esc, is rendered again
	h.keys(tui.KeyEvent{Code: tui.KeyEscape}, key('j'))
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
	// the title bar's switch, at its right: "[ Raw " starts where the switch's cells do
	at := 60 - len(switchRaw+switchSep+switchRendered)
	click := func(x int) {
		_ = h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: 0})
		h.barrier()
	}
	click(at + 2)
	if mode() != Raw {
		t.Fatal("a click on Raw did not switch")
	}
	click(at + len(switchRaw) + 3)
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

// A host turns the Rendered view off for a document its renderer does not read, and on again
// : off, Raw is drawn but Rendered stays the choice, SetMode(Rendered) records
// the choice and Ctrl+T is not consumed; on again, Rendered is drawn with no key; the renderer is
// kept. Raw chosen while off stays Raw when it comes back on.
func TestRenderedEnabledSwitch(t *testing.T) {
	h := startEditor(t, 60, 12, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText("# Title\nbody")))
	views := func() (EditorMode, EditorMode) {
		var m, sh EditorMode
		h.onLoop(func() { m, sh = h.e.Mode(), h.e.Shown() })
		return m, sh
	}
	if m, sh := views(); m != Rendered || sh != Rendered {
		t.Fatalf("did not start Rendered: chosen %v, drawn %v", m, sh)
	}
	h.onLoop(func() { h.e.SetRenderedEnabled(false) })
	if m, sh := views(); m != Rendered || sh != Raw {
		t.Fatalf("turned off: chosen %v, drawn %v; want Rendered kept, Raw drawn", m, sh)
	}
	h.keys(ctrl('t'))
	if m, sh := views(); m != Rendered || sh != Raw {
		t.Fatalf("Ctrl+T with the view off: chosen %v, drawn %v; want it not consumed", m, sh)
	}
	var on bool
	h.onLoop(func() { h.e.SetRenderedEnabled(true); on = h.e.RenderedEnabled() })
	if m, sh := views(); !on || m != Rendered || sh != Rendered {
		t.Fatalf("turned back on (enabled %v): chosen %v, drawn %v; want Rendered with no key", on, m, sh)
	}

	h.onLoop(func() { h.e.SetRenderedEnabled(false); h.e.SetMode(Raw); h.e.SetMode(Rendered) })
	if m, sh := views(); m != Rendered || sh != Raw {
		t.Fatalf("SetMode(Rendered) with the view off: chosen %v, drawn %v", m, sh)
	}
	h.onLoop(func() { h.e.SetMode(Raw); h.e.SetRenderedEnabled(true) })
	if m, sh := views(); m != Raw || sh != Raw {
		t.Fatalf("Raw chosen while off, then on: chosen %v, drawn %v; want Raw", m, sh)
	}
	h.keys(ctrl('t'))
	if m, _ := views(); m != Rendered {
		t.Fatalf("Ctrl+T turned back on: %v, want Rendered", m)
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
	h.keys(key('g'), key('g'), key('i')) // typing in the block: its source, every mark shown
	h.paint()
	for _, s := range spansOfLine(h, 0) {
		if s.Hidden {
			t.Errorf("the fence under the cursor in Insert hides %q", s.Text)
		}
	}
	h.keys(tui.KeyEvent{Code: tui.KeyEscape}) // Normal again: the block renders as every other does
	h.paint()
	if s := spansOfLine(h, 0); len(s) == 0 || !s[0].Hidden {
		t.Errorf("the fence under the cursor in Normal shows its marks: %+v", s)
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

// Table editing runs in the Markdown editor too, the table keys on (CoreTableEditing): Tab on a row
// goes to the next cell with its text selected, Shift+Tab back, and Tab past the table's last cell
// adds a row.
func TestTableEditingRunsInTheMarkdownEditor(t *testing.T) {
	doc := "| a | b |\n|---|---|\n| c | d |"
	h := startEditor(t, 60, 20, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText(doc), tuiwidget.CoreKeyset(tuiwidget.KeysetStandard), tuiwidget.CoreTableEditing(true)))
	tab, back := tui.KeyEvent{Code: tui.KeyTab}, tui.KeyEvent{Code: tui.KeyTab, Mods: tui.ModShift}
	var walk []string
	var added string
	var line int
	h.onLoop(func() {
		c := h.e.Core()
		selected := func() string { // the cell's selection on the cursor's line
			var b strings.Builder
			ln := c.Cursor().Line
			for col, r := range []rune(c.LineAt(ln)) {
				if c.Selected(ln, col) {
					b.WriteRune(r)
				}
			}
			return b.String()
		}
		c.SetLine(0, 2)
		for _, k := range []tui.KeyEvent{tab, back, tab, tab, tab} {
			c.HandleKey(k)
			walk = append(walk, selected())
		}
		c.HandleKey(tab)
		added, line = c.Value(), c.Cursor().Line
	})
	if strings.Join(walk, " ") != "b a b c d" {
		t.Errorf("Tab, Shift+Tab, Tab, Tab, Tab selected %q, want b a b c d", walk)
	}
	if added != doc+"\n|   |   |" || line != 3 {
		t.Errorf("Tab past the last cell gave %q, the cursor on line %d; want a new row, on it", added, line)
	}
}

// layCounter counts a renderer's LayOut calls, for the relayout cells.
type layCounter struct {
	Renderer
	calls int
}

func (c *layCounter) LayOut(b Block, lines []string, width float32, cursorInside bool, t *gui.TextShaper, th Theme, host DiagramHost) BlockLayout {
	c.calls++
	return c.Renderer.LayOut(b, lines, width, cursorInside, t, th, host)
}

// besideRenderer draws each block taller by extra: a renderer drawing from more than the text.
type besideRenderer struct {
	Renderer
	extra float32
}

func (r *besideRenderer) LayOut(b Block, lines []string, width float32, cursorInside bool, t *gui.TextShaper, th Theme, host DiagramHost) BlockLayout {
	out := r.Renderer.LayOut(b, lines, width, cursorInside, t, th, host)
	out.Height += r.extra
	return out
}

// laidAfterPaint is block i's layout as the last paint left it: nil when it was not drawn, and
// whether it is still the one from before a Relayout.
func laidAfterPaint(h *edHarness, i int) (height float32, laid, stale bool) {
	h.onLoop(func() {
		if lb := h.e.layout.laid[i]; lb != nil {
			height, laid, stale = lb.bl.Height, true, lb.stale
		}
	})
	return height, laid, stale
}

// Relayout lays every block out again, the text unchanged: what the renderer draws from beside the
// text changed, which no edit says. Until it is called the layouts kept stand; once called, the
// next paint draws the new one. The cursor and the text stay as they were.
func TestRelayoutLaysTheBlocksOutAgain(t *testing.T) {
	h := startEditor(t, 60, 14, WithRenderer(&besideRenderer{Renderer: NewMarkdownRenderer()}), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText("first\n## Title here\nlast")))
	r := h.e.render.(*besideRenderer)
	h.keys(key('j'))
	h.paint()
	before, laid, _ := laidAfterPaint(h, 1)
	if !laid {
		t.Fatal("the heading was not drawn")
	}
	h.onLoop(func() { r.extra = 40 })
	h.paint()
	if got, _, _ := laidAfterPaint(h, 1); got != before {
		t.Fatalf("the block was laid out again with nothing said: %v -> %v", before, got)
	}
	h.onLoop(func() { h.e.Relayout() })
	h.paint()
	if got, laid, stale := laidAfterPaint(h, 1); !laid || stale || got != before+40 {
		t.Fatalf("after Relayout the paint left the block %v high (drawn %v, stale %v); want %v", got, laid, stale, before+40)
	}
	var line int
	var text string
	h.onLoop(func() { line, text = h.e.Core().Cursor().Line, h.e.Core().Value() })
	if line != 1 || text != "first\n## Title here\nlast" {
		t.Errorf("Relayout moved the cursor to line %d or changed the text to %q", line, text)
	}
}

// scrolledIntoATallBlock is an editor whose first block draws extra high from beside the text, the
// view scrolled 200 into it, the cursor on the last block (Lector #397 r0).
func scrolledIntoATallBlock(t *testing.T, extra float32) (*edHarness, *besideRenderer) {
	t.Helper()
	r := &besideRenderer{Renderer: NewMarkdownRenderer(), extra: extra}
	h := startEditor(t, 60, 14, WithRenderer(r), WithMode(Rendered), WithCore(tuiwidget.CoreInitialText("external output\n\nlast")))
	h.keys(key('G'))
	h.paint()
	h.onLoop(func() { h.e.layout.scrollBy(-100000); h.e.layout.scrollBy(200) })
	h.paint()
	if s := scrollOf(h); s != 200 {
		t.Fatalf("the view is scrolled %v into the block, want 200", s)
	}
	return h, r
}

func scrollOf(h *edHarness) float32 {
	var s float32
	h.onLoop(func() { s = h.e.layout.scroll })
	return s
}

// A tall block the view is scrolled into is drawn anew after Relayout: its height kept until it is
// laid out again, the view still finds it at the top, though the cursor is in another block.
func TestRelayoutRedrawsATallBlockScrolledInto(t *testing.T) {
	h, r := scrolledIntoATallBlock(t, 500)
	before, _, _ := laidAfterPaint(h, 0)
	h.onLoop(func() { r.extra = 600; h.e.Relayout() })
	h.paint()
	if got, laid, stale := laidAfterPaint(h, 0); !laid || stale || got != before+100 {
		t.Fatalf("after Relayout, scrolled into it, the block is %v high (drawn anew %v, stale %v); want %v", got, laid && !stale, stale, before+100)
	}
	if s := scrollOf(h); s != 200 {
		t.Errorf("Relayout moved the view to %v", s)
	}
}

// A tall block laid out again shorter than the scroll leaves the view at the text's end, not past it.
func TestRelayoutKeepsTheScrollWithinAShorterText(t *testing.T) {
	h, r := scrolledIntoATallBlock(t, 500)
	h.onLoop(func() { r.extra = 0; h.e.Relayout() })
	h.paint()
	h.paint()
	var s, want float32
	h.onLoop(func() { s, want = h.e.layout.scroll, max(h.e.layout.total()-(h.e.layout.height-2*padY), 0) })
	if s != want {
		t.Fatalf("after the block shrank the view is scrolled %v; want the text's end, %v", s, want)
	}
	if _, laid, stale := laidAfterPaint(h, 2); !laid || stale {
		t.Errorf("the last block is not drawn after the block above it shrank (drawn %v, stale %v)", laid, stale)
	}
}

// A read-only Rendered editor is a viewer: every block renders, the cursor's row
// included, so no row shows its raw source. An editable one typing (Insert) keeps
// the cursor's block raw, and turning read-only re-lays the row the cursor is on.
func TestAReadonlyRenderedEditorRendersTheCursorsRowToo(t *testing.T) {
	h := startEditor(t, 60, 14, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText("first\n## Title here\nlast")))
	h.keys(key('j'), key('i')) // the cursor onto the heading, typing
	h.paint()
	for _, s := range spansOfLine(h, 1) {
		if s.Hidden {
			t.Fatalf("an editable editor in Insert hides marks on the cursor's row: %+v", s)
		}
	}
	h.onLoop(func() { h.e.SetReadOnly(true) })
	h.paint()
	head := spansOfLine(h, 1)
	if len(head) < 2 || !head[0].Hidden || head[0].Text != "## " {
		t.Fatalf("the cursor's row in a viewer is not rendered: %+v", head)
	}
	if head[1].Font.Size <= h.e.layout.th.Prose.Size || !head[1].Font.Bold {
		t.Errorf("the cursor's row in a viewer is not styled as a heading: %+v", head[1])
	}
}

// A cursor moving in a viewer re-lays nothing: every block's layout is the same
// whatever block the cursor is in, so the cache holds; nor does one moving in
// Normal. An editable editor typing re-lays the rows the cursor enters and leaves.
func TestACursorMovingInAViewerReLaysNothing(t *testing.T) {
	viewer := startEditor(t, 60, 14, WithRenderer(&layCounter{Renderer: NewMarkdownRenderer()}), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText("first\n## Title here\nlast")))
	vc := viewer.e.render.(*layCounter)
	viewer.onLoop(func() { viewer.e.SetReadOnly(true) })
	viewer.paint()
	first := vc.calls
	viewer.keys(key('j'))
	viewer.paint()
	if vc.calls != first {
		t.Fatalf("a cursor moving onto a block re-laid it in a viewer: %d -> %d calls", first, vc.calls)
	}
	viewer.keys(key('k'))
	viewer.paint()
	if vc.calls != first {
		t.Fatalf("a cursor moving off a block re-laid it in a viewer: %d -> %d calls", first, vc.calls)
	}

	editor := startEditor(t, 60, 14, WithRenderer(&layCounter{Renderer: NewMarkdownRenderer()}), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText("first\n## Title here\nlast")))
	ec := editor.e.render.(*layCounter)
	editor.paint()
	before := ec.calls
	editor.keys(key('j'))
	editor.paint()
	if ec.calls != before {
		t.Fatalf("a cursor moving in Normal re-laid a block: %d -> %d calls", before, ec.calls)
	}
	editor.keys(key('i'), tui.KeyEvent{Code: tui.KeyDown}) // typing, and moving down
	editor.paint()
	if ec.calls <= before {
		t.Fatalf("an editable editor typing re-laid nothing when the cursor moved onto a block: %d -> %d calls", before, ec.calls)
	}
}

// ReplaceValue keeps the reader's place where SetValue, a load, starts at the top: the wheel's
// scroll and the cursor it left behind stay through a new text (decl's keepPlace).
func TestEditorReplaceValueKeepsTheReadersPlace(t *testing.T) {
	var lines []string
	for i := range 40 {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	h := startEditor(t, 50, 6, WithCore(tuiwidget.CoreInitialText(strings.Join(lines, "\n"))))
	h.paint()
	var scrolled float32
	h.onLoop(func() { h.e.layout.scrollBy(80); scrolled = h.e.layout.scroll })
	if scrolled <= 0 {
		t.Fatal("the editor did not scroll")
	}
	var scroll float32
	var at tuiwidget.TextPos
	h.onLoop(func() {
		h.e.ReplaceValue(strings.ReplaceAll(strings.Join(lines, "\n"), "line", "row"))
		scroll, at = h.e.layout.scroll, h.e.core.Cursor()
	})
	h.paint()
	if scroll != scrolled || at != (tuiwidget.TextPos{}) {
		t.Errorf("after ReplaceValue: scroll %v, cursor %v; want %v and the cursor where it was", scroll, at, scrolled)
	}
	h.onLoop(func() { h.e.SetValue("fresh"); scroll = h.e.layout.scroll })
	if scroll != 0 {
		t.Errorf("after SetValue the scroll is %v, want 0: a load starts at the top", scroll)
	}
}
