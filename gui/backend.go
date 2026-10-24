package gui

import (
	"context"
	"image"
	"image/color"
	"sync"
	"sync/atomic"

	"github.com/yongjohnlee80/golib/errs"

	"github.com/yongjohnlee80/gio/app"
	"github.com/yongjohnlee80/gio/font"
	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/io/system"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/gio/unit"
	"golang.org/x/image/math/fixed"

	"github.com/yongjohnlee80/golib/tui"
)

// ErrNotStarted is returned by Size before Start has opened the window.
var ErrNotStarted = errs.Sentinel(errs.ErrPrecondition, "gui: backend not started")

// errClosedBeforeOpen is Start's answer when the window is destroyed before its first frame.
var errClosedBeforeOpen = errs.Sentinel(errs.ErrClosed, "gui: window closed before it opened")

// Backend Concurrency and Thread Handoff Architecture:
//
//	+------------------------+                     +------------------------+
//	|   TUI Event Loop       |                     |  Gio Window Goroutine  |
//	|   (App / Widgets)      |                     |  (GPU Raster / Input)  |
//	+------------------------+                     +------------------------+
//	       |           |                                       |
//	       | Flush()   | Invalidate()                          | FrameEvent
//	       v           v                                       v
//	  +---------------------+   atomic snapshot   +-------------------------+
//	  | Backend.mu / latest | <================== | Backend.metrics         |
//	  | (Recorded CallOps)  | ==================> | Backend.font            |
//	  +---------------------+   atomic snapshot   +-------------------------+
//	                                                           |
//	                                                           v
//	                                              +-------------------------+
//	                                              |  eventQueue.forward()   |
//	                                              |  (Key / Mouse / Resize) |
//	                                              +-------------------------+
//	                                                           |
//	                                                           v
//	                                              +-------------------------+
//	                                              |  Backend.Events() chan  |
//	                                              +-------------------------+
//
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
// Backend coordinates state across three primary execution contexts:
//  1. The TUI event loop goroutine: owns the grid buffer, image map, cursor state, and
//     invokes Flush/PlaceImage/SetCursor.
//  2. The Gio event loop goroutine: owns the physical OS window, processes OS frames,
//     and consumes completed frame macros.
//  3. The event forwarder goroutine: decouples low-latency input event decoding from
//     the TUI dispatch pipeline.
//
// Cross-thread handoffs use atomic pointers for metrics and font state, and lock-protected
// latching under Backend.mu for frame macros and clipboard writes without blocking either loop.
type Backend struct {
	cfg config
	win *app.Window

	// font is the cell font: SetFont's size and SetZoom's percent, and the fontState the Gio
	// goroutine reads, one load per frame (fonts.go).
	font fontControl

	// metrics is the Gio goroutine's latest snapshot. Readers Load it once and use that value.
	metrics atomic.Pointer[metrics]
	focused atomic.Bool // the window has keyboard focus
	raw     atomic.Bool // SetRawKeys's request; the Gio goroutine applies it (rawkeys.go)

	q          *eventQueue
	started    chan struct{} // closed by the first FrameEvent
	startOnce  sync.Once
	forwarding atomic.Bool // Start launched the forwarder, so Stop may wait for it

	// tui's loop goroutine owns these: every Backend method below runs there.
	grid    grid
	paintM  metrics // the metrics the grid was laid out for (Flush)
	render  *renderer
	cursor  tui.CursorLatch       // the cursor calls; publishFrame takes it
	images  imageTable            // PlaceImage and DeleteImage; publishFrame applies them (images.go)
	natives []tui.NativePlacement // this frame's, from PlaceNatives

	stage stageLayouts // a Panes window's layout snapshots (panes.go)

	streams     streamSet // its live streams (stream.go)
	presentList []*stream // streamsPresented's list, reused; Gio goroutine
	presenting  []*stream // the streams the frame being presented selected; Gio goroutine

	// The hand-over of frames to the Gio goroutine, guarded by mu.
	mu     sync.Mutex
	latest *frame // the newest finished frame; Flush replaces it and never waits
	// clip is the clipboard's hand-over: a write, and the reads (clipboard_read.go). Its own lock.
	clip clipboardBridge

	// The Gio goroutine owns these.
	gio gioState

	// rec is the input path's recorder (recorder.go). Stop reports its close's error once runDone
	// says the close has happened.
	rec         inputRecorder
	runDone     chan struct{}
	runLaunched atomic.Bool // run was launched: its tail closes the recorder, and Stop waits
}

