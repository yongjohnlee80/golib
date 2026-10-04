package widget

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldTop is the scroll-top search the editor used before lowestTop: from
// top, step down until the rows from there to ln fit in h. Kept as the
// oracle lowestTop must agree with.
func oldTop(lines []string, top, ln, h int, v wrapView) int {
	for top < ln {
		rows := 0
		for i := top; i <= ln && rows <= h; i++ {
			rows += wrapRowsOfLine(lines, i, v)
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
		v := wrapView{w: 2 + rng.Intn(30), h: 1 + rng.Intn(12), wrap: WrapSoft, measure: tui.StringWidth}
		ln := rng.Intn(n)
		top := rng.Intn(ln + 1)
		if got, want := lowestTop(lines, top, ln, v.h, v), oldTop(lines, top, ln, v.h, v); got != want {
			t.Fatalf("trial %d: lowestTop = %d, the stepwise search %d (top %d, ln %d, %dx%d)", trial, got, want, top, ln, v.w, v.h)
		}
	}
}

// A settled view wraps with the width it settled, without working it out
// again for every line.
func TestASettledViewKeepsItsWidth(t *testing.T) {
	lines := []string{strings.Repeat("word ", 40), "short"}
	v := wrapView{w: 20, h: 1, wrap: WrapSoft, measure: tui.StringWidth}
	s := v.settled(lines)
	if s.usable != wrapUsableWidth(lines, v) {
		t.Fatalf("settled width %d, want %d", s.usable, wrapUsableWidth(lines, v))
	}
	if wrapRowsOfLine(lines, 0, s) != wrapRowsOfLine(lines, 0, v) {
		t.Error("a settled view wraps differently")
	}
	none := wrapView{w: 20, h: 1, wrap: WrapNone, measure: tui.StringWidth}.settled(lines)
	if none.usable != 0 || wrapRowsOfLine(lines, 0, none) != 1 {
		t.Errorf("unwrapped: usable %d, rows %d", none.usable, wrapRowsOfLine(lines, 0, none))
	}
}
