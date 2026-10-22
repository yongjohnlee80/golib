package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// keyList is a list of rows and the activations its owner heard, driven by keys alone.
func keyList(rows []string, onActivate func(l *List[string], i int)) (l *List[string], acts *[]int) {
	acts = new([]int)
	l = NewList(WithItems(rows, func(v string) string { return v }),
		WithListOnActivate[string](func(i int) {
			*acts = append(*acts, i)
			if onActivate != nil {
				onActivate(l, i)
			}
		}))
	return l, acts
}

func pressKey(c interface{ HandleEvent(tui.Event) bool }, codes ...rune) {
	for _, code := range codes {
		c.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: code})
	}
}

// A list whose rows are emptied acts on nothing, and once given rows again it starts at the first
// of them: its cursor is a row of the rows it has, never one it had.
func TestAListEmptiedThenFilledActsOnTheNewRows(t *testing.T) {
	l, acts := keyList([]string{"a", "b", "c"}, nil)
	pressKey(l, tui.KeyDown, tui.KeyDown, tui.KeyEnter)
	if len(*acts) != 1 || (*acts)[0] != 2 {
		t.Fatalf("the control: Down, Down, Enter activated %v, want [2]", *acts)
	}
	l.SetItems(nil)
	pressKey(l, tui.KeyEnter, tui.KeyDown, tui.KeyEnter)
	if len(*acts) != 1 {
		t.Fatalf("an empty list activated %v", (*acts)[1:])
	}
	l.SetItems([]string{"x", "y"})
	pressKey(l, tui.KeyEnter)
	pressKey(l, tui.KeyDown, tui.KeyEnter)
	if got := (*acts)[1:]; len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("refilled with x and y: Enter, then Down and Enter activated %v, want [0 1]", got)
	}
}

// An owner whose activation empties the rows leaves the list with nothing to act on: the keys after
// it activate nothing and the list stays empty.
func TestAListActivationThatEmptiesTheRowsLeavesNothingToAct(t *testing.T) {
	l, acts := keyList([]string{"a", "b"}, func(l *List[string], _ int) { l.SetItems(nil) })
	pressKey(l, tui.KeyDown, tui.KeyEnter)
	if len(*acts) != 1 || (*acts)[0] != 1 {
		t.Fatalf("Down, Enter activated %v, want [1]", *acts)
	}
	pressKey(l, tui.KeyEnter, tui.KeyUp, tui.KeyEnter)
	l.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Y: 0, Count: 1})
	l.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, Y: 0, Count: 2})
	if len(*acts) != 1 || l.count != 0 {
		t.Errorf("after the rows were emptied: activations %v, %d rows", *acts, l.count)
	}
}