// frame is one finished frame: the recorded drawing and where the caret is, for the IME.
type frame struct {
	m        metrics // the snapshot it was drawn with: a Panes window's input maps through it
	draw     op.CallOp
	drawCost int             // draw's ops (gui_opcount; zero otherwise)
	parts    []framePart     // stream slots, each with the drawing after it
	caret    image.Rectangle // device pixels; empty when the cursor is hidden
	base     int             // the caret cell's baseline, from its top
	// fg and bg are the colours of the text at the caret, for the preedit drawn there; tinted
	// false: unknown, and the window theme's are used.
	fg, bg color.NRGBA
	tinted bool
	// cursors are the native views' pointer shapes, in paint order (pointershape.go)
	cursors []cursorArea
}

// frameCaret puts the frame's caret, for the input method and the preedit: the cell cursor
// when it shows, with its cell's colours, else a native view's own caret, read after it painted.
func (b *Backend) frameCaret(f *frame, m metrics, cur cursorState) {
	if _, in := m.pane(cur.x, cur.y); cur.visible && (in || m.layout == nil) {
		f.caret = m.cellRect(cur.x, cur.y, 1, 1)
		f.base = m.baseline
		f.fg, f.bg = b.render.colors(b.grid.at(cur.x, cur.y).Attrs)
		f.tinted = true
	} else if c, ok := viewCaret(m, b.natives); ok {
		f.caret, f.base, f.fg, f.bg, f.tinted = c.rect, c.base, c.fg, c.bg, c.tinted
	}
}

var (
	_ tui.Backend           = (*Backend)(nil)
	_ tui.GraphicsBackend   = (*Backend)(nil)
	_ tui.ClipboardWriter   = (*Backend)(nil)
	_ tui.ClipboardReader   = (*Backend)(nil)
	_ tui.CursorColorSetter = (*Backend)(nil)
)

// NewBackend makes a backend for a window that opens when the App starts.
func NewBackend(opts ...Option) *Backend {
	cfg := defaultConfig()
	for _, o := range opts {
		if o != nil { // a nil option is no option
			o(&cfg)
		}
	}
	// Two shapers: text.Shaper is not safe for concurrent use, and both tui's loop (cells) and
	// the Gio goroutine (the IME preedit) shape text. System fonts stay enabled, so characters
	// the cell font lacks (Hangul, CJK, symbols) fall back to installed fonts.
	cells := text.NewShaper(text.WithCollection(gofont.Collection()))
	cfg.typeface = cellTypeface(cfg) // the full list, once: fallbackChain leaves it as it is
	b := &Backend{
		cfg:     cfg,
		win:     new(app.Window),
		q:       newEventQueue(),
		started: make(chan struct{}),
		runDone: make(chan struct{}),
		render:  newRenderer(cells, fallbackChain(cfg.typeface), cfg.theme),
		images:  newImageTable(),
	}
	if cfg.stage != nil {
		cfg.stage.b = b
	}
	b.gio.shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	b.rec.r = cfg.recorder
	b.font.size, b.font.zoom = cfg.fontSize, 100
	b.font.state.Store(&fontState{fm: measureFont(cells, fallbackChain(cfg.typeface)), typeface: fallbackChain(cfg.typeface),
		size: cfg.fontSize, fonts: b.render.fonts})
	return b
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
		b.font.size = unit.Sp(size)
	}
	b.refont(typeface != "")
}

// SetCellFaces is tui.FaceHost: the families cells are drawn in by their Cell.Face, each falling
// back as the window's own family does when it is not installed. It is called on the tui loop, as
// Flush is, which is where the renderer runs: nothing else reads the table. An index never changes
// family (tui interns them), so the glyphs cached for it stay right.
func (b *Backend) SetCellFaces(families []string) {
	faces := make([]font.Typeface, len(families))
	for i, f := range families {
		if i > 0 && f != "" {
			faces[i] = font.Typeface(faceTypeface(f))
		}
	}
	b.render.faces = faces
}

