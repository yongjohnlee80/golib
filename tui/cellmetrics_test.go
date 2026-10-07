package tui

import "testing"

type metricBackend struct {
	*TestBackend
	cell CellPixels
}

func (b *metricBackend) CellPixels() CellPixels { return b.cell }

func TestCellPixels(t *testing.T) {
	plain := startApp(t, &probe{name: "plain"}, 4, 2)
	var got CellPixels
	plain.onLoop(func() { got = plain.app.CellPixels() })
	if got != (CellPixels{}) {
		t.Fatalf("a plain backend reports cell pixels %v", got)
	}

	p := &probe{name: "p"}
	mb := &metricBackend{TestBackend: NewTestBackend(4, 2), cell: CellPixels{W: 9, H: 18}}
	h := runApp(t, NewApp(p, WithBackend(mb), WithMinFrameInterval(0)), mb.TestBackend)
	h.onLoop(func() { got = p.ctx.CellPixels() })
	if got != (CellPixels{W: 9, H: 18}) {
		t.Fatalf("Context.CellPixels = %v; want the backend's 9×18", got)
	}
}

// The sub-cell position travels with the event to the node, untranslated, while X and Y become
// local.
func TestMouseSubCellPositionSurvivesRouting(t *testing.T) {
	child := &probe{name: "child"}
	root := &box{rects: []Rect{{X: 2, Y: 1, W: 4, H: 2}}}
	root.Add(child)
	h := startApp(t, root, 8, 4)
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 3, Y: 2, SubX: 0.25, SubY: 0.75})
	waitFor(t, "the press", func() bool { return len(probeEvents(child)) > 0 })
	for _, ev := range probeEvents(child) {
		if m, ok := ev.(MouseEvent); ok && m.Kind == MousePress {
			if m.X != 1 || m.Y != 1 || m.SubX != 0.25 || m.SubY != 0.75 {
				t.Fatalf("delivered %+v; want local 1,1 with SubX 0.25, SubY 0.75", m)
			}
			return
		}
	}
	t.Fatal("no press delivered")
}
