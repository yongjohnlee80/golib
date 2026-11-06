package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

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
	g := c.rows[first].grid.(mermaidPicture).g
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
	x0, left, right := c.gridPlace(&c.rows[first], c.width)
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

// After a fractional scroll (half a page on an odd-height view) the view paints from row
// int(scrollY), so a card's region is counted from there: it sits on its label, not a row above.
func TestACardsRegionFollowsAFractionalScroll(t *testing.T) {
	long := "a message long enough to pass the message cap of fifty-six cells, then a tail"
	src := "sequenceDiagram\n  participant Alpha\n  participant Bravo\n  Alpha->>Bravo: " + long + "\n"
	html := "<p>one</p><p>two</p><p>three</p>" + navFence(src)
	v, c := navView(t, html, 120, 9)
	// the capped message's row and a column of it, in the layout's rows
	row, col := -1, -1
	for ri := range c.rows {
		for x := 0; x < c.width && row < 0; x++ {
			if text, _ := c.ToolTipAt(float32(x), float32(ri)); text == long {
				row, col = ri, x
			}
		}
	}
	if row < 0 {
		t.Fatal("no capped message to hit")
	}
	at := func(scroll float32) (string, tui.Rect) {
		v.scrollY = scroll
		return v.ToolTipAt(tui.Point{X: col, Y: row - int(scroll)})
	}
	if text, r := at(0); text != long || r.Y != row {
		t.Fatalf("unscrolled: %q at row %d, want row %d", text, r.Y, row)
	}
	if text, r := at(1.5); text != long || r.Y != row-1 {
		t.Errorf("scrolled 1.5 rows: %q at view row %d, want %d (painted from row 1)", text, r.Y, row-1)
	}
}
