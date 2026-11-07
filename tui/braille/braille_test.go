package braille

import (
	"slices"
	"testing"
)

// Each dot of a cell is its own Braille bit, numbered as Unicode numbers them: the left column
// 1-2-3-7 from the top, the right 4-5-6-8. A full cell is ⣿; a dot outside the room is dropped.
func TestEachDotIsItsBrailleBit(t *testing.T) {
	want := [2][4]rune{{'⠁', '⠂', '⠄', '⡀'}, {'⠈', '⠐', '⠠', '⢀'}}
	for x := range 2 {
		for y := range 4 {
			c := New(1, 1)
			c.Set(x, y, 0)
			if got, s := c.At(0, 0); got != want[x][y] || s != 0 {
				t.Errorf("dot (%d,%d) = %q series %d, want %q series 0", x, y, got, s, want[x][y])
			}
		}
	}
	c := New(1, 1)
	for x := range 2 {
		for y := range 4 {
			c.Set(x, y, 0)
		}
	}
	c.Set(-1, 0, 0)
	c.Set(2, 0, 0)
	c.Set(0, 4, 0)
	if got, _ := c.At(0, 0); got != '⣿' {
		t.Fatalf("a full cell is %q, want ⣿", got)
	}
	if got := New(2, 1).Rows(); !slices.Equal(got, []string{"  "}) {
		t.Fatalf("an empty canvas reads %q, want blanks", got)
	}
}

// A line sets every dot between its ends, both included, whatever its direction; a level line
// across two cells is the cells' top dots, a diagonal from one corner to the other its corners
// and every dot between.
func TestALineSetsEveryDotBetweenItsEnds(t *testing.T) {
	c := New(2, 1)
	c.Line(0, 0, 3, 0, 0)
	if got := c.Rows(); !slices.Equal(got, []string{"⠉⠉"}) {
		t.Fatalf("a level line reads %q, want ⠉⠉", got)
	}
	back := New(2, 1)
	back.Line(3, 0, 0, 0, 0)
	if !slices.Equal(back.Rows(), c.Rows()) {
		t.Fatalf("drawn right to left it reads %q, left to right %q", back.Rows(), c.Rows())
	}
	d := New(1, 1)
	d.Line(0, 0, 1, 3, 0)
	set := 0
	for x := range 2 {
		for y := range 4 {
			if d.dots[0]&dotBits[x][y] != 0 {
				set++
			}
		}
	}
	if set != 4 || d.dots[0]&dotBits[0][0] == 0 || d.dots[0]&dotBits[1][3] == 0 {
		t.Fatalf("a steep line sets %d dots (%08b), want 4 with both corners", set, d.dots[0])
	}
}

// An area fills each column from its line down to the base; a cell wears the last series that set
// a dot in it, and a rule over a cell wins over its dots, a crossing of ─ and │ being ┼.
func TestAnAreaFillsDownAndARuleWinsItsCell(t *testing.T) {
	c := New(1, 2)
	c.FillDown(0, 2, 7, 0)
	if got := c.Rows(); !slices.Equal(got, []string{"⡄", "⡇"}) {
		t.Fatalf("a fill from row 2 to 7 reads %q, want ⡄ over ⡇", got)
	}
	c.Set(1, 7, 1)
	if _, s := c.At(0, 1); s != 1 {
		t.Fatalf("the cell wears series %d, want 1 (the last to set a dot)", s)
	}
	c.Rule(0, 1, '─', 2)
	if r, s := c.At(0, 1); r != '─' || s != 2 {
		t.Fatalf("a rule over the cell reads %q series %d, want ─ series 2", r, s)
	}
	c.Rule(0, 1, '│', 2)
	if r, _ := c.At(0, 1); r != '┼' {
		t.Fatalf("─ crossed by │ reads %q, want ┼", r)
	}
	c.Rule(5, 5, '─', 0) // outside: dropped
}
