package core

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	txt "github.com/yongjohnlee80/golib/tui/widget/internal/text"
)

// oldTop is the scroll-top search the editor used before txt.LowestTop: from
// top, step down until the rows from there to ln fit in h. Kept as the
// oracle txt.LowestTop must agree with.
func oldTop(lines []string, top, ln, h int, v txt.WrapView) int {
	for top < ln {
		rows := 0
		for i := top; i <= ln && rows <= h; i++ {
			rows += txt.WrapRowsOfLine(lines, i, v)
		}
		if rows <= h {
			break
		}
		top++
	}
	return top
}

func TestLowestTopAgreesWithTheStepwiseSearch(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	words := []string{"a", "word", "longer", "wrapping", "x", "世界", "é"}
	for trial := 0; trial < 3000; trial++ {
		n := 1 + rng.Intn(40)
		lines := make([]string, n)
		for i := range lines {
			var b strings.Builder
			for k := rng.Intn(30); k > 0; k-- {
				b.WriteString(words[rng.Intn(len(words))])
				b.WriteByte(' ')
			}
			lines[i] = b.String()
		}
		v := txt.WrapView{W: 2 + rng.Intn(30), H: 1 + rng.Intn(12), Wrap: true, Measure: tui.StringWidth}
		ln := rng.Intn(n)
		top := rng.Intn(ln + 1)
		if got, want := txt.LowestTop(lines, top, ln, v.H, v), oldTop(lines, top, ln, v.H, v); got != want {
			t.Fatalf("trial %d: lowestTop = %d, the stepwise search %d (top %d, ln %d, %dx%d)", trial, got, want, top, ln, v.W, v.H)
		}
	}
}

// A settled view wraps with the width it settled, without working it out
// again for every line.
func TestASettledViewKeepsItsWidth(t *testing.T) {
	lines := []string{strings.Repeat("word ", 40), "short"}
	v := txt.WrapView{W: 20, H: 1, Wrap: true, Measure: tui.StringWidth}
	s := v.Settled(lines)
	if s.Usable != txt.WrapUsableWidth(lines, v) {
		t.Fatalf("settled width %d, want %d", s.Usable, txt.WrapUsableWidth(lines, v))
	}
	if txt.WrapRowsOfLine(lines, 0, s) != txt.WrapRowsOfLine(lines, 0, v) {
		t.Error("a settled view wraps differently")
	}
	none := txt.WrapView{W: 20, H: 1, Wrap: false, Measure: tui.StringWidth}.Settled(lines)
	if none.Usable != 0 || txt.WrapRowsOfLine(lines, 0, none) != 1 {
		t.Errorf("unwrapped: usable %d, rows %d", none.Usable, txt.WrapRowsOfLine(lines, 0, none))
	}
}

// A soft-wrapped TextArea puts its cursor below the rows of every line above
// it, and hides it once it is below the viewport.
func TestATextAreaCursorCountsWrappedRowsAbove(t *testing.T) {
	ta := NewTextArea(WithWrap(WrapSoft))
	ta.SetValue("one two three four five six\nseven\neight nine ten eleven twelve")
	ta.Layout(tui.Constraints{MaxW: 10, MaxH: 6})
	ta.buf.Ln, ta.buf.Col, ta.top = 1, 2, 0
	v := ta.view().Settled(ta.buf.Lines)
	_, y, ok := ta.Cursor()
	if want := txt.WrapRowsOfLine(ta.buf.Lines, 0, v); !ok || y != want {
		t.Fatalf("cursor on line 1 at y=%d ok=%v, want y=%d (the first line's rows)", y, ok, want)
	}
	// The third line's last character sits below the first two lines' rows
	// plus its own wrapped rows: past the 6-row viewport.
	ta.buf.Ln, ta.buf.Col = 2, len(txt.Clusters(ta.buf.Lines[2]))
	if _, y, ok := ta.Cursor(); ok {
		t.Errorf("a cursor below a 6-row viewport is shown at y=%d", y)
	}
}
