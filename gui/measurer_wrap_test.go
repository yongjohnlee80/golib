package gui

import (
	"strings"
	"testing"
)

// A measurer wraps where flow's paragraphs do: after a hyphen and after a CJK character, with no
// space there. Before, it broke only at spaces, so an unspaced CJK label or a long hyphenated one
// in a diagram never wrapped: its box widened and the fitted picture shrank.
func TestAMeasurerWrapsAfterHyphensAndCJK(t *testing.T) {
	m, release := AcquireMeasurer(1)
	defer release()
	f := Font{Size: 14}
	width := func(s string) float32 {
		x := m.Measure(s, f).X
		return x[len(x)-1]
	}
	if lines, _ := m.Wrap("alpha-beta-gamma", f, width("alpha-beta-")); !equal(lines, "alpha-beta-", "gamma") {
		t.Errorf("hyphenated: %q, want [alpha-beta- gamma]", lines)
	}
	cjk := "日本語のラベルです"
	limit := width(cjk) / 2
	if limit <= 0 {
		t.Fatalf("%q measures %g wide: the cell observes nothing", cjk, width(cjk))
	}
	lines, size := m.Wrap(cjk, f, limit)
	if len(lines) < 2 || strings.Join(lines, "") != cjk || size.W > limit {
		t.Errorf("CJK at half its width: %q (%g wide), want it broken between characters, each line within %g", lines, size.W, limit)
	}
	if lines, _ := m.Wrap("plain words wrap", f, width("plain words")); !equal(lines, "plain words", "wrap") {
		t.Errorf("spaced: %q, want [plain words wrap]", lines)
	}
}

func equal(got []string, want ...string) bool {
	return strings.Join(got, "\x00") == strings.Join(want, "\x00")
}
