package gui

import (
	"image"
	"testing"
)

// union is the bounding box of rs.
func union(rs []image.Rectangle) image.Rectangle {
	var u image.Rectangle
	for _, r := range rs {
		u = u.Union(r)
	}
	return u
}

// A corner's arms meet exactly: the ink starts at the line, with no stub past the join.
func TestCornerHasNoStub(t *testing.T) {
	cell := image.Rect(0, 0, 9, 18)
	for _, lw := range []int{1, 2, 3} {
		got := union(armRects(lineArms['┌'], cell, lw))
		cx, cy := cell.Dx()/2, cell.Dy()/2
		want := image.Rect(cx-lw/2, cy-lw/2, cell.Max.X, cell.Max.Y)
		if got != want {
			t.Errorf("┌ at lw %d covers %v; want %v", lw, got, want)
		}
		got = union(armRects(lineArms['┘'], cell, lw))
		want = image.Rect(0, 0, cx-lw/2+lw, cy-lw/2+lw)
		if got != want {
			t.Errorf("┘ at lw %d covers %v; want %v", lw, got, want)
		}
	}
}

// Straight lines run edge to edge with nothing missing at the centre.
func TestStraightLinesSpanTheCell(t *testing.T) {
	cell := image.Rect(10, 20, 19, 38)
	h := union(armRects(lineArms['─'], cell, 1))
	if h.Min.X != cell.Min.X || h.Max.X != cell.Max.X || h.Dy() != 1 {
		t.Errorf("─ covers %v in %v", h, cell)
	}
	v := union(armRects(lineArms['│'], cell, 1))
	if v.Min.Y != cell.Min.Y || v.Max.Y != cell.Max.Y || v.Dx() != 1 {
		t.Errorf("│ covers %v in %v", v, cell)
	}
	// The two halves of a straight line meet: their total length is the cell's.
	length := 0
	for _, r := range armRects(lineArms['─'], cell, 1) {
		length += r.Dx()
	}
	if length != cell.Dx() {
		t.Errorf("─'s arms total %dpx in a %dpx cell: a gap or an overlap at the centre", length, cell.Dx())
	}
}

func TestHeavyIsThickerThanLight(t *testing.T) {
	cell := image.Rect(0, 0, 9, 18)
	if l, h := union(armRects(lineArms['─'], cell, 1)), union(armRects(lineArms['━'], cell, 1)); h.Dy() <= l.Dy() {
		t.Errorf("━ is %dpx thick, ─ %dpx", h.Dy(), l.Dy())
	}
}

func TestEveryTableRuneIsDrawable(t *testing.T) {
	for r := range lineArms {
		if !drawable(r) {
			t.Errorf("%q is in the table but not drawable", r)
		}
	}
	for _, r := range "▀▄█▌▐░▒▓▁▏╭╮╯╰" {
		if !drawable(r) {
			t.Errorf("%q is not drawable", r)
		}
	}
	if drawable('a') || drawable('╳') {
		t.Error("a rune outside the tables is drawn from geometry")
	}
}
