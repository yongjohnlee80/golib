package widget

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// pointerProbe is a widget that takes every press, recording where (in its own cells).
type pointerProbe struct {
	mu      sync.Mutex
	presses []tui.Point
}

func (*pointerProbe) Init(*tui.Context) {}
func (*pointerProbe) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH})
}
func (*pointerProbe) Render(tui.Surface) {}
func (p *pointerProbe) HandleEvent(ev tui.Event) bool {
	if m, ok := ev.(tui.MouseEvent); ok && m.Kind == tui.MousePress {
		p.mu.Lock()
		p.presses = append(p.presses, tui.Point{X: m.X, Y: m.Y})
		p.mu.Unlock()
		return true
	}
	return false
}

func (p *pointerProbe) got() []tui.Point {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]tui.Point(nil), p.presses...)
}

// pressThrough runs a probe under s (s stacked over it at 2,1, 4x2 cells) on a terminal TestBackend,
// presses at (x, y) and answers what the probe got.
func pressThrough(t *testing.T, s *Shape, x, y int) []tui.Point {
	t.Helper()
	probe := &pointerProbe{}
	root := tui.NewStack()
	root.AddFilled(probe)
	root.AddAt(s, 2, 1)
	tb := tui.NewTestBackend(10, 4)
	app := tui.NewApp(root, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.Run(ctx)
	for deadline := time.Now().Add(5 * time.Second); tb.Flushes() == 0; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the app never drew")
		}
	}
	done, err := tb.InjectFenced(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the press was never dispatched")
	}
	return probe.got()
}

// A shape takes no pointer events, as Qt's Rectangle takes none: a press on a shape stacked over a
// widget reaches the widget, at the same point.
func TestAPressOnAShapeReachesTheWidgetBeneath(t *testing.T) {
	t.Parallel()
	got := pressThrough(t, NewShape(ShapeRect, WithShapeSize(4, 2)), 3, 2)
	if len(got) != 1 || got[0] != (tui.Point{X: 3, Y: 2}) {
		t.Fatalf("the widget beneath got %v, want the press at (3,2)", got)
	}
}

// A shape that accepts the pointer (WithAcceptsPointer, or SetAcceptsPointer at run time) takes the
// press, and the widget beneath gets none of it, though it still gets a press beside the shape.
func TestAShapeThatAcceptsThePointerTakesThePress(t *testing.T) {
	t.Parallel()
	if got := pressThrough(t, NewShape(ShapeRect, WithShapeSize(4, 2), WithAcceptsPointer(true)), 3, 2); len(got) != 0 {
		t.Fatalf("the widget beneath got %v through a shape that accepts the pointer", got)
	}
	s := NewShape(ShapeRect, WithShapeSize(4, 2))
	s.SetAcceptsPointer(true)
	if got := pressThrough(t, s, 3, 2); len(got) != 0 {
		t.Fatalf("SetAcceptsPointer(true): the widget beneath got %v", got)
	}
	if got := pressThrough(t, NewShape(ShapeRect, WithShapeSize(4, 2), WithAcceptsPointer(true)), 8, 0); len(got) != 1 {
		t.Fatalf("beside the shape the widget got %v, want the press", got)
	}
}

// Where nothing is drawn natively (a terminal), a shape set to its notice shows the localised
// "not in the terminal" notice, centred and alone, instead of its outline.
func TestAShapeSetToItsNoticeSaysSoOnATerminal(t *testing.T) {
	s := NewShape(ShapeRect)
	s.SetNotice(true)
	const text = "Not available in the terminal"
	w, h := len(text)+4, 3
	blank := strings.Repeat(" ", w)
	want := blank + "\n" + "  " + text + "  " + "\n" + blank
	if got := shapeScreen(t, s, w, h, want); got != want {
		t.Fatalf("the screen is\n%s\nwant\n%s", got, want)
	}
}
