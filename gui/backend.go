package gui

import (
	"context"
	"errors"
	"image"
	"sync"
	"sync/atomic"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"

	"github.com/yongjohnlee80/golib/tui"
)

// ErrNotStarted is returned by Size before Start has opened the window.
var ErrNotStarted = errors.New("gui: backend not started")

// errClosedBeforeOpen is Start's answer when the window is destroyed before its first frame.
var errClosedBeforeOpen = errors.New("gui: window closed before it opened")

// Backend is a tui.Backend that is a native window. It also implements tui.GraphicsBackend,
// tui.ClipboardWriter and tui.CursorColorSetter, so images, copy and the cursor colour work as
// they do on a terminal that supports them.
type Backend struct {
	cfg config
	win *app.Window
	fm  fontMetrics

	// metrics is the Gio goroutine's latest snapshot. Readers Load it once and use that value.
	metrics atomic.Pointer[metrics]
	focused atomic.Bool // the window has keyboard focus

	q          *eventQueue
	started    chan struct{} // closed by the first FrameEvent
	startOnce  sync.Once
	forwarding atomic.Bool // Start launched the forwarder, so Stop may wait for it

	// tui's loop goroutine owns these: every Backend method below runs there.
	grid    grid
	paintM  metrics // the metrics the grid was laid out for (Flush)
	render  *renderer
	cursor  cursorState
	images  map[uint32]placedImage
	imgList []uint32 // placement order

	// The hand-over to the Gio goroutine, guarded by mu.
	mu       sync.Mutex
	latest   *frame  // the newest finished frame; Flush replaces it and never waits
	copyText *string // a clipboard write for the Gio goroutine to perform

	// The Gio goroutine owns these.
	gio gioState
}

// frame is one finished frame: the recorded drawing and where the caret is, for the IME.
type frame struct {
	draw  op.CallOp
	caret image.Rectangle // device pixels; empty when the cursor is hidden
	base  int             // the caret cell's baseline, from its top
}

var (
	_ tui.Backend           = (*Backend)(nil)
	_ tui.GraphicsBackend   = (*Backend)(nil)
	_ tui.ClipboardWriter   = (*Backend)(nil)
	_ tui.CursorColorSetter = (*Backend)(nil)
)

// NewBackend makes a backend for a window that opens when the App starts.
func NewBackend(opts ...Option) *Backend {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	// Two shapers: text.Shaper is not safe for concurrent use, and both tui's loop (cells) and
	// the Gio goroutine (the IME preedit) shape text. System fonts stay enabled, so characters
	// the cell font lacks (Hangul, CJK, symbols) fall back to installed fonts.
	cells := text.NewShaper(text.WithCollection(gofont.Collection()))
	b := &Backend{
		cfg:     cfg,
		win:     new(app.Window),
		fm:      measureFont(cells, cfg.typeface),
		q:       newEventQueue(),
		started: make(chan struct{}),
		render:  newRenderer(cells, cfg.typeface, cfg.theme),
		images:  map[uint32]placedImage{},
	}
	b.gio.shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	return b
}

// measureFont measures the cell font's "0" at 100px per em, and scales it to one em.
func measureFont(s *text.Shaper, typeface string) fontMetrics {
	const ref = 100
	s.LayoutString(text.Parameters{Font: fontOf(typeface), PxPerEm: fixed.I(ref), MaxWidth: 1 << 20}, "0")
	g, ok := s.NextGlyph()
	for _, more := s.NextGlyph(); more; _, more = s.NextGlyph() {
		// drain the layout, so the shaper starts clean
	}
	if !ok || g.Advance <= 0 {
		return fontMetrics{advance: 0.6, ascent: 0.8, descent: 0.2} // a typical monospace cell
	}
	toEm := func(v fixed.Int26_6) float32 { return float32(v) / 64 / ref }
	return fontMetrics{advance: toEm(g.Advance), ascent: toEm(g.Ascent), descent: toEm(g.Descent)}
}

// Start opens the window, starts the Gio goroutine and the event forwarder, and returns once the
// first frame has published metrics, so the App's Size and first frame read a real window. That
// first configuration is consumed, not sent: the App reads the size from Size, and tui/term sends
// no focus event at start either. Cancelling ctx first closes the window and returns ctx.Err().
func (b *Backend) Start(ctx context.Context) error {
	b.win.Option(b.cfg.windowOptions()...)
	b.forwarding.Store(true)
	go b.q.forward()
	go b.run()
	select {
	case <-b.started:
		return nil
	case <-b.q.done:
		if err := b.q.reason(); err != nil {
			return err
		}
		return errClosedBeforeOpen
	case <-ctx.Done():
		b.q.end(ctx.Err())
		b.win.Perform(system.ActionClose)
		<-b.q.exited
		return ctx.Err()
	}
}

// Stop closes the window. It is idempotent and returns with Events closed, whether or not Gio
// ever delivers the window's DestroyEvent.
func (b *Backend) Stop() error {
	b.q.end(nil)
	b.win.Perform(system.ActionClose)
	if b.forwarding.Load() {
		<-b.q.exited
	}
	return nil
}

// Size is the grid that fits the window, in whole cells.
func (b *Backend) Size() (tui.Size, error) {
	m := b.metrics.Load()
	if m == nil {
		return tui.Size{}, ErrNotStarted
	}
	return m.grid, nil
}

