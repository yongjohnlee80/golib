package widget_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// fortyLines is a text of 40 numbered lines.
func fortyLines() string {
	var b strings.Builder
	for i := range 40 {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// scrollEditor is a focused Editor over fortyLines at 40×10, telling each top line it is told into
// a log read on the loop.
func scrollEditor(t *testing.T) (*harness, *shell, *editor.Editor, func() []int) {
	t.Helper()
	var told []int
	ed := editor.NewEditor(editor.WithCore(editor.CoreInitialText(fortyLines())),
		editor.WithEditorOnScroll(func(top int) { told = append(told, top) }))
	sh := newShell(ed)
	h := startApp(t, sh, 40, 10)
	h.onLoop(func() { h.app.FocusInto(ed) })
	h.barrier(sh)
	log := func() (out []int) {
		h.onLoop(func() { out = append([]int(nil), told...) })
		return out
	}
	return h, sh, ed, log
}

// The editor tells its host the first line shown once at each change, whatever moved it: a key's
// motion and the reveal it makes, the wheel, SetTopLine; a move that leaves the top where it was
// tells nothing.
func TestTheEditorTellsItsTopLineOnceAtEachChange(t *testing.T) {
	h, sh, ed, log := scrollEditor(t)
	top := func() (n int) {
		h.onLoop(func() { n = ed.TopLine() })
		return n
	}
	step := func(what string, act func(), moves bool) {
		t.Helper()
		before := log()
		was := top()
		act()
		h.barrier(sh)
		got, now := log(), top()
		switch {
		case moves && (len(got) != len(before)+1 || got[len(got)-1] != now || now == was):
			t.Fatalf("%s: told %v after %v, top %d (was %d); want one more, the new top", what, got, before, now, was)
		case !moves && len(got) != len(before):
			t.Fatalf("%s: told %v after %v, top %d; want nothing told", what, got, before, now)
		}
	}
	step("G, the cursor to the last line", func() { h.inject(key('G')) }, true)
	step("gg, back to the first", func() { h.inject(key('g'), key('g')) }, true)
	step("j, within the view", func() { h.inject(key('j')) }, false)
	step("the wheel down", func() {
		h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 5, Y: 5})
	}, true)
	step("SetTopLine(12)", func() { h.onLoop(func() { ed.SetTopLine(12) }) }, true)
	step("SetTopLine to the top it has", func() { h.onLoop(func() { ed.SetTopLine(12) }) }, false)
	if got := top(); got != 12 {
		t.Fatalf("top %d after SetTopLine(12)", got)
	}
	step("ReplaceValue, the text the same length: the place kept", func() { h.onLoop(func() { ed.ReplaceValue(fortyLines()) }) }, false)
	step("ReplaceValue with five lines: the top clamped to the last", func() {
		h.onLoop(func() { ed.ReplaceValue("a\nb\nc\nd\ne") })
	}, true)
	if got := top(); got != 4 {
		t.Fatalf("top %d after ReplaceValue with five lines, want 4", got)
	}
	step("SetValue, a new text from its top", func() { h.onLoop(func() { ed.SetValue(fortyLines()) }) }, true)
	if got := top(); got != 0 {
		t.Fatalf("top %d after SetValue, want 0", got)
	}
}

// SetTopLine clamps: never above the first line, and once laid out never so low that the last
// line is above the view's bottom; before a layout, no lower than the last line.
func TestSetTopLineClamps(t *testing.T) {
	h, _, ed, _ := scrollEditor(t)
	var got [2]int
	h.onLoop(func() {
		ed.SetTopLine(-3)
		got[0] = ed.TopLine()
		ed.SetTopLine(1000)
		got[1] = ed.TopLine()
	})
	if got != [2]int{0, 30} {
		t.Fatalf("SetTopLine(-3), SetTopLine(1000): tops %v, want [0 30] (40 lines, 10 rows)", got)
	}
	unlaid := editor.NewEditor(editor.WithCore(editor.CoreInitialText(fortyLines())))
	unlaid.SetTopLine(1000)
	if unlaid.TopLine() != 39 {
		t.Fatalf("before a layout, SetTopLine(1000): top %d, want 39", unlaid.TopLine())
	}
}

// lineTints are a line style's background in a sepia page and a dark one.
var lineTints = map[string]style.Color{
	"sepia": style.RGB(0xd2, 0xdc, 0xb4),
	"dark":  style.RGB(0x24, 0x3a, 0x2a),
}

