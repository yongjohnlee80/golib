package gui

import (
	"image"
	"sync"

	"github.com/yongjohnlee80/golib/tui"
)

// Panes is the tui root of a pixel-root window: a tree of rows and columns in dp whose leaves are
// cell panes (Cells) and pixel panes (Pixels). Give it to the App as its root and to the backend
// with WithPanes. Each frame the backend fits the tree to the window and gives every pane its own
// band of one virtual cell grid; the Panes places each pane's component at its band, so tui lays
// out, diffs and routes in cells as ever, and the backend maps each band to its pane's pixels.
//
// The tree is fixed for the Panes' life: to change them, build a new Panes.
type Panes struct {
	root   Node
	leaves []*leaf
	b      *Backend // set by WithPanes; nil lays out with a nominal cell (a terminal backend)
	ctx    *tui.Context
}

// NewPanes makes a Panes of root.
func NewPanes(root Node) *Panes { return &Panes{root: root, leaves: leaves(root)} }

// WithPanes makes the window's root pixel-laid-out by s, which must also be the App's root.
// Without it the window is one cell grid, as before.
func WithPanes(s *Panes) Option { return func(c *config) { c.stage = s } }

// nominalCell is the cell a Panes fits with when its backend has no cell pixels (a terminal): a
// typical monospace cell, so dp tracks become sensible column and row counts.
var nominalCell = image.Pt(8, 16)

// Init mounts every pane's component.
func (s *Panes) Init(ctx *tui.Context) {
	s.ctx = ctx
	for _, l := range s.leaves {
		ctx.Mount(l.c)
	}
}

// Layout admits one layout snapshot and places each pane's component at its band in it.
// Everything the tree then paints, and every caret and native placement, is drawn through that
// snapshot: the backend pairs them by the snapshot's generation, not by the grid's size.
func (s *Panes) Layout(cs tui.Constraints) tui.Size {
	size := tui.Size{W: cs.MaxW, H: cs.MaxH}
	var l *paneLayout
	if s.b != nil {
		l = s.b.admitLayout(size)
	}
	if l == nil {
		l = nominalLayout(s.root, size)
	}
	for i, p := range l.panes {
		if i >= len(s.leaves) {
			break
		}
		c := s.leaves[i].c
		band := intersect(p.band, tui.Rect{W: size.W, H: size.H})
		s.ctx.LayoutChild(c, tui.Tight(tui.Size{W: band.W, H: band.H}))
		s.ctx.PlaceChild(c, band)
	}
	return cs.Constrain(l.grid)
}

// Render draws nothing: the panes draw themselves, and a hole is never drawn.
func (s *Panes) Render(tui.Surface) {}

// HandleEvent consumes nothing.
func (s *Panes) HandleEvent(tui.Event) bool { return false }

// nominalLayout fits root to size cells of nominalCell pixels at scale 1: what a Panes lays out
// with under a backend that has no pixels.
func nominalLayout(root Node, size tui.Size) *paneLayout {
	inner := image.Rect(0, 0, size.W*nominalCell.X, size.H*nominalCell.Y)
	var gaps []image.Rectangle
	rects := fit(root, inner, 1, nil, &gaps)
	grid, panes := bands(root, rects, nominalCell)
	return &paneLayout{grid: grid, inner: inner, panes: panes, gaps: gaps}
}

// placeStage fits the Panes to m's window inside pad device pixels of padding and makes m that
// layout's snapshot: its grid the virtual grid, its origin the inner top-left. gen is left 0;
// publishLayout numbers it.
func placePanes(st *Panes, m metrics, pad int) metrics {
	inner := image.Rect(pad, pad, max(m.window.X-pad, pad), max(m.window.Y-pad, pad))
	var gaps []image.Rectangle
	rects := fit(st.root, inner, m.scale, nil, &gaps)
	grid, panes := bands(st.root, rects, m.cell)
	grid = tui.Size{W: max(grid.W, 1), H: max(grid.H, 1)}
	m.grid, m.origin = grid, inner.Min
	m.layout = &paneLayout{grid: grid, inner: inner, panes: panes, gaps: gaps}
	return m
}

// stageLayouts is the hand-over of layout snapshots from the Gio goroutine (publish) to the App
// goroutine (admit, then paint).
type stageLayouts struct {
	gen uint64 // Gio goroutine: the last generation given

	mu        sync.Mutex
	published []metrics // published since the last admission, oldest first

	admitted metrics // App goroutine: what the tree was last laid out with; layout nil before
}

// maxPublished bounds the snapshots waiting for admission. Dropping the oldest is safe: a newer
// one has its own ResizeEvent queued, so another Layout follows and admits it.
const maxPublished = 16

// publishLayout numbers m's layout and offers it for admission, when it differs from old's;
// otherwise m keeps old's layout and generation. Gio goroutine.
func (b *Backend) publishLayout(old *metrics, m metrics) metrics {
	if old != nil && old.layout != nil && old.layout.sameShape(m.layout) {
		m.layout = old.layout
		return m
	}
	b.stage.gen++
	m.layout.gen = b.stage.gen
	b.stage.mu.Lock()
	if len(b.stage.published) == maxPublished {
		b.stage.published = append(b.stage.published[:0], b.stage.published[1:]...)
	}
	b.stage.published = append(b.stage.published, m)
	b.stage.mu.Unlock()
	return m
}

// admitLayout takes the newest published snapshot whose grid is size (the App's size: the
// ResizeEvent it drained), drops it and every older one, and makes it the admitted snapshot.
// With none matching, the admitted snapshot stays, and nil is returned before any admission.
// App goroutine (Panes.Layout).
func (b *Backend) admitLayout(size tui.Size) *paneLayout {
	b.stage.mu.Lock()
	for i := len(b.stage.published) - 1; i >= 0; i-- {
		if b.stage.published[i].grid == size {
			b.stage.admitted = b.stage.published[i]
			b.stage.published = append(b.stage.published[:0], b.stage.published[i+1:]...)
			break
		}
	}
	b.stage.mu.Unlock()
	return b.stage.admitted.layout
}

// FlushSized is Flush with the App's size for the diff (tui.SizedFlusher). A Panes window paints
// with the admitted snapshot and only when its grid is size: a diff laid out for another
// snapshot updates the grid but is not presented, and the window keeps its shown frame until
// the Layout that admits the matching snapshot. A cell-root window flushes as Flush does.
func (b *Backend) FlushSized(size tui.Size, diff []tui.CellUpdate) error {
	if b.cfg.stage == nil {
		return b.Flush(diff)
	}
	select {
	case <-b.q.done:
		return nil
	default:
	}
	if size != (tui.Size{W: b.grid.w, H: b.grid.h}) {
		b.grid.resize(size.W, size.H) // tui never diffs across a size change: diff repaints it all
	}
	b.grid.apply(diff)
	m := b.stage.admitted
	if m.layout == nil || m.grid != size {
		return nil
	}
	b.paintM = m
	b.publishFrame(m)
	return nil
}

// inputMetrics is the snapshot pointer events map through. A Panes window maps them through the
// shown frame's snapshot, the cells the user sees, so a click never lands by a layout not yet
// drawn; before the first frame, and in a cell-root window, the newest. Gio goroutine.
func (b *Backend) inputMetrics(newest metrics) metrics {
	if b.cfg.stage != nil && b.gio.shown != nil && b.gio.shown.m.layout != nil {
		return b.gio.shown.m
	}
	return newest
}
