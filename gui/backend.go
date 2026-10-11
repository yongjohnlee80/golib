package gui

import (
	"context"
	"errors"
	"image"
	"image/color"
	"sync"
	"sync/atomic"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"golang.org/x/image/math/fixed"

	"github.com/yongjohnlee80/golib/tui"
)

// ErrNotStarted is returned by Size before Start has opened the window.
var ErrNotStarted = errors.New("gui: backend not started")

// errClosedBeforeOpen is Start's answer when the window is destroyed before its first frame.
var errClosedBeforeOpen = errors.New("gui: window closed before it opened")

// Backend is an implementation of [tui.Backend] that displays TUI applications in a native
// operating system window using Gio.
//
// In addition to the fundamental [tui.Backend] interface, Backend implements:
//   - [tui.GraphicsBackend]: Renders [tui.ImagePlacement] bitmaps natively via [Backend.PlaceImage].
//   - [tui.ClipboardWriter]: Writes UTF-8 text to the host system clipboard via [Backend.WriteClipboard].
//   - [tui.CursorColorSetter]: Configures dynamic caret styling via [Backend.SetCursorColor].
//   - [tui.NativeHost]: Hosts arbitrary vector graphics views through [Backend.PlaceNatives].
//   - [tui.NativeStyler]: Inquires component styles through [Backend.NativeFor].
//   - [tui.CellMetrics]: Reports logical cell pixel dimensions via [Backend.CellPixels].
//
// Concurrency Model:
// Backend coordinates state between the TUI event loop goroutine and the Gio window event goroutine.
// All public methods conforming to [tui.Backend] are invoked exclusively on the TUI event loop.
// Fields mutated during Flush, cursor updates, or image placements are strictly loop-owned,
// while cross-thread handoffs to Gio (metrics, font generations) use atomic pointers, and completed
// frames or clipboard requests latch under Backend.mu without waiting for GPU presentation.
type Backend struct {
	cfg config
	win *app.Window

	// font is the cell font as the Gio goroutine reads it: one Load per frame, for measure and
	// the IME's preedit alike. The tui loop replaces it (SetFont, SetZoom) and never mutates one.
	font atomic.Pointer[fontState]
	// The tui loop owns these: what SetFont, SetProseFont and SetZoom were last given.
	fontSize unit.Sp // the size before zoom
	zoom     int     // percent

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
	imgList []uint32              // placement order
	natives []tui.NativePlacement // this frame's, from PlaceNatives

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
	// fg and bg are the colours of the text at the caret, for the preedit drawn there; tinted
	// false: unknown, and the window theme's are used.
	fg, bg color.NRGBA
	tinted bool
}

// frameCaret puts the frame's caret, for the input method and the preedit: the cell cursor
// when it shows, with its cell's colours, else a native view's own caret, read after it painted.
func (b *Backend) frameCaret(f *frame, m metrics) {
	if b.cursor.visible {
		f.caret = m.cellRect(b.cursor.x, b.cursor.y, 1, 1)
		f.base = m.baseline
		f.fg, f.bg = b.render.colors(b.grid.at(b.cursor.x, b.cursor.y).Attrs)
		f.tinted = true
	} else if c, ok := viewCaret(m, b.natives); ok {
		f.caret, f.base, f.fg, f.bg, f.tinted = c.rect, c.base, c.fg, c.bg, c.tinted
	}
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
	cfg.typeface = cellTypeface(cfg) // the full list, once: fallbackChain leaves it as it is
	b := &Backend{
		cfg:      cfg,
		win:      new(app.Window),
		fontSize: cfg.fontSize,
		zoom:     100,
		q:        newEventQueue(),
		started:  make(chan struct{}),
		render:   newRenderer(cells, fallbackChain(cfg.typeface), cfg.theme),
		images:   map[uint32]placedImage{},
	}
	b.gio.shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	b.font.Store(&fontState{fm: measureFont(cells, fallbackChain(cfg.typeface)), typeface: fallbackChain(cfg.typeface),
		size: cfg.fontSize})
	return b
}