// A line's own style covers its whole row in sepia and dark, its text and the cells past it, under
// a highlight overlay (its colour over the tint) and under the selection (the selection over it);
// the other lines keep the page.
func TestALineStyleCoversItsRowUnderTheOverlayAndTheSelection(t *testing.T) {
	selection := style.RGB(0x80, 0x80, 0xff)
	keyword := style.RGB(0xc0, 0x20, 0x20)
	for name, th := range switchThemes {
		t.Run(name, func(t *testing.T) {
			var syntax editor.SyntaxStyles
			syntax[highlight.Keyword] = style.New().Foreground(keyword)
			tint := lineTints[name]
			ed := editor.NewEditor(
				editor.WithCore(editor.CoreInitialText("plain\nkw marked\nplain"),
					editor.CoreHighlighter(highlight.HighlighterFunc(func(_ string, previous highlight.State) ([]highlight.Span, highlight.State) {
						return nil, previous
					})),
					editor.CoreSyntaxStyles(syntax)),
				editor.WithEditorStyles(core.TextInputStyles{Text: switchPages[name], Selection: style.New().Background(selection)}),
				editor.WithEditorLineStyle(func(line int) (style.Style, bool) {
					return style.New().Background(tint), line == 1
				}))
			ed.Core().SetHighlightOverlay(func(line string, sem []highlight.Span) []highlight.Span {
				if strings.HasPrefix(line, "kw") {
					return append(sem, highlight.Span{Start: 0, End: 2, Style: highlight.Keyword})
				}
				return sem
			})
			sh := newShell(ed)
			h := startAppOpts(t, sh, 40, 6, tui.WithTheme(&th))
			h.onLoop(func() { h.app.FocusInto(ed) })
			h.barrier(sh)
			rgb := func(c style.Color) tui.CellColor {
				r, g, b, _ := c.RGBValues()
				return tui.CellColor{Kind: tui.CellColorRGB, R: r, G: g, B: b}
			}
			page, _ := switchPages[name].GetBackground()
			for _, c := range []struct {
				what string
				x, y int
				bg   style.Color
				fg   *style.Color
			}{
				{"the styled line's highlighted text", 0, 1, tint, &keyword},
				{"the styled line's plain text", 4, 1, tint, nil},
				{"past the styled line's text", 20, 1, tint, nil},
				{"another line's text", 0, 0, page, nil},
				{"past another line's text", 20, 2, page, nil},
			} {
				a := cellAttrs(h, c.x, c.y)
				if a.BG != rgb(c.bg) || (c.fg != nil && a.FG != rgb(*c.fg)) {
					t.Errorf("%s (%d,%d): %s", c.what, c.x, c.y, cellLook(tui.Cell{Attrs: a}))
				}
			}
			// The selection over it: "j" to the styled line, "vl" selecting two of its cells.
			h.inject(key('j'), key('v'), key('l'))
			h.barrier(sh)
			if a := cellAttrs(h, 1, 1); a.BG != rgb(selection) {
				t.Errorf("a selected cell on the styled line: %s, want the selection's background", cellLook(tui.Cell{Attrs: a}))
			}
			if a := cellAttrs(h, 5, 1); a.BG != rgb(tint) {
				t.Errorf("an unselected cell on the styled line: %s, want the tint", cellLook(tui.Cell{Attrs: a}))
			}
		})
	}
}

// A frame asks a line's look once: the row's fill and each of its cells share the answer, so no
// frame asks for the same line twice running. The Rendered view, which draws its own looks, never
// asks.
func TestALineStyleIsAskedOncePerLineAFrame(t *testing.T) {
	var asked []int
	ed := editor.NewEditor(editor.WithCore(editor.CoreInitialText(fortyLines())),
		editor.WithEditorLineStyle(func(line int) (style.Style, bool) {
			asked = append(asked, line)
			return style.New().Background(style.RGB(0x20, 0x40, 0x20)), line%2 == 0
		}))
	sh := newShell(ed)
	h := startApp(t, sh, 40, 10)
	h.barrier(sh)
	var got []int
	h.onLoop(func() { got = append([]int(nil), asked...) })
	if len(got) == 0 {
		t.Fatal("the Raw view never asked a line's look")
	}
	for i := 1; i < len(got); i++ {
		if got[i] == got[i-1] {
			t.Fatalf("line %d asked twice running (ask %d of %v): a frame asks each line once", got[i], i, got)
		}
	}
	h.onLoop(func() {
		ed.SetRenderer(&editor.MarkdownCellRenderer{})
		ed.SetView(editor.ViewRendered)
	})
	h.barrier(sh)
	// A text set after the asks are cleared, and waited for as the Rendered view draws it (the
	// heading without its "#"): a frame of the Rendered view was painted, and asked nothing.
	h.onLoop(func() { asked = nil; ed.SetValue("# Rendered heading\n\nbody") })
	h.waitFor("the Rendered view's heading", func() bool {
		screen := h.tb.String()
		return strings.Contains(screen, "Rendered heading") && !strings.Contains(screen, "# Rendered heading")
	})
	h.onLoop(func() { got = append([]int(nil), asked...) })
	if len(got) != 0 {
		t.Fatalf("the Rendered view asked the looks of lines %v", got)
	}
}
