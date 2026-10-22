package widget

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/vt"
)

// fakeLines is a cellLines of fixed lines, trailing blanks trimmed as a terminal's lineRunes does,
// and a line out of range has nothing, as Terminal.lineCells gives.
type fakeLines struct {
	lines []string
	wraps map[int]bool
}

func (f fakeLines) lineCount() int { return len(f.lines) }
func (f fakeLines) wrapped(i int) bool {
	return f.wraps[i]
}
func (f fakeLines) runes(i int) []rune {
	if i < 0 || i >= len(f.lines) {
		return nil
	}
	return []rune(strings.TrimRight(f.lines[i], " "))
}

// wideCells is a terminal line: each rune a cell, a rune in wide taking two (its cell, then a
// continuation of width 0), and trailing blank cells as a screen row has them.
func wideCells(text string, wide string, blanks int) []vt.Cell {
	var cells []vt.Cell
	for _, r := range text {
		switch {
		case r == ' ':
			cells = append(cells, vt.Cell{Width: 1})
		case strings.ContainsRune(wide, r):
			cells = append(cells, vt.Cell{Content: string(r), Width: 2}, vt.Cell{})
		default:
			cells = append(cells, vt.Cell{Content: string(r), Width: 1})
		}
	}
	for range blanks {
		cells = append(cells, vt.Cell{Width: 1})
	}
	return cells
}

// A line's clusters are its cells read as the word motions read text: a wide cluster once, at its
// first cell, a blank a space, the trailing blanks gone; a cell maps to the cluster covering it, past
// the text to the line's end, and the line's end maps back to its last character.
func TestATerminalLineReadsAsClusters(t *testing.T) {
	cells := wideCells("界a b", "界", 3) // 界 on cells 0-1, a on 2, blank 3, b 4, then 3 blanks
	cs, cols := lineClustersOf(cells)
	if strings.Join(cs, "|") != "界|a| |b" || fmt.Sprint(cols) != "[0 2 3 4]" {
		t.Fatalf("clusters %q at %v", cs, cols)
	}
	for _, c := range []struct{ col, idx int }{{0, 0}, {1, 0}, {2, 1}, {4, 3}, {5, 4}, {7, 4}} {
		if got := clusterAtCell(cells, c.col); got != c.idx {
			t.Errorf("cell %d: cluster %d, want %d", c.col, got, c.idx)
		}
	}
	for _, c := range []struct{ idx, col int }{{0, 0}, {1, 2}, {3, 4}, {4, 4}} {
		if got := cellOfCluster(cells, c.idx); got != c.col {
			t.Errorf("cluster %d: cell %d, want %d", c.idx, got, c.col)
		}
	}
	if got := cellOfCluster(wideCells("", "", 4), 0); got != 0 {
		t.Errorf("an empty line's end is cell %d, want 0", got)
	}
}

// A selection joins the lines a wrap broke and ends the others in a newline, trailing blanks
// trimmed.
func TestTerminalSelectionReadsACellLines(t *testing.T) {
	ls := fakeLines{lines: []string{"abcdef", "gh", "ij"}, wraps: map[int]bool{0: true}}
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