// fontState is one hand-over of the cell font to the Gio goroutine.
type fontState struct {
	fm       fontMetrics
	typeface string  // the cell typeface list, fallbackChain's
	size     unit.Sp // the size the cells are drawn at: SetFont's, times SetZoom's percent
	gen      uint64  // bumped by every change of font, family or size
}

// SetFont changes the cell font while the window runs: typeface a CSS-style family list as
// WithFont takes it, size in sp. "" keeps the typeface, and size 0 the size. A family that is not
// installed falls back as WithFont's does. The window lays out and paints again at the next frame,
// whether or not the cell's size changed. Call it on the tui loop: from a handler, or in Post.
func (b *Backend) SetFont(typeface string, size float32) {
	if typeface != "" {
		b.cfg.typeface, b.cfg.fontSet = typeface, true
		b.cfg.typeface = cellTypeface(b.cfg)
	}
	if size > 0 {
		b.fontSize = unit.Sp(size)
	}
	b.refont(typeface != "")
}

// SetZoom scales the cell font, and with it everything sized by the window's text (Canvas.TextSize),
// to pct percent of SetFont's size: 50 to 300, 100 the size itself. On the tui loop.
func (b *Backend) SetZoom(pct int) {
	b.zoom = min(max(pct, 50), 300)
	b.refont(false)
}

// SetProseFont is the family a Font with no Family draws in, in the window's native views (the
// Rendered view's prose, HTMLView's default, the native style's labels): a CSS-style family list,
// "" for the default ("sans-serif, emoji"). On the tui loop.
func (b *Backend) SetProseFont(family string) {
	if family == "" {
		family = uiFamily
	}
	b.render.fonts.Prose = family
	b.refont(false)
}

