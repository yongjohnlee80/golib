package vegalite

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
			c := newCanvas(1, 1)
			c.set(x, y, 0)
			if got, s := c.at(0, 0); got != want[x][y] || s != 0 {
				t.Errorf("dot (%d,%d) = %q series %d, want %q series 0", x, y, got, s, want[x][y])
			}
		}
	}
	c := newCanvas(1, 1)
	for x := range 2 {
		for y := range 4 {
			c.set(x, y, 0)
		}
	}
	c.set(-1, 0, 0)
	c.set(2, 0, 0)
	c.set(0, 4, 0)
	if got, _ := c.at(0, 0); got != '⣿' {
		t.Fatalf("a full cell is %q, want ⣿", got)
	}
	if got := newCanvas(2, 1).rows(); !slices.Equal(got, []string{"  "}) {
		t.Fatalf("an empty canvas reads %q, want blanks", got)
	}
}

// A line sets every dot between its ends, both included, whatever its direction; a level line
// across two cells is the cells' top dots, a diagonal from one corner to the other its corners
// and every dot between.
func TestALineSetsEveryDotBetweenItsEnds(t *testing.T) {
	c := newCanvas(2, 1)
	c.line(0, 0, 3, 0, 0)
	if got := c.rows(); !slices.Equal(got, []string{"⠉⠉"}) {
		t.Fatalf("a level line reads %q, want ⠉⠉", got)
	}
	back := newCanvas(2, 1)
	back.line(3, 0, 0, 0, 0)
	if !slices.Equal(back.rows(), c.rows()) {
		t.Fatalf("drawn right to left it reads %q, left to right %q", back.rows(), c.rows())
	}
	d := newCanvas(1, 1)
	d.line(0, 0, 1, 3, 0)
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
	c := newCanvas(1, 2)
	c.fillDown(0, 2, 7, 0)
	if got := c.rows(); !slices.Equal(got, []string{"⡄", "⡇"}) {
		t.Fatalf("a fill from row 2 to 7 reads %q, want ⡄ over ⡇", got)
	}
	c.set(1, 7, 1)
	if _, s := c.at(0, 1); s != 1 {
		t.Fatalf("the cell wears series %d, want 1 (the last to set a dot)", s)
	}
	c.rule(0, 1, '─', 2)
	if r, s := c.at(0, 1); r != '─' || s != 2 {
		t.Fatalf("a rule over the cell reads %q series %d, want ─ series 2", r, s)
	}
	c.rule(0, 1, '│', 2)
	if r, _ := c.at(0, 1); r != '┼' {
		t.Fatalf("─ crossed by │ reads %q, want ┼", r)
	}
	c.rule(5, 5, '─', 0) // outside: dropped
}
