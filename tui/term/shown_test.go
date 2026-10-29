package term

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// TestTheEmitterWritesNoControlFromACell: a cell's content reaches the terminal drawable, whatever
// made the cell. ESC]52 (set the clipboard) and its BEL in cells come out as ␛ and ␇, and the only
// escape sequences in the frame are the emitter's own.
func TestTheEmitterWritesNoControlFromACell(t *testing.T) {
	e := newEmitter(false)
	var diff []tui.CellUpdate
	for i, r := range "\x1b]52;c;cGF5bG9hZA==\x07" {
		diff = append(diff, tui.CellUpdate{X: i, Y: 0, Cell: tui.Cell{Content: string(r), Width: 1}})
	}
	diff = append(diff, tui.CellUpdate{X: 30, Y: 0, Cell: tui.Cell{Content: "\u009b2J", Width: 1}})
	got := string(e.frame(tui.Capabilities{}, diff, tui.CursorState{}, true, nil))
	if strings.Contains(got, "\x1b]") || strings.Contains(got, "\x07") || strings.Contains(got, "\u009b") {
		t.Fatalf("the frame obeys a cell: %q", got)
	}
	if !strings.Contains(got, "\u241b]52;c;") || !strings.Contains(got, "\u2407") {
		t.Errorf("the frame does not draw the cells: %q", got)
	}
}
