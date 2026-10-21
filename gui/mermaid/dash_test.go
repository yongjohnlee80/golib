package mermaid

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// A dotted edge paints in bounded time and ops whatever the theme's font size, the dash's unit:
// none (0), a negative one, and one so small a float32 walk along the edge would never advance.
func TestADottedEdgePaintsForAnyFontSize(t *testing.T) {
	for _, size := range []float32{0, -4, 1e-6} {
		th := testTheme
		th.Font.Size = size
		l := &Laid{th: th, size: gui.Size{W: 400, H: 100}, fit: 1}
		l.edges = append(l.edges, laidEdge{pts: []gui.Point{{X: 10, Y: 50}, {X: 390, Y: 50}}, color: th.Line, line: pm.Dotted})
		rc := &segmentCanvas{RecordingCanvas: gui.NewRecordingCanvas(gui.Size{W: 400, H: 100}, gui.Size{W: 8, H: 16})}
		done := make(chan struct{})
		go func() { defer close(done); l.Paint(rc) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("font size %v: painting a dotted edge never finished", size)
		}
		if rc.segments > 2*4096 { // 4096 dashes, a move and a line each
			t.Errorf("font size %v: a dotted edge drew %d path segments", size, rc.segments)
		}
	}
}

// segmentCanvas counts the path segments stroked.
type segmentCanvas struct {
	*gui.RecordingCanvas
	segments int
}

func (c *segmentCanvas) StrokePath(p *gui.Path, width float32, b gui.Brush) {
	c.segments += p.Segments()
	c.RecordingCanvas.StrokePath(p, width, b)
}

// Lay refuses a theme whose font size is not positive, as unsupported: every length in a diagram is
// a multiple of it, so nothing could be laid out or drawn to scale.
func TestLayRefusesANonPositiveFontSize(t *testing.T) {
	m, release := gui.AcquireMeasurer(1)
	defer release()
	for _, size := range []float32{0, -1} {
		th := testTheme
		th.Font.Size = size
		_, err := Lay(context.Background(), "flowchart LR\n  A -.-> B", 400, th, m)
		if !errors.Is(err, pm.ErrUnsupported) {
			t.Errorf("font size %v: Lay returned %v, want ErrUnsupported", size, err)
		}
	}
}
