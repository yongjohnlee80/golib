package gui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// fillPane fills whatever surface it is given with one letter, so the screen shows each pane's band.
type fillPane struct{ r string }

func (*fillPane) Init(*tui.Context) {}
func (*fillPane) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH})
}
func (p *fillPane) Render(s tui.Surface) {
	sz := s.Size()
	s.Fill(tui.Rect{W: sz.W, H: sz.H}, p.r, style.Style{})
}
func (*fillPane) HandleEvent(tui.Event) bool { return false }

// A Panes under a terminal backend (no window, so no pixels) lays out with the nominal 8x16 cell.
// In a 40x10 terminal the inner area is 320x160 px: the status bar is 40 px at the bottom, so the
// row above it is 120 px; the sidebar is 100 px of it, and the view takes the other 220.
// Cell panes floor, pixel panes ceil: the sidebar is 100/8 = 12 x 120/16 = 7, the view
// ceil(220/8) = 28 x ceil(120/16) = 8, and the status bar 320/8 = 40 x 40/16 = 2, below the row's
// 8 rows. The sidebar's eighth row is nobody's.
func TestPanesUnderATerminalLayOutWithTheNominalCell(t *testing.T) {
	p := NewPanes(Column(
		Sized(Flex(1), Row(
			Sized(Fixed(100), Cells(&fillPane{"S"})),
			Sized(Flex(1), Pixels(&fillPane{"V"})),
		)),
		Sized(Fixed(40), Cells(&fillPane{"B"})),
	))
	be := tui.NewTestBackend(40, 10)
	app := tui.NewApp(p, tui.WithBackend(be))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.Run(ctx)

	row := func(left string, lw int, right string, rw int) string {
		return strings.Repeat(left, lw) + strings.Repeat(right, rw)
	}
	var want []string
	for range 7 {
		want = append(want, row("S", 12, "V", 28))
	}
	want = append(want, row(" ", 12, "V", 28))
	for range 2 {
		want = append(want, row("B", 40, "", 0))
	}
	wantScreen := strings.Join(want, "\n")

	var got string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if got = be.String(); got == wantScreen {
			return
		}
	}
	t.Fatalf("the screen is\n%s\nwant\n%s", got, wantScreen)
}

// Panes draws nothing and consumes no event: its panes do both. Render must not touch its surface
// (a hole is never drawn), so a nil one is safe.
func TestPanesRenderNothingAndConsumeNoEvent(t *testing.T) {
	p := NewPanes(Cells(&fillPane{"x"}))
	if p.HandleEvent(tui.KeyEvent{}) {
		t.Fatal("Panes consumed an event")
	}
	p.Render(nil)
}
