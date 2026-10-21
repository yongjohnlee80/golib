package tui

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

// faceBox is a box whose subtree is drawn in family (CellFacer); family is set on the loop.
type faceBox struct {
	box
	family string
}

func (b *faceBox) CellFace() string { return b.family }

// faceBackend records the face tables it is handed.
type faceBackend struct {
	*TestBackend
	mu     sync.Mutex
	tables [][]string
}

func (b *faceBackend) SetCellFaces(families []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tables = append(b.tables, families)
}

func (b *faceBackend) table() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.tables) == 0 {
		return nil
	}
	return b.tables[len(b.tables)-1]
}

func startFaced(t *testing.T, root Component, w, h int) (*harness, *faceBackend) {
	t.Helper()
	fb := &faceBackend{TestBackend: NewTestBackend(w, h)}
	app := NewApp(root, WithBackend(fb), WithMinFrameInterval(0))
	return runApp(t, app, fb.TestBackend), fb
}

// faceAt is the face of the screen's cell (x, y), as last flushed.
func faceAt(h *harness, x, y int) uint16 { return h.tb.Snapshot()[y][x].Face }

// A CellFacer's subtree is drawn in its face, its child's cells too; cells outside it, and a later
// sibling painting over part of it, in the window's. The sibling gone, the cells beneath come back
// in the face through the diff. The FaceHost is handed the table.
func TestCellFace_ASubtreeIsDrawnInItsFaceAndALaterPaintInTheWindows(t *testing.T) {
	child := &box{fill: "c"}
	term := &faceBox{box: box{fill: "t", rects: []Rect{{X: 1, Y: 1, W: 3, H: 1}}}, family: "Iosevka"}
	term.Add(child)
	menu := &box{fill: "m"}
	root := &box{rects: []Rect{{X: 0, Y: 0, W: 10, H: 4}, {X: 6, Y: 2, W: 3, H: 1}}}
	root.Add(term, menu)
	h, fb := startFaced(t, root, 14, 5)
	waitFor(t, "the table", func() bool { return slices.Equal(fb.table(), []string{"", "Iosevka"}) })
	h.sync()
	for _, c := range []struct {
		what string
		x, y int
		want uint16
	}{{"the facer's own cell", 0, 0, 1}, {"its child's", 2, 1, 1}, {"outside it", 12, 0, 0}, {"the later sibling over it", 7, 2, 0}} {
		if got := faceAt(h, c.x, c.y); got != c.want {
			t.Errorf("%s (%d,%d): face %d, want %d", c.what, c.x, c.y, got, c.want)
		}
	}
	// the menu goes: its cells repaint in the facer's face, by the diff
	h.onLoop(func() { root.Remove(menu); root.rects = root.rects[:1] })
	waitFor(t, "the cells beneath back in the face", func() bool { return faceAt(h, 7, 2) == 1 })
	if row := h.tb.Snapshot()[2]; row[7].Content != "t" {
		t.Errorf("the cell beneath the menu: %q", row[7].Content)
	}
}

// Faces are interned by family: a family keeps its index, a new one takes the next, and a family
// changed back takes its old index again. A face changed with nothing else repaints the cells.
func TestCellFace_FamiliesKeepTheirIndex(t *testing.T) {
	term := &faceBox{box: box{fill: "t"}, family: "A"}
	root := &box{rects: []Rect{{W: 4, H: 1}}}
	root.Add(term)
	h, fb := startFaced(t, root, 6, 2)
	waitFor(t, "A drawn", func() bool { return faceAt(h, 0, 0) == 1 })
	h.onLoop(func() { term.family = "B"; term.ctx.MarkDirty() })
	waitFor(t, "B drawn, the content the same", func() bool { return faceAt(h, 0, 0) == 2 })
	h.onLoop(func() { term.family = "A"; term.ctx.MarkDirty() })
	waitFor(t, "A again, its own index", func() bool { return faceAt(h, 0, 0) == 1 })
	if got := fb.table(); !slices.Equal(got, []string{"", "A", "B"}) {
		t.Errorf("table %q", got)
	}
	h.onLoop(func() { term.family = ""; term.ctx.MarkDirty() })
	waitFor(t, "no family: the window's", func() bool { return faceAt(h, 0, 0) == 0 })
}

// The table is bounded: a family past it is drawn in the window's face.
func TestCellFace_TheTableIsBounded(t *testing.T) {
	root := &box{}
	for i := range maxCellFaces + 2 {
		root.rects = append(root.rects, Rect{X: i, W: 1, H: 1})
		root.Add(&faceBox{box: box{fill: "f"}, family: fmt.Sprintf("F%d", i)})
	}
	h, fb := startFaced(t, root, maxCellFaces+4, 1)
	waitFor(t, "the table full", func() bool { return len(fb.table()) == maxCellFaces })
	h.sync()
	if got := faceAt(h, maxCellFaces-2, 0); got != maxCellFaces-1 {
		t.Errorf("the last that fits: face %d", got)
	}
	for _, x := range []int{maxCellFaces - 1, maxCellFaces, maxCellFaces + 1} {
		if got := faceAt(h, x, 0); got != 0 {
			t.Errorf("past the table at %d: face %d, want the window's", x, got)
		}
	}
}

// tipFaceBox is a faced control with a tooltip: a terminal whose card shows over its own cells.
type tipFaceBox struct {
	faceBox
	tip string
}

func (b *tipFaceBox) ToolTip() string { return b.tip }

// The tooltip card is painted after the tree in the window's face, over a faced subtree's cells;
// closed, the cells beneath come back in the subtree's face through the diff.
func TestCellFace_TheToolTipCardOverAFacedSubtreeIsTheWindows(t *testing.T) {
	label := &tipFaceBox{faceBox: faceBox{box: box{fill: "l"}, family: "Iosevka"}, tip: "the card"}
	term := &faceBox{box: box{fill: "t"}, family: "Iosevka"}
	root := &box{rects: []Rect{{X: 0, Y: 0, W: 6, H: 1}, {X: 0, Y: 1, W: 20, H: 3}}}
	root.Add(label, term)
	h, _ := startFaced(t, root, 24, 5)
	waitFor(t, "the terminal in its face", func() bool { return faceAt(h, 2, 1) == 1 })
	h.inject(motion(1, 0))
	waitFor(t, "the card", func() bool {
		var id NodeID
		h.onLoop(func() { id = h.app.hover.tip.node })
		return id != 0
	})
	h.sync()
	var card Rect
	h.onLoop(func() { card = h.app.hover.tip.rect })
	if card.Y != 1 || card.W < 3 {
		t.Fatalf("the card is at %+v, want over the terminal's first row", card)
	}
	for x := card.X; x < card.X+card.W; x++ {
		if got := faceAt(h, x, card.Y); got != 0 {
			t.Fatalf("the card's cell (%d,%d) is in face %d, want the window's", x, card.Y, got)
		}
	}
	h.inject(motion(22, 4)) // off the label: the card closes
	waitFor(t, "the terminal's cells back in its face", func() bool { return faceAt(h, card.X+1, card.Y) == 1 })
}
