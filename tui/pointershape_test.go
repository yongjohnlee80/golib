package tui

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// shapeBackend is a TestBackend that shapes the pointer, keeping each frame's runs.
type shapeBackend struct {
	*TestBackend
	mu     sync.Mutex
	frames [][]PointerShapePlacement
}

func (b *shapeBackend) PlacePointerShapes(ps []PointerShapePlacement) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.frames = append(b.frames, append([]PointerShapePlacement(nil), ps...))
}

func (b *shapeBackend) last() []PointerShapePlacement {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.frames) == 0 {
		return nil
	}
	return b.frames[len(b.frames)-1]
}

// shaperBox is a box that shapes the pointer over its regions, counting each time it is asked.
type shaperBox struct {
	*box
	regions []PointerRegion
	asked   *atomic.Int64
}

func (s *shaperBox) PointerShapes(dst []PointerRegion) []PointerRegion {
	s.asked.Add(1)
	return append(dst, s.regions...)
}

func startShapes(t *testing.T, root Component, w, h int) *shapeBackend {
	t.Helper()
	sb := &shapeBackend{TestBackend: NewTestBackend(w, h)}
	runApp(t, NewApp(root, WithBackend(sb), WithMinFrameInterval(0)), sb.TestBackend)
	return sb
}

// runs is a frame's runs as (x, y, w, shape), ignoring their layers.
func runs(ps []PointerShapePlacement) [][4]int {
	out := make([][4]int, len(ps))
	for i, p := range ps {
		out[i] = [4]int{p.X, p.Y, p.W, int(p.Shape)}
	}
	return out
}

func waitRuns(t *testing.T, sb *shapeBackend, want [][4]int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !reflect.DeepEqual(runs(sb.last()), want) {
		if time.Now().After(deadline) {
			t.Fatalf("the frame's pointer shapes (x, y, w, shape) %v, want %v", runs(sb.last()), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A shaper's regions shape the pointer as they show: in absolute cells, its gutter (no region)
// left out, and the cells a later sibling paints over it (a dialog) taken away.
func TestAShapersRegionsShowWhereNothingCoversThem(t *testing.T) {
	var asked atomic.Int64
	ed := &shaperBox{box: &box{fill: "e"}, regions: []PointerRegion{{Rect: Rect{X: 2, W: 6, H: 4}, Shape: PointerText}}, asked: &asked}
	modal := &box{fill: "M"}
	root := &box{rects: []Rect{{X: 1, Y: 1, W: 8, H: 4}, {X: 4, Y: 2, W: 3, H: 2}}}
	root.Add(ed, modal)
	sb := startShapes(t, root, 12, 6)
	text := int(PointerText)
	waitRuns(t, sb, [][4]int{
		{3, 1, 6, text},
		{3, 2, 1, text}, {7, 2, 2, text},
		{3, 3, 1, text}, {7, 3, 2, text},
		{3, 4, 6, text},
	})
}

// A region is cut to its node's visible cells, and a shaper's own child, painted after it (a
// scrollbar), covers it; a default-shaped region adds nothing.
func TestAShapersRegionIsCutToWhatShows(t *testing.T) {
	var asked atomic.Int64
	bar := &box{fill: "|"}
	link := &shaperBox{box: &box{fill: "l", rects: []Rect{{X: 3, W: 1, H: 2}}}, regions: []PointerRegion{
		{Rect: Rect{W: 8, H: 2}, Shape: PointerHand},
		{Rect: Rect{W: 8, H: 1}, Shape: PointerDefault},
	}, asked: &asked}
	link.Add(bar)
	root := &box{rects: []Rect{{X: 8, W: 8, H: 2}}} // half off a 12-wide screen
	root.Add(link)
	sb := startShapes(t, root, 12, 2)
	hand := int(PointerHand)
	waitRuns(t, sb, [][4]int{{8, 0, 3, hand}, {8, 1, 3, hand}})
}

// On a backend that shapes no pointer, a shaper is never asked: not on a terminal, and not on one
// that draws native views (whose paint is stamped) but shows no pointer shapes.
func TestAShaperIsNotAskedWithoutAShapeHost(t *testing.T) {
	shaped := func() (Component, *atomic.Int64) {
		var asked atomic.Int64
		root := &box{rects: []Rect{{W: 4, H: 1}}}
		root.Add(&shaperBox{box: &box{fill: "e"}, regions: []PointerRegion{{Rect: Rect{W: 4, H: 1}, Shape: PointerText}}, asked: &asked})
		return root, &asked
	}
	root, asked := shaped()
	h := startApp(t, root, 4, 1)
	waitFor(t, "a frame", func() bool { return h.tb.Flushes() > 0 })
	h.sync()
	if n := asked.Load(); n != 0 {
		t.Errorf("a shaper was asked %d times on a terminal", n)
	}
	root, asked = shaped()
	nh, nb := startNative(t, root, 4, 1, nil)
	waitFor(t, "a frame", func() bool { return nb.Flushes() > 0 })
	nh.sync()
	if n := asked.Load(); n != 0 {
		t.Errorf("a shaper was asked %d times on a native-view backend that shows no pointer shapes", n)
	}
}
