package widget

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// editor_highlight_internal_test.go holds the Editor's highlighter hook to
// ADR-tui-0015's S1/S2: styles paint in order, and lines are highlighted only
// when, and only as far as, they need to be.

// wordHighlighter styles the word "kw" as Keyword, and "/*" … "*/" as a
// comment that carries across lines. It counts its calls.
type wordHighlighter struct{ calls atomic.Int32 }

func (h *wordHighlighter) HighlightBlock(line string, prev highlight.State) ([]highlight.Span, highlight.State) {
	h.calls.Add(1)
	var spans []highlight.Span
	i := 0
	if prev == 1 {
		end := strings.Index(line, "*/")
		if end < 0 {
			return []highlight.Span{{Start: 0, End: len(line), Style: highlight.Comment}}, 1
		}
		spans = append(spans, highlight.Span{Start: 0, End: end + 2, Style: highlight.Comment})
		i = end + 2
	}
	for i < len(line) {
		switch {
		case strings.HasPrefix(line[i:], "/*"):
			end := strings.Index(line[i+2:], "*/")
			if end < 0 {
				return append(spans, highlight.Span{Start: i, End: len(line), Style: highlight.Comment}), 1
			}
			spans = append(spans, highlight.Span{Start: i, End: i + 2 + end + 2, Style: highlight.Comment})
			i += 2 + end + 2
		case strings.HasPrefix(line[i:], "kw"):
			spans = append(spans, highlight.Span{Start: i, End: i + 2, Style: highlight.Keyword})
			i += 2
		default:
			i++
		}
	}
	return spans, 0
}

var (
	kwRed   = style.New().Foreground(style.ANSI(1))
	cmGreen = style.New().Foreground(style.ANSI(2))
)

func syntaxRedGreen() SyntaxStyles {
	var st SyntaxStyles
	st[highlight.Keyword] = kwRed
	st[highlight.Comment] = cmGreen
	return st
}

func TestTheByteSpansMapOntoClusters(t *testing.T) {
	e := NewEditor(WithHighlighter(highlight.HighlighterFunc(func(line string, _ highlight.State) ([]highlight.Span, highlight.State) {
		i := strings.Index(line, "kw")
		return []highlight.Span{{Start: i, End: i + 2, Style: highlight.Keyword}}, 0
	})))
	e.SetValue("中é kw")
	got := e.highlightLine(0, 0).styles
	want := []highlight.Style{highlight.Normal, highlight.Normal, highlight.Normal, highlight.Keyword, highlight.Keyword}
	if len(got) != len(want) {
		t.Fatalf("styles %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("styles %v, want %v", got, want)
		}
	}
}

// renderEditor runs e on a test backend of w×h.
func renderEditor(t *testing.T, e *Editor, w, h int) *tui.TestBackend {
	t.Helper()
	ih := startAppInternal(t, e, w, h)
	t.Cleanup(ih.stopInternal)
	ih.onLoopInternal(func() {})
	return ih.tb
}

func TestASpanPaintsOverTheTextAndUnderTheSelection(t *testing.T) {
	hl := &wordHighlighter{}
	e := NewEditor(WithHighlighter(hl), WithSyntaxStyles(syntaxRedGreen()),
		WithEditorStyles(TextInputStyles{Text: style.New().Background(style.ANSI(4))}))
	e.SetValue("a kw /* c */ b")
	tb := renderEditor(t, e, 30, 3)
	waitCells(t, tb, func(g [][]tui.Cell) bool {
		return g[0][2].Attrs.FG == ansiCell(1) && g[0][5].Attrs.FG == ansiCell(2)
	})
	g := tb.Snapshot()
	if g[0][2].Attrs.BG != ansiCell(4) {
		t.Errorf("the keyword lost the text's background: %+v", g[0][2].Attrs)
	}
	if g[0][0].Attrs.FG != (tui.CellColor{}) {
		t.Errorf("plain text took a syntax colour: %+v", g[0][0].Attrs)
	}
}

// TestTheSelectionWinsOverASpan: select the keyword in Visual mode — it is
// reversed, over its syntax colour.
func TestTheSelectionWinsOverASpan(t *testing.T) {
	e := NewEditor(WithHighlighter(&wordHighlighter{}), WithSyntaxStyles(syntaxRedGreen()))
	e.SetValue("a kw b")
	ih := startAppInternal(t, e, 20, 2)
	t.Cleanup(ih.stopInternal)
	ih.onLoopInternal(func() { e.Context().RequestFocus() })
	key := func(ch rune) tui.KeyEvent {
		return tui.KeyEvent{Kind: tui.KeyPress, Code: ch, Base: ch, Text: string(ch)}
	}
	if err := ih.tb.Inject(key('0'), key('w'), key('v')); err != nil {
		t.Fatal(err)
	}
	waitCells(t, ih.tb, func(g [][]tui.Cell) bool {
		c := g[0][2].Attrs
		return c.Mask&tui.AttrReverse != 0 && c.FG == ansiCell(1)
	})
}