// refont publishes a new fontState: the cell font measured again, a new generation, and the
// renderer's face when the typeface changed (the glyphs it cached are cleared by the generation).
// Then it asks Gio for a frame, which pushes the App a ResizeEvent at the new generation.
func (b *Backend) refont(face bool) {
	old := b.font.Load()
	tf := fallbackChain(b.cfg.typeface)
	fs := &fontState{fm: old.fm, typeface: tf, size: b.fontSize * unit.Sp(b.zoom) / 100, gen: old.gen + 1}
	if face {
		fs.fm = measureFont(b.render.shaper, tf)
		b.render.typeface = font.Typeface(tf)
		b.render.fonts.Mono = tf
	}
	b.render.fonts.Gen = fs.gen
	b.font.Store(fs)
	b.win.Invalidate()
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

// Start initializes the native window, configures window properties, launches the
// internal event forwarder and Gio event loops, and blocks until the initial frame has
// published valid window metrics.
//
// By waiting for the first frame's metrics, Start guarantees that subsequent calls to [Backend.Size]
// return true window dimensions rather than zero or uninitialized values. The initial configuration
// event is consumed internally.
//
// If ctx is cancelled before the window opens, Start terminates the window and returns ctx.Err().
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

// Stop shuts down the native window and terminates event processing.
//
// Stop is idempotent and safe to call multiple times. It signals window termination and releases
// internal event queues. For a backend that completed [Backend.Start], Stop waits for the forwarder
// goroutine to exit and guarantees that the channel returned by [Backend.Events] is closed upon return.
func (b *Backend) Stop() error {
	b.q.end(nil)
	b.win.Perform(system.ActionClose)
	if b.forwarding.Load() {
		<-b.q.exited
	}
	return nil
}

// Size reports the current grid dimensions in whole monospace character cells.
// If the backend has not completed [Backend.Start], Size returns [ErrNotStarted].
func (b *Backend) Size() (tui.Size, error) {
	m := b.metrics.Load()
	if m == nil {
		return tui.Size{}, ErrNotStarted
	}
	return m.grid, nil
}

// Grid returns the active cell grid dimensions and the font generation counter at which
// those dimensions were measured.
//
// When modifying fonts via [Backend.SetFont] or zoom levels via [Backend.SetZoom], callers
// can inspect the returned generation counter against [Backend.FontGeneration] to determine
// whether the reported grid reflects the updated typography or an earlier layout snapshot.
func (b *Backend) Grid() (tui.Size, uint64) {
	m := b.metrics.Load()
	if m == nil {
		return tui.Size{}, 0
	}
	return m.grid, m.fontGen
}

// FontGeneration returns the monotonic generation counter of the most recently requested
// font or zoom configuration (via [Backend.SetFont], [Backend.SetProseFont], or [Backend.SetZoom]).
func (b *Backend) FontGeneration() uint64 { return b.font.Load().gen }

// Events returns the receive-only channel carrying decoded, un-coalesced [tui.Event] values.
//
// Events is closed exactly once when the backend shuts down cleanly or encounters an unrecoverable
// window error.
func (b *Backend) Events() <-chan tui.Event { return b.q.events }

// Err returns the terminating error that caused the backend to stop, such as [ErrEventOverflow],
// a window destruction error, or a context cancellation during [Backend.Start]. It returns nil
// after a clean [Backend.Stop].
func (b *Backend) Err() error { return b.q.reason() }

// Capabilities reports the terminal-compatible capabilities supported by the native window backend.
//
// It reports [tui.ProfileTrueColor], full Kitty keyboard protocol support, bracketed paste,
// hardware mouse tracking, native vector view embedding, and image support. Protocol-specific
// terminal control modes (such as SyncOutput, InBandResize, and Undercurl) are reported as false.
func (b *Backend) Capabilities() tui.Capabilities {
	t := b.cfg.theme
	return tui.Capabilities{
		ColorProfile:   tui.ProfileTrueColor,
		KittyKeyboard:  true,
		BracketedPaste: true,
		Mouse:          tui.TriYes,
		NativeViews:    true,
		KittyGraphics:  tui.TriYes,
		DarkBackground: t.dark(),
		DefaultFG:      probed(t.FG),
		DefaultBG:      probed(t.BG),
		// SyncOutput, InBandResize, UnicodeCore, Undercurl stay false: they name terminal
		// protocol features, and nothing outside tui/term reads them.
	}
}

// Flush rasterizes dirty cell updates and latched cursor state into a recorded vector frame
// macro and transfers it to the Gio presentation loop.
//
// Flush updates the internal grid buffer, executes run-length draw optimizations, captures caret
// coordinates for OS IME positioning, and latches the completed frame under [Backend.mu]. It signals
// Gio to redraw via [app.Window.Invalidate] without waiting for GPU presentation or for Gio to consume
// the previous frame.
func (b *Backend) Flush(diff []tui.CellUpdate) error {
	select {
	case <-b.q.done:
		return nil // no window to draw on
	default:
	}
	m := b.paintMetrics(diff)
	b.grid.apply(diff)
	draw := b.render.frame(&b.grid, m, b.cursor, b.natives, b.imageList(), b.focused.Load())
	f := &frame{draw: draw}
	b.frameCaret(f, m)
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

// fullRepaint reports whether diff covers every column of a w×h grid, row-major, with no gap:
// what the App emits for its first frame at a new size. A width-2 update covers two columns, since
// tui's diff never emits a continuation (tui/buffer.go diff, rule 2), so the update count alone
// says nothing.
func fullRepaint(diff []tui.CellUpdate, s tui.Size) bool {
	if s.W <= 0 || s.H <= 0 {
		return false
	}
	x, y := 0, 0
	for _, u := range diff {
		if u.X != x || u.Y != y {
			return false
		}
		x += max(int(u.Cell.Width), 1)
		if x > s.W {
			return false // a wide cell cannot hang past the edge (tui's W3)
		}
		if x == s.W {
			x, y = 0, y+1
		}
	}
	return x == 0 && y == s.H
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
