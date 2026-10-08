package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// fakeLayout is a widget's geometry in units of its own: each cluster measures per, except those
// in wide, and a page is page lines. It records what the core asks of it.
type fakeLayout struct {
	per     int
	wide    map[string]int
	page    int
	changed []int
	reveals [][2]int
}

func (l *fakeLayout) Reveal(line, col int) { l.reveals = append(l.reveals, [2]int{line, col}) }
func (l *fakeLayout) PageLines() int       { return l.page }
func (l *fakeLayout) Changed(from int)     { l.changed = append(l.changed, from) }
func (l *fakeLayout) Measure(s string) int {
	if w, ok := l.wide[s]; ok {
		return w
	}
	return l.per * len([]rune(s))
}

func coreWith(t *testing.T, text string, opts ...widget.CoreOption) (*widget.EditorCore, *fakeLayout) {
	t.Helper()
	l := &fakeLayout{per: 24, page: 5}
	c := widget.NewEditorCore(append([]widget.CoreOption{widget.CoreInitialText(text)}, opts...)...)
	c.Bind(nil, l)
	return c, l
}

func keys(c *widget.EditorCore, s string) {
	for _, r := range s {
		c.HandleKey(tui.KeyEvent{Code: r, Text: string(r)})
	}
}

func TestEditorCoreEditsWithNoWidget(t *testing.T) {
	c, _ := coreWith(t, "")
	keys(c, "ihello")
	c.HandleKey(tui.KeyEvent{Code: tui.KeyEscape})
	if got := c.Value(); got != "hello" {
		t.Fatalf("Value = %q, want hello", got)
	}
	if c.Mode() != widget.ModeNormal {
		t.Fatalf("Mode = %v, want Normal", c.Mode())
	}
	if ln, col := c.Line(); ln != 0 || col != 4 {
		t.Fatalf("cursor = (%d, %d), want (0, 4)", ln, col)
	}
	keys(c, "x")
	c.HandleKey(tui.KeyEvent{Code: 'u', Text: "u"})
	if got := c.Value(); got != "hello" {
		t.Fatalf("after x then u: %q, want hello", got)
	}
}

// j keeps its column in the layout's units, not in clusters or cells: from "ab" at b (24 units in),
// the next line's first cluster is 48 wide, so 24 units falls inside it.
func TestEditorCoreStickyColumnIsInTheLayoutsUnits(t *testing.T) {
	c, l := coreWith(t, "ab\nwxyz\nabcd")
	l.wide = map[string]int{"w": 48}
	keys(c, "l")
	keys(c, "j")
	if ln, col := c.Line(); ln != 1 || col != 0 {
		t.Fatalf("after j: (%d, %d), want (1, 0): 24 units is inside the 48-wide w", ln, col)
	}
	keys(c, "j")
	if ln, col := c.Line(); ln != 2 || col != 1 {
		t.Fatalf("after j j: (%d, %d), want (2, 1): the sticky column is still 24 units", ln, col)
	}
}

func TestEditorCoreTellsTheLayoutWhichLinesChanged(t *testing.T) {
	c, l := coreWith(t, "a\nb\nc")
	keys(c, "jjx")
	if len(l.changed) != 1 || l.changed[0] != 2 {
		t.Fatalf("x on line 2: Changed %v, want [2]", l.changed)
	}
	c2, l2 := coreWith(t, "a\nb\nc")
	keys(c2, "o")
	if len(l2.changed) != 1 || l2.changed[0] != 1 {
		t.Fatalf("o on line 0: Changed %v, want [1]", l2.changed)
	}
}

func TestEditorCorePagesByTheLayoutsPage(t *testing.T) {
	c, l := coreWith(t, strings.Repeat("x\n", 19)+"x")
	c.HandleKey(tui.KeyEvent{Code: tui.KeyPageDown})
	if ln, _ := c.Line(); ln != 5 {
		t.Fatalf("PageDown with a 5-line page: line %d, want 5", ln)
	}
	l.page = 7
	c.HandleKey(tui.KeyEvent{Code: tui.KeyPageDown})
	if ln, _ := c.Line(); ln != 12 {
		t.Fatalf("PageDown with a 7-line page: line %d, want 12", ln)
	}
	if r := l.reveals[len(l.reveals)-1]; r != [2]int{12, 0} {
		t.Fatalf("the last Reveal was %v, want the cursor (12, 0)", r)
	}
}

func TestEditorCorePressAndDragSelect(t *testing.T) {
	c, _ := coreWith(t, "hello\nworld")
	if !c.PressAt(0, 1) || !c.Dragging() {
		t.Fatal("a press did not start a drag")
	}
	c.DragTo(0, 1)
	if c.Mode() != widget.ModeNormal {
		t.Fatal("a drag that has not moved selected")
	}
	c.DragTo(1, 2)
	c.EndDrag()
	if c.Mode() != widget.ModeVisual || c.Dragging() {
		t.Fatalf("after the drag: mode %v, dragging %v", c.Mode(), c.Dragging())
	}
	if got := c.SelectedText(); got != "ello\nwor" {
		t.Fatalf("SelectedText = %q, want %q", got, "ello\nwor")
	}
}

func TestEditorCoreTellsTheCursorsMoveOncePerInput(t *testing.T) {
	moves := 0
	c, _ := coreWith(t, "abc", widget.CoreOnCursorPositionChange(func() { moves++ }))
	keys(c, "l")
	keys(c, "$")
	if moves != 2 {
		t.Fatalf("two motions: %d moves, want 2", moves)
	}
	keys(c, "$")
	if moves != 2 {
		t.Fatalf("a motion that stays: %d moves, want 2", moves)
	}
	c.PressAt(0, 0)
	if moves != 3 {
		t.Fatalf("a press: %d moves, want 3", moves)
	}
}

// The stock rows over a bare core: enabled as the core stands, and their CoreMenuActions act on it.
func TestCoreContextItemsActOnTheCore(t *testing.T) {
	c, _ := coreWith(t, "abc")
	rows := func() map[widget.ItemID]widget.MenuItemModel {
		m := map[widget.ItemID]widget.MenuItemModel{}
		for _, r := range widget.CoreContextItems(c) {
			m[r.ID] = r
		}
		return m
	}
	if r := rows(); r[widget.EditorMenuUndo].Enabled || r[widget.EditorMenuCopy].Enabled {
		t.Fatal("Undo or Copy enabled before an edit or a selection")
	}
	keys(c, "x")
	undo := rows()[widget.EditorMenuUndo]
	if !undo.Enabled {
		t.Fatal("Undo disabled after an edit")
	}
	undo.Action.(widget.CoreMenuAction).Run(c)
	if got := c.Value(); got != "abc" {
		t.Fatalf("after the Undo row: %q, want abc", got)
	}
	keys(c, "vl")
	if !rows()[widget.EditorMenuCopy].Enabled {
		t.Fatal("Copy disabled with a selection")
	}
}