// Events is the window's input, in order and uncoalesced. Closed once, by the forwarder.
func (b *Backend) Events() <-chan tui.Event { return b.q.events }

// Err is what ended the backend: the window's DestroyEvent error, ErrEventOverflow, or Start's
// context error. Nil after a clean Stop or a clean close. The first reason is the one kept.
func (b *Backend) Err() error { return b.q.reason() }

// Capabilities claims what the window does as a terminal would (ADR 1791330692 §4.3). It is
// constant after Start.
func (b *Backend) Capabilities() tui.Capabilities {
	t := b.cfg.theme
	return tui.Capabilities{
		ColorProfile:   tui.ProfileTrueColor,
		KittyKeyboard:  true,
		BracketedPaste: true,
		Mouse:          tui.TriYes,
		KittyGraphics:  tui.TriYes,
		DarkBackground: t.dark(),
		DefaultFG:      probed(t.FG),
		DefaultBG:      probed(t.BG),
		// SyncOutput, InBandResize, UnicodeCore, Undercurl stay false: they name terminal
		// protocol features, and nothing outside tui/term reads them.
	}
}

// Flush draws a frame and hands it to the Gio goroutine. It never blocks: a frame Gio never took
// is replaced, because every frame is a complete picture.
func (b *Backend) Flush(diff []tui.CellUpdate) error {
	select {
	case <-b.q.done:
		return nil // no window to draw on
	default:
	}
	m := b.paintMetrics(diff)
	b.grid.apply(diff)
	draw := b.render.frame(&b.grid, m, b.cursor, b.imageList(), b.focused.Load())
	f := &frame{draw: draw}
	if b.cursor.visible {
		f.caret = m.cellRect(b.cursor.x, b.cursor.y, 1, 1)
		f.base = m.baseline
	}
	b.mu.Lock()
	b.latest = f
	b.mu.Unlock()
	b.win.Invalidate()
	return nil
}

// paintMetrics chooses the metrics a frame is drawn with, so the App's layout and the paint
// agree after a resize. The Gio goroutine publishes a new grid size before the App has handled
// the ResizeEvent that announces it, so a frame may still be laid out for the old grid. The App
// never diffs across a size change (tui/app.go renderFrame): its first frame at a new size
// repaints every cell, row-major. Until such a frame arrives, the old metrics are kept.
func (b *Backend) paintMetrics(diff []tui.CellUpdate) metrics {
	m := *b.metrics.Load()
	if m.grid == (tui.Size{W: b.grid.w, H: b.grid.h}) {
		b.paintM = m // same grid: a new scale or font size draws at once
		return m
	}
	if fullRepaint(diff, m.grid) {
		b.grid.resize(m.grid.W, m.grid.H)
		b.paintM = m
	}
	return b.paintM
}

// fullRepaint reports whether diff is every cell of a w×h grid in row-major order: what the App
// emits for its first frame at a new size.
func fullRepaint(diff []tui.CellUpdate, s tui.Size) bool {
	n := s.W * s.H
	if n == 0 || len(diff) != n {
		return false
	}
	last := diff[n-1]
	if diff[0].X != 0 || diff[0].Y != 0 || last.X != s.W-1 || last.Y != s.H-1 {
		return false
	}
	return s.H == 1 || (diff[s.W].X == 0 && diff[s.W].Y == 1)
}

// The cursor is latched, as tui/term latches it, and drawn by the next Flush.

func (b *Backend) ShowCursor()                      { b.cursor.visible = true }
func (b *Backend) HideCursor()                      { b.cursor.visible = false }
func (b *Backend) SetCursor(x, y int)               { b.cursor.x, b.cursor.y = x, y }
func (b *Backend) SetCursorShape(s tui.CursorShape) { b.cursor.shape = s }

// SetCursorColor colours the caret; set=false gives it back the theme's cursor colour.
func (b *Backend) SetCursorColor(c tui.CellColor, set bool) {
	b.cursor.colored = set
	if set {
		b.cursor.color = b.cfg.theme.resolve(c, true)
	}
}

// PlaceImage shows an image over its cells from the next Flush on. The PNG is decoded once per
// version: a placement that only moves or changes its Clip reuses it.
func (b *Backend) PlaceImage(p tui.ImagePlacement) {
	old, had := b.images[p.ID]
	if had && old.Version == p.Version {
		old.ImagePlacement = p
		b.images[p.ID] = old
		return
	}
	img, ok := decodeImage(p)
	if !ok {
		return // not a PNG: the cells beneath show, as on a terminal that refuses it
	}
	if !had {
		b.imgList = append(b.imgList, p.ID)
	}
	b.images[p.ID] = img
}

// DeleteImage removes an image from the next Flush on.
func (b *Backend) DeleteImage(id uint32) {
	if _, ok := b.images[id]; !ok {
		return
	}
	delete(b.images, id)
	for i, v := range b.imgList {
		if v == id {
			b.imgList = append(b.imgList[:i], b.imgList[i+1:]...)
			break
		}
	}
}

func (b *Backend) imageList() []placedImage {
	out := make([]placedImage, 0, len(b.imgList))
	for _, id := range b.imgList {
		out = append(out, b.images[id])
	}
	return out
}

// WriteClipboard sets the system clipboard. The Gio goroutine performs it on its next frame.
func (b *Backend) WriteClipboard(p []byte) error {
	s := string(p)
	b.mu.Lock()
	b.copyText = &s
	b.mu.Unlock()
	b.win.Invalidate()
	return nil
}
