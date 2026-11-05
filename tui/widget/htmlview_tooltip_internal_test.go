package widget

import "testing"

// A capped label in a panned wide diagram is hit where it is shown: the layout's ToolTipAt finds
// its whole text at its shown cells, with the pan offset and the « column taken into account, and
// nothing over the edge columns.
func TestACappedLabelInAPannedDiagramIsHitWhereItIsShown(t *testing.T) {
	long := "a message long enough to pass the message cap of fifty-six cells, then a tail"
	src := "sequenceDiagram\n"
	for _, p := range []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf"} {
		src += "  participant " + p + "\n"
	}
	src += "  Alpha->>Bravo: " + long + "\n  Foxtrot->>Golf: short\n"
	c := layCells(t, navFence(src), navRoom)
	bi, first := diagramBlock(t, c)
	if c.over(bi) == 0 {
		t.Fatalf("precondition: the diagram fits its room (%d)", navRoom)
	}
	g := c.rows[first].grid
	var gx, gy, gw int
	found := false
	for y := 0; y < g.Size.H && !found; y++ {
		for x := 0; x < g.Size.W; x++ {
			if full, r, ok := g.HitAt(x, y); ok && full == long {
				gx, gy, gw, found = r.X, r.Y, r.W, true
				break
			}
		}
	}
	if !found {
		t.Fatal("no region for the capped message")
	}
	navRows(c, 0, len(c.rows)) // painted: where the cells are drawn is where they are hit
	c.Pan(0, len(c.rows), 3)
	navRows(c, 0, len(c.rows))
	x0, left, right := c.gridPlace(&c.rows[first], c.paintW)
	if x0 != left-c.offset(bi) || c.offset(bi) != 3 {
		t.Fatalf("gridPlace x0 %d left %d offset %d", x0, left, c.offset(bi))
	}
	hits := 0
	for gxi := gx; gxi < gx+gw; gxi++ {
		px := x0 + gxi
		text, r := c.ToolTipAt(float32(px), float32(first+gy))
		visible := px >= left && px < right
		if visible != (text == long) {
			t.Errorf("page column %d (grid column %d, room %d..%d): %q", px, gxi, left, right-1, text)
		}
		if text != "" {
			hits++
			if int(r.X) < left || int(r.X+r.W) > right || int(r.Y) != first+gy {
				t.Errorf("the region %+v leaves the room %d..%d or its row %d", r, left, right-1, first+gy)
			}
		}
	}
	if hits == 0 {
		t.Error("the capped message is hit nowhere after the pan")
	}
	if text, _ := c.ToolTipAt(float32(left-1), float32(first+gy)); text != "" {
		t.Errorf("the « column has a region: %q", text)
	}
	if text, _ := c.ToolTipAt(float32(right), float32(first+gy)); text != "" {
		t.Errorf("the » column has a region: %q", text)
	}
}
