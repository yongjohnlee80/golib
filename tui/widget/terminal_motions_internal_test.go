package widget

import (
	"strings"
	"testing"
)

// fakeLines is a cellLines of fixed lines, trailing blanks trimmed as a terminal's lineRunes does,
// and a line out of range has nothing, as Terminal.lineCells gives.
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
func (f fakeLines) runes(i int) []rune {
	if i < 0 || i >= len(f.lines) {
		return nil
	}
	return []rune(strings.TrimRight(f.lines[i], " "))
}

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

// A motion reads the line it is on: e on a four-letter word ends it, though the next line is one
// letter.
func TestTerminalMotionsReadTheirOwnLine(t *testing.T) {
	ls := fakeLines{lines: []string{"abcd", "x"}, cols: 6}
	if l, c := wordEnd(ls, 0, 0); l != 0 || c != 3 {
		t.Errorf("e on abcd from its start: %d,%d, want 0,3", l, c)
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

func TestEveryNormalCommandHasAnAction(t *testing.T) {
	// The normal-mode commands and their actions are one set: a command with no action would be
	// matched and then dropped, and an action with no command could never run.
	ids := map[string]bool{}
	for _, c := range terminalNormalCommands {
		ids[c.ID] = true
		if terminalNormalActions[c.ID] == nil {
			t.Errorf("%s has no action", c.ID)
		}
	}
	for id := range terminalNormalActions {
		if !ids[id] {
			t.Errorf("the action for %s has no command in terminalNormalCommands", id)
		}
	}
}
