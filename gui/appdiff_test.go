package gui

import (
	"context"
	"image"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// recorder is a tui.TestBackend that keeps every diff the App flushed, so the gui grid can be
// checked against tui's real diff contract rather than a synthetic one.
type recorder struct {
	*tui.TestBackend
	mu    sync.Mutex
	diffs [][]tui.CellUpdate
}

func (r *recorder) Flush(diff []tui.CellUpdate) error {
	r.mu.Lock()
	r.diffs = append(r.diffs, slices.Clone(diff))
	r.mu.Unlock()
	return r.TestBackend.Flush(diff)
}

func (r *recorder) take() [][]tui.CellUpdate {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.diffs
	r.diffs = nil
	return out
}

// settle waits until the App has flushed and gone quiet.
func settle(t *testing.T, r *recorder) {
	t.Helper()
	last, quiet := -1, 0
	for range 200 {
		n := r.Flushes()
		if n == last && n > 0 {
			if quiet++; quiet >= 3 {
				return
			}
		} else {
			quiet = 0
		}
		last = n
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the App never settled")
}

// replay applies diffs to b's grid exactly as Flush does, and checks the grid against the
// TestBackend's own picture of the screen.
func replay(t *testing.T, b *Backend, r *recorder, diffs [][]tui.CellUpdate) {
	t.Helper()
	for _, d := range diffs {
		b.paintMetrics(d)
		b.grid.apply(d)
	}
	snap := r.Snapshot()
	if b.grid.h != len(snap) || (len(snap) > 0 && b.grid.w != len(snap[0])) {
		t.Fatalf("grid is %dx%d; the App's screen is %dx%d", b.grid.w, b.grid.h, len(snap[0]), len(snap))
	}
	for y, row := range snap {
		for x, want := range row {
			if got := b.grid.at(x, y); got != want {
				t.Fatalf("cell (%d,%d) = %+v; the App shows %+v", x, y, got, want)
			}
		}
	}
}

// The App's real diffs, with Korean and emoji (width-2 cells) at the start, middle and right edge,
// through a first frame, edits that swap wide and narrow cells, and two resizes.
func TestGridFollowsTheAppsWideCells(t *testing.T) {
	text := widget.NewText("한글 ab 😀 cd 가")
	rec := &recorder{TestBackend: tui.NewTestBackend(12, 3)}
	app := tui.NewApp(text, tui.WithBackend(rec))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.Run(ctx)

	b := &Backend{}
	use := func(w, h int) {
		m := metrics{cell: image.Pt(8, 16), grid: tui.Size{W: w, H: h}}
		b.metrics.Store(&m)
	}
	use(12, 3)
	settle(t, rec)
	replay(t, b, rec, rec.take()) // the first frame: wide cells, and one at the right edge

	for _, s := range []string{"ab 한 cd", "가나다라마바사", "x"} { // narrow over wide, wide over narrow
		app.Update(func() { text.SetText(s) })
		settle(t, rec)
		replay(t, b, rec, rec.take())
	}

	for _, size := range []tui.Size{{W: 9, H: 4}, {W: 15, H: 2}} {
		app.Update(func() { text.SetText("한글 ab 😀 cd 가") })
		// Let the edit's frame land first: TestBackend resizes its own screen at once and refuses
		// a frame still laid out for the old width (the race paintMetrics handles in the window).
		settle(t, rec)
		replay(t, b, rec, rec.take())
		use(size.W, size.H)
		rec.InjectResize(size.W, size.H)
		settle(t, rec)
		replay(t, b, rec, rec.take()) // a resize: the full repaint carries wide cells
	}
}

func TestFullRepaintCountsWideCellsAsTwoColumns(t *testing.T) {
	wide := tui.Cell{Content: "한", Width: 2}
	diff := []tui.CellUpdate{
		{X: 0, Y: 0, Cell: wide}, {X: 2, Y: 0, Cell: cell("a")},
		{X: 0, Y: 1, Cell: cell("b")}, {X: 1, Y: 1, Cell: wide},
	}
	if !fullRepaint(diff, tui.Size{W: 3, H: 2}) {
		t.Fatal("a repaint with wide cells, one ending at the right edge, was not recognised")
	}
	if fullRepaint(diff[:3], tui.Size{W: 3, H: 2}) {
		t.Fatal("a partial diff was taken for a repaint")
	}
	if fullRepaint([]tui.CellUpdate{{X: 0, Y: 0, Cell: cell("a")}, {X: 1, Y: 0, Cell: wide}}, tui.Size{W: 2, H: 1}) {
		t.Fatal("a wide cell hanging past the edge was accepted")
	}
}

func TestGridWritesTheContinuation(t *testing.T) {
	var g grid
	g.resize(4, 1)
	g.apply([]tui.CellUpdate{{X: 1, Y: 0, Cell: cell("z")}, {X: 2, Y: 0, Cell: cell("y")}})
	wide := tui.Cell{Content: "😀", Width: 2, Attrs: tui.CellAttrs{Mask: tui.AttrBold}}
	g.apply([]tui.CellUpdate{{X: 1, Y: 0, Cell: wide}})
	if got := g.at(2, 0); !got.Continuation() || got.Attrs != wide.Attrs {
		t.Fatalf("the cell after a wide head is %+v; want its continuation, in the head's attributes", got)
	}
}
