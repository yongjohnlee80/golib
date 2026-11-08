package text

import "testing"

// WrapScrollable decides whether a scrollbar column is showing, which
// WrapUsableWidth then subtracts — so it moves every wrapped line by a cell
// when it is wrong.
//
// It had NO coverage. Mutating both of its branches left the whole widget suite
// green, which is how the gap was found: the derivation that made it shared by
// Editor and TextArea also made an untested defect reach two widgets instead of
// one, so the coverage had to come with the sharing rather than after it.
func TestWrapScrollable(t *testing.T) {
	// One cell per cluster keeps the arithmetic readable; the wrapping logic is
	// what is under test, not the measurement.
	one := func(string) int { return 1 }

	t.Run("WrapNone counts logical lines against the height", func(t *testing.T) {
		lines := []string{"a", "b", "c"}
		if WrapScrollable(lines, WrapView{W: 10, H: 3, Wrap: false, Measure: one}) {
			t.Error("three lines in three rows must not scroll")
		}
		if !WrapScrollable(lines, WrapView{W: 10, H: 2, Wrap: false, Measure: one}) {
			t.Error("three lines in two rows must scroll")
		}
	})

	t.Run("soft wrap counts SCREEN rows, not lines", func(t *testing.T) {
		// One logical line of six cells wraps to three rows at width 2 — the
		// count uses w-1, because a scrollbar is the thing being decided.
		lines := []string{"abcdef"}
		if !WrapScrollable(lines, WrapView{W: 3, H: 2, Wrap: true, Measure: one}) {
			t.Error("one line wrapping to three rows must scroll a two-row viewport")
		}
		if WrapScrollable(lines, WrapView{W: 3, H: 3, Wrap: true, Measure: one}) {
			t.Error("three wrapped rows in a three-row viewport must not scroll")
		}
		// The same content does NOT scroll unwrapped. Asserting only the wrapped
		// arm would pass for a function that ignored the mode entirely.
		if WrapScrollable(lines, WrapView{W: 3, H: 2, Wrap: false, Measure: one}) {
			t.Error("a single logical line must not scroll a two-row viewport unwrapped")
		}
	})

	t.Run("a zero height never scrolls", func(t *testing.T) {
		if WrapScrollable([]string{"a", "b"}, WrapView{W: 10, H: 0, Wrap: true, Measure: one}) {
			t.Error("an unlaid-out widget must not report a scrollbar")
		}
	})
}