func TestOnlyWhatIsSeenOrChangedIsHighlighted(t *testing.T) {
	hl := &wordHighlighter{}
	e := NewEditor(WithHighlighter(hl), WithSyntaxStyles(syntaxRedGreen()))
	var lines []string
	for range 100 {
		lines = append(lines, "kw x")
	}
	e.SetValue(strings.Join(lines, "\n"))
	ih := startAppInternal(t, e, 20, 5)
	t.Cleanup(ih.stopInternal)
	// change runs fn on the loop and counts the highlighting the frames after
	// it cost — counted around the change itself, since the frame that paints
	// it runs as soon as it has.
	change := func(fn func()) int32 {
		before := hl.calls.Load()
		ih.onLoopInternal(fn)
		ih.onLoopInternal(func() {})
		ih.onLoopInternal(func() {})
		return hl.calls.Load() - before
	}
	ih.onLoopInternal(func() {})
	if first := hl.calls.Load(); first == 0 || first > 5 {
		t.Fatalf("the first frame highlighted %d lines of 100, want at most the 5 on screen", first)
	}
	if n := change(func() { e.MarkDirty() }); n != 0 {
		t.Errorf("an unchanged frame highlighted %d lines", n)
	}
	edit := func(i int, text string) func() {
		return func() {
			lines[i] = text
			e.SetValue(strings.Join(lines, "\n"))
		}
	}
	// Opening a comment changes every line after it — but only the ones on
	// screen are highlighted.
	if n := change(edit(1, "kw /*")); n == 0 || n > 5 {
		t.Errorf("opening a comment re-highlighted %d lines, want the visible ones only", n)
	}
	change(edit(1, "kw y"))
	// An edit that changes nothing after it converges at once: one line.
	if n := change(edit(2, "kw z")); n != 1 {
		t.Errorf("an edit to one line re-highlighted %d lines, want 1", n)
	}
}

func TestACommentCarriesAcrossLinesOnScreen(t *testing.T) {
	e := NewEditor(WithHighlighter(&wordHighlighter{}), WithSyntaxStyles(syntaxRedGreen()))
	e.SetValue("x /* open\ninside\nend */ kw")
	tb := renderEditor(t, e, 20, 4)
	waitCells(t, tb, func(g [][]tui.Cell) bool {
		return g[1][0].Attrs.FG == ansiCell(2) && g[2][7].Attrs.FG == ansiCell(1)
	})
}

func TestSetHighlighterNilTurnsItOff(t *testing.T) {
	e := NewEditor(WithHighlighter(&wordHighlighter{}), WithSyntaxStyles(syntaxRedGreen()))
	e.SetValue("kw")
	ih := startAppInternal(t, e, 10, 2)
	t.Cleanup(ih.stopInternal)
	waitCells(t, ih.tb, func(g [][]tui.Cell) bool { return g[0][0].Attrs.FG == ansiCell(1) })
	ih.onLoopInternal(func() { e.SetHighlighter(nil) })
	waitCells(t, ih.tb, func(g [][]tui.Cell) bool { return g[0][0].Attrs.FG == (tui.CellColor{}) })
	ih.onLoopInternal(func() { e.WithSyntaxStyles(SyntaxStyles{}).SetHighlighter(&wordHighlighter{}) })
	waitCells(t, ih.tb, func(g [][]tui.Cell) bool { return g[0][0].Attrs.FG == (tui.CellColor{}) })
}

func ansiCell(n uint8) tui.CellColor { return tui.CellColor{Kind: tui.CellColorANSI, Index: n} }

// waitCells polls the screen until cond holds.
func waitCells(t *testing.T, tb *tui.TestBackend, cond func([][]tui.Cell) bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !cond(tb.Snapshot()); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the screen never matched:\n%s", tb.String())
		}
	}
}

// TestADeepJumpHighlightsInBoundedFrames: `G` on a long file needs every line
// above the screen, for the state it carries down — here, a comment opened on
// the first line and never closed. A frame does at most hlFrameBudget of them
// and asks for another, so no one frame freezes on the whole file; and the
// screen still ends in the right colours, carried from line 0.
func TestADeepJumpHighlightsInBoundedFrames(t *testing.T) {
	const n, rows = 20001, 5
	hl := &wordHighlighter{}
	e := NewEditor(WithHighlighter(hl), WithSyntaxStyles(syntaxRedGreen()))
	lines := make([]string, n)
	lines[0] = "/* never closed"
	for i := 1; i < n; i++ {
		lines[i] = "inside"
	}
	lines[n-1] = "last"
	e.SetValue(strings.Join(lines, "\n"))
	ih := startAppInternal(t, e, 20, rows)
	t.Cleanup(ih.stopInternal)
	ih.syncInternal()

	// One frame's highlighting, measured as Render does it, right after the
	// jump.
	var work int32
	ih.onLoopInternal(func() {
		e.goToLine(false, 1, true) // G
		e.ensureVisible()
		before := hl.calls.Load()
		f := e.beginHighlightFrame()
		for ln := e.top; ln < min(e.top+rows, len(e.lines)); ln++ {
			e.highlighted(ln, f)
		}
		work = hl.calls.Load() - before
	})
	// Bounded by the budget, and — whatever the budget is set to — by a
	// fraction of the file: the whole file in one frame is the defect.
	if work > hlFrameBudget+rows || work > n/4 {
		t.Fatalf("one frame highlighted %d lines of %d after the jump, want at most %d",
			work, n, min(hlFrameBudget+rows, n/4))
	}
	// The frames that follow finish the catch-up on their own, and the last
	// line wears the comment colour carried from the first.
	waitCells(t, ih.tb, func(g [][]tui.Cell) bool {
		for _, row := range g {
			if row[0].Content == "l" && row[1].Content == "a" && row[0].Attrs.FG == ansiCell(2) {
				return true
			}
		}
		return false
	})
}
