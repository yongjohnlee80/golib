package gui_test

import (
	"testing"
	"time"

	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// cursorAt is the cursor over cell (x, y) in a frame's areas: the last area over it, else the
// default.
func cursorAt(f []gui.CursorCells, x, y int) pointer.Cursor {
	c := pointer.CursorDefault
	for _, a := range f {
		if x >= a.X && x < a.X+a.W && y >= a.Y && y < a.Y+a.H {
			c = a.Cursor
		}
	}
	return c
}

// A floating window's frame arrows reach the window's cursor areas through the App and the
// Backend, as a window runs them: on gui's capabilities its sides are east-west, its bottom
// north-south and its bottom corners the diagonals, over the frame its content's Box paints, and
// its title row keeps the arrow.
func TestAWindowsFrameArrowsAreTheWindowsCursorAreas(t *testing.T) {
	w := tuiwidget.NewWindowMod(tuiwidget.NewBox(tuiwidget.NewText("win"), tuiwidget.WithBorder(style.BorderNormal)),
		tuiwidget.WithWindowMove(), tuiwidget.WithWindowResize())
	f := tuiwidget.NewFloat(w, tuiwidget.WithAnchor(tuiwidget.AtRect(tui.Rect{X: 5, Y: 2, W: 12, H: 6})))
	host := tuiwidget.NewOverlayHost(tuiwidget.NewText("base"))
	host.Attach(f)
	p := startPipeOn(t, host, 40, 12, tui.Capabilities{NativeViews: true, FrameDrag: true})
	p.update(t, f.Show)
	want := [...]pointer.Cursor{pointer.CursorEastWestResize, pointer.CursorEastWestResize, pointer.CursorNorthSouthResize,
		pointer.CursorNorthEastSouthWestResize, pointer.CursorNorthWestSouthEastResize, pointer.CursorDefault}
	var got [len(want)]pointer.Cursor
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		p.mu.Lock()
		var fr []gui.CursorCells
		if n := len(p.frames); n > 0 {
			fr = p.frames[n-1]
		}
		p.mu.Unlock()
		got = [...]pointer.Cursor{cursorAt(fr, 5, 4), cursorAt(fr, 16, 4), cursorAt(fr, 9, 7), cursorAt(fr, 5, 7), cursorAt(fr, 16, 7), cursorAt(fr, 9, 2)}
		if got == want {
			return
		}
	}
	t.Fatalf("left, right, bottom, bottom-left, bottom-right, title: %v, want %v", got, want)
}
