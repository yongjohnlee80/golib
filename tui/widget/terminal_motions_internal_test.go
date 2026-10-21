package widget

import (
	"strings"
	"testing"
)

// fakeLines is a cellLines of fixed lines, trailing blanks trimmed as a terminal's lineRunes does.
type fakeLines struct {
	lines []string
	cols  int
	wraps map[int]bool
}

func (f fakeLines) lineCount() int { return len(f.lines) }
func (f fakeLines) width() int     { return f.cols }
func (f fakeLines) wrapped(i int) bool {
	return f.wraps[i]
}
func (f fakeLines) runes(i int) []rune { return []rune(strings.TrimRight(f.lines[i], " ")) }

// The normal-mode motions read only a cellLines: w, b and e walk runs of non-blanks, and a line's
// end ends a word.
func TestTerminalMotionsReadACellLines(t *testing.T) {
	ls := fakeLines{lines: []string{"ab cd", "ef gh"}, cols: 6}
	for _, c := range []struct {
		name       string
		move       func(cellLines, int, int) (int, int)
		from, want [2]int
	}{
		{"w to the next word", wordForward, [2]int{0, 0}, [2]int{0, 3}},
		{"w across the line end", wordForward, [2]int{0, 3}, [2]int{1, 0}},
		{"e to this word's end", wordEnd, [2]int{0, 0}, [2]int{0, 1}},
		{"e to the next word's end", wordEnd, [2]int{0, 1}, [2]int{0, 4}},
		{"b to this word's start", wordBackward, [2]int{0, 4}, [2]int{0, 3}},
		{"b back over the line end", wordBackward, [2]int{1, 0}, [2]int{0, 3}},
	} {
		l, col := c.move(ls, c.from[0], c.from[1])
		if [2]int{l, col} != c.want {
			t.Errorf("%s from %v: %v, want %v", c.name, c.from, [2]int{l, col}, c.want)
		}
	}
}

// A selection joins the lines a wrap broke and ends the others in a newline, trailing blanks
// trimmed.
func TestTerminalSelectionReadsACellLines(t *testing.T) {
	ls := fakeLines{lines: []string{"abcdef", "gh", "ij"}, cols: 6, wraps: map[int]bool{0: true}}
	n := &normalState{visual: visualLine, aLine: 0, line: 2}
	if got := n.selection(ls); got != "abcdefgh\nij" {
		t.Errorf("selection %q, want the wrapped line joined and the next after a newline", got)
	}
}
