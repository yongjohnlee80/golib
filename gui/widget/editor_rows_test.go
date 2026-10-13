package widget

import (
	"image/color"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// In the Rendered view the cursor's block is laid out raw only while typing: Normal, Visual and
// Ctrl-o's command render it; i and R show it raw, and Esc renders it again. The Text keymap is
// always typing; a viewer never is.
func TestTheRenderedCursorRowByMode(t *testing.T) {
	h := startEditor(t, 60, 12, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText("# Heading\n\nbody **bold** text")))
	inside := func() (in bool) {
		h.onLoop(func() { l := h.e.layout; in = l.lay(l.blockOf(l.cursorLine()), nil).inside })
		return
	}
	press := func(ks ...tui.KeyEvent) {
		h.onLoop(func() {
			for _, k := range ks {
				h.e.core.HandleKey(k)
			}
		})
	}
	esc := tui.KeyEvent{Code: tui.KeyEscape}
	steps := []struct {
		what string
		keys []tui.KeyEvent
		raw  bool
	}{
		{"Normal", nil, false},
		{"i", []tui.KeyEvent{key('i')}, true},
		{"Esc", []tui.KeyEvent{esc}, false},
		{"R", []tui.KeyEvent{key('R')}, true},
		{"Esc out of Replace", []tui.KeyEvent{esc}, false},
		{"v", []tui.KeyEvent{key('v')}, false},
		{"Ctrl-o's command", []tui.KeyEvent{esc, key('i'), {Code: 'o', Mods: tui.ModCtrl}}, false},
		{"Insert again after it", []tui.KeyEvent{key('l')}, true},
	}
	for _, s := range steps {
		press(s.keys...)
		if got := inside(); got != s.raw {
			t.Errorf("%s: the cursor's block raw %v, want %v", s.what, got, s.raw)
		}
	}
	press(esc)
	h.onLoop(func() { h.e.core.SetKeyset(tuiwidget.KeysetStandard) })
	if !inside() {
		t.Error("the Text keymap: the cursor's block rendered, want raw (always typing)")
	}
	h.onLoop(func() { h.e.core.SetReadOnly(true) })
	if inside() {
		t.Error("a viewer: the cursor's block raw, want rendered")
	}
}

// The caret on hidden markup sits on the next drawn character of its line, or after the last
// drawn one; motions stay on the source, so l h and i Esc come back to the same drawn place.
func TestTheCaretOnHiddenMarkup(t *testing.T) {
	h := startEditor(t, 60, 12, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText("# Heading\n\nbody **bold** text\n\nend **x**")))
	caretX := func(line, col int) (x float32) {
		h.onLoop(func() {
			r, ok := h.e.layout.caretAt(line, col)
			if !ok {
				t.Fatalf("no caret at (%d,%d)", line, col)
			}
			x = r.X
		})
		return
	}
	for _, c := range []struct {
		what               string
		line, hidden, next int
	}{
		{"a heading's #", 0, 0, 2},
		{"a heading's space", 0, 1, 2},
		{"a bold's leading **", 2, 5, 7},
		{"a bold's closing **", 2, 11, 13},
	} {
		if a, b := caretX(c.line, c.hidden), caretX(c.line, c.next); a != b {
			t.Errorf("%s: the caret at x %v, want the next drawn character's %v", c.what, a, b)
		}
	}
	// a table cell's reserved room is hidden too: the caret on a cell's bar is on its text
	h.onLoop(func() { h.e.core.SetValue("| a | b |\n|---|---|\n| c | d |") })
	if a, b := caretX(0, 4), caretX(0, 6); a != b {
		t.Errorf("a table's bar: the caret at x %v, want the cell's text's %v", a, b)
	}
	h.onLoop(func() { h.e.core.SetValue("# Heading\n\nbody **bold** text\n\nend **x**") })
	if a, b := caretX(4, 8), caretX(4, 6); a <= b {
		t.Errorf("trailing **: the caret at x %v, want after the last drawn character (x > %v)", a, b)
	}
	h.onLoop(func() { h.e.core.SetLine(0, 2) })
	at := func() (x float32) {
		h.onLoop(func() {
			ln, col := h.e.core.Line()
			r, _ := h.e.layout.caretAt(ln, col)
			x = r.X
		})
		return
	}
	start := at()
	h.onLoop(func() { h.e.core.HandleKey(key('l')); h.e.core.HandleKey(key('h')) })
	if got := at(); got != start {
		t.Errorf("l then h: the caret at x %v, want %v", got, start)
	}
	h.onLoop(func() { h.e.core.HandleKey(key('i')); h.e.core.HandleKey(tui.KeyEvent{Code: tui.KeyEscape}) })
	if got := at(); got != start {
		t.Errorf("i then Esc: the caret at x %v, want %v", got, start)
	}
}