// SetZoom scales the cell font, and with it everything sized by the window's text (Canvas.TextSize),
// to pct percent of SetFont's size: 50 to 300, 100 the size itself. On the tui loop.
func (b *Backend) SetZoom(pct int) {
	b.font.setZoom(pct)
	b.refont(false)
}

// SetProseFont is the family a Font with no Family draws in, in the window's native views (the
// Rendered view's prose, HTMLView's default, the native style's labels): a CSS-style family list,
// "" for the default ("sans-serif, emoji"). On the tui loop.
func (b *Backend) SetProseFont(family string) {
	if family == "" {
		family = uiFamily
	}
	b.render.fonts.Prose = withAliases(family) // a family by any of its names, as the cells
	b.refont(false)
}

// refont publishes a new fontState (fontControl.publish), measured again and the renderer's face
// changed when face. Then it asks Gio for a frame, which pushes the App a ResizeEvent at the new
// generation.
func (b *Backend) refont(face bool) {
	b.font.publish(b.render, b.cfg.typeface, face)
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
	b.runLaunched.Store(true)
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
	b.closeStreams() // pending pushes complete Closed; the frames are released
	b.win.Perform(system.ActionClose)
	if b.runLaunched.Load() {
		<-b.q.exited
		// The window goroutine closed the recorder on its way out; runDone closes
		// after that close, so its error can be read here without a race.
		<-b.runDone
	} else {
		// run was never launched, so its tail will not close the recorder: Stop is
		// the recorder's only close. The once keeps it single.
		b.rec.close()
	}
	return b.rec.err
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
func (b *Backend) FontGeneration() uint64 { return b.font.load().gen }

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
	if b.cfg.stage != nil {
		return b.FlushSized(tui.Size{W: b.grid.w, H: b.grid.h}, diff)
	}
	m := b.paintMetrics(diff)
	b.grid.apply(diff)
	b.publishFrame(m)
	return nil
}

// publishFrame draws the grid with m and hands the frame to the Gio goroutine.
func (b *Backend) publishFrame(m metrics) {
	cur := b.takeCursor()
	b.images.apply()
	draw, parts := b.render.frameParts(&b.grid, m, cur, b.natives, b.images.list(), b.focused.Load())
	f := &frame{draw: draw, parts: parts, m: m, drawCost: b.render.firstCost}
	b.frameCaret(f, m, cur)
	f.cursors = viewCursors(nil, m, b.natives)
	b.mu.Lock()
	b.latest = f
	b.mu.Unlock()
	b.win.Invalidate()
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

// The cursor and the images are latched, as every Backend latches them (tui.CursorLatch,
// tui.ImageLatch), and the next frame takes them.

// ShowCursor marks the cursor visible on the next Flush.
func (b *Backend) ShowCursor() { b.cursor.Show() }

// HideCursor hides the cursor on the next Flush.
func (b *Backend) HideCursor() { b.cursor.Hide() }

// SetCursor moves the cursor to (x, y) grid cell coordinates.
func (b *Backend) SetCursor(x, y int) { b.cursor.SetPos(x, y) }

// SetCursorShape selects the visual shape of the cursor (block, beam, or underline).
func (b *Backend) SetCursorShape(s tui.CursorShape) { b.cursor.SetShape(s) }

// SetCursorColor colours the caret; set=false gives it back the theme's cursor colour.
func (b *Backend) SetCursorColor(c tui.CellColor, set bool) { b.cursor.SetColor(c, set) }

// takeCursor is the latched cursor as the renderer draws it, its colour resolved by the theme the
// frame is drawn with.
func (b *Backend) takeCursor() cursorState {
	st, _ := b.cursor.Take()
	cur := cursorState{visible: st.Visible, x: st.X, y: st.Y, shape: st.Shape, colored: st.Colored}
	if st.Colored {
		cur.color = b.cfg.theme.resolve(st.Color, true)
	}
	return cur
}

// PlaceImage shows an image over its cells from the next Flush on.
func (b *Backend) PlaceImage(p tui.ImagePlacement) { b.images.latch.Place(p) }

// DeleteImage removes an image from the next Flush on.
func (b *Backend) DeleteImage(id uint32) { b.images.latch.Delete(id) }

// WriteClipboard sets the system clipboard. The Gio goroutine performs it on its next frame.
func (b *Backend) WriteClipboard(p []byte) error {
	b.clip.put(string(p))
	b.win.Invalidate()
	return nil
}