// gj and gk over the GUI's display rows: a wrapped raw line, and a rendered paragraph's rows.
func TestGjGkOverPixelRows(t *testing.T) {
	text := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima"
	for _, view := range []EditorMode{Raw, Rendered} {
		h := startEditor(t, 24, 12, WithRenderer(NewMarkdownRenderer()), WithMode(view), WithWrap(tuiwidget.WrapSoft),
			WithCore(tuiwidget.CoreInitialText(text)))
		var rows int
		h.onLoop(func() { rows = h.e.layout.RowCount(0) })
		if rows < 3 {
			t.Fatalf("view %v: the line takes %d rows at this width; the cell needs it wrapped", view, rows)
		}
		rowAt := func() (row, x int) {
			h.onLoop(func() { _, col := h.e.core.Line(); row, x = h.e.layout.RowOf(0, col) })
			return
		}
		h.onLoop(func() { h.e.core.SetLine(0, 1); h.e.core.HandleKey(key('g')); h.e.core.HandleKey(key('j')) })
		if r, _ := rowAt(); r != 1 {
			t.Errorf("view %v: gj to row %d, want 1", view, r)
		}
		h.onLoop(func() { h.e.core.HandleKey(key('g')); h.e.core.HandleKey(key('j')) })
		if r, _ := rowAt(); r != 2 {
			t.Errorf("view %v: gj again to row %d, want 2", view, r)
		}
		h.onLoop(func() { h.e.core.HandleKey(key('g')); h.e.core.HandleKey(key('k')) })
		h.onLoop(func() { h.e.core.HandleKey(key('g')); h.e.core.HandleKey(key('k')) })
		var col int
		h.onLoop(func() { _, col = h.e.core.Line() })
		if r, _ := rowAt(); r != 0 || col != 1 {
			t.Errorf("view %v: gk gk back to row %d col %d, want row 0 col 1", view, r, col)
		}
		// past a row's end is its own last cluster, never the next row's first
		var row int
		h.onLoop(func() { row, _ = h.e.layout.RowOf(0, h.e.layout.ColAt(0, 0, 1<<20)) })
		if row != 0 {
			t.Errorf("view %v: ColAt past row 0's end is on row %d", view, row)
		}
	}
}

// Replace's caret is an underline: a 2-pixel bar along the cell's foot, not Insert's bar.
func TestTheReplaceCaretIsAnUnderline(t *testing.T) {
	r := gui.Rect{X: 10, Y: 20, W: 1, H: 16}
	for _, c := range []struct {
		m    tuiwidget.EditorMode
		w, h float32
	}{{tuiwidget.ModeInsert, 2, 16}, {tuiwidget.ModeReplace, 0, 2}} {
		rc := gui.NewRecordingCanvas(gui.Size{W: 100, H: 100}, gui.Size{W: cellW, H: cellH})
		th := Theme{Text: color.NRGBA{A: 0xff}}
		th.Mono.Size = 16
		paintCaret(rc, r, c.m, th)
		var got gui.Rect
		for _, call := range rc.Calls {
			if call.Op == "FillRect" {
				got = call.Rect
			}
		}
		if got.H != c.h || (c.w > 0 && got.W != c.w) || (c.m == tuiwidget.ModeReplace && got.Y != r.Y+r.H-2) {
			t.Errorf("%v: the caret %+v", c.m, got)
		}
	}
}

// A block is raw or rendered whole: typing on a fence's opening line shows its closing fence's
// marks too; and a Visual selection over hidden markup is drawn on the rendered text.
func TestTheRenderedBlockIsRawWholeAndVisualShows(t *testing.T) {
	h := startEditor(t, 60, 14, WithRenderer(NewMarkdownRenderer()), WithMode(Rendered),
		WithCore(tuiwidget.CoreInitialText("```go\nx := 1\n```\n\nbody **bold** text")))
	h.keys(key('i'))
	h.paint()
	for _, s := range spansOfLine(h, 2) {
		if s.Hidden {
			t.Errorf("typing on the fence's first line: its closing line hides %q", s.Text)
		}
	}
	h.keys(tui.KeyEvent{Code: tui.KeyEscape}, key('G'), key('0'), key('v'), key('$'))
	h.paint()
	var rects int
	h.onLoop(func() {
		lb := h.e.layout.lay(h.e.layout.blockOf(4), nil)
		if lb.inside {
			t.Error("Visual: the block is raw, want rendered")
		}
		for _, ll := range lb.bl.Lines {
			rects += len(selection(h.e.core, ll, false, h.e.layout.width, true))
		}
	})
	if rects == 0 {
		t.Error("Visual over a rendered row with hidden markup draws no selection")
	}
}
