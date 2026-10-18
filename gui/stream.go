package gui

import (
	"context"
	"image"
	"sync"
	"time"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/clip"
	"github.com/yongjohnlee80/gio/op/extimage"
	"github.com/yongjohnlee80/gio/op/paint"
)

// Streams: a view's pictures at the display's pace, without a tui flush.
//
// A stream belongs to the Backend that made it. Its producer pushes from any goroutine; the
// window draws the newest picture into every slot DrawStream recorded, once a vsync, and only
// when something was pushed. Each push returns a Receipt saying what became of it.
//
// One state machine serves both kinds: a Stream of frames (op/extimage: decoded video, another
// GPU's renders, memory pixels) and a CanvasStream of pictures drawn with a Canvas.

// Frame is a picture to draw: op/extimage's, re-exported so an app imports golib/gui alone.
type Frame = extimage.Frame

// Outcome is what became of a pushed picture.
type Outcome uint8

const (
	_ Outcome = iota
	// Drawn: submitted in a presented window frame (the time is Receipt.When). It is not
	// confirmation that it reached the glass; the toolkit reports no scanout time.
	Drawn
	// Superseded: a newer push replaced it before it was drawn.
	Superseded
	// NotShown: a window frame was presented without the stream in it (its view is hidden).
	// The picture stays pending and is drawn if the view comes back.
	NotShown
	// Unsupported: the window draws no frame like it (FrameCaps); it was never queued.
	Unsupported
	// Closed: the stream was closed, or its window stopped.
	Closed
	// Busy: a CanvasStream was already recording; the draw function was not called.
	Busy
)

func (o Outcome) String() string {
	switch o {
	case Drawn:
		return "Drawn"
	case Superseded:
		return "Superseded"
	case NotShown:
		return "NotShown"
	case Unsupported:
		return "Unsupported"
	case Closed:
		return "Closed"
	case Busy:
		return "Busy"
	}
	return "pending"
}

// Receipt is one push's outcome. It is made when the push is admitted, so its completion can't be
// missed however soon it comes; it completes exactly once.
type Receipt struct {
	done    chan struct{}
	once    sync.Once
	outcome Outcome
	when    time.Time
	then    func(Outcome) // the producer side's own follow-up (MutableImage), run once on completion
}

func newReceipt() *Receipt { return &Receipt{done: make(chan struct{})} }

// receiptOf is a receipt already complete with o.
func receiptOf(o Outcome) *Receipt {
	r := newReceipt()
	r.complete(o, time.Time{})
	return r
}

func (r *Receipt) complete(o Outcome, when time.Time) {
	r.once.Do(func() {
		r.outcome, r.when = o, when
		close(r.done)
		if r.then != nil {
			r.then(o)
		}
	})
}

// Done is closed when the outcome is known.
func (r *Receipt) Done() <-chan struct{} { return r.done }

// Wait blocks until the outcome is known or ctx ends.
func (r *Receipt) Wait(ctx context.Context) (Outcome, error) {
	select {
	case <-r.done:
		return r.outcome, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// Outcome is the outcome, or 0 while it is not known.
func (r *Receipt) Outcome() Outcome {
	select {
	case <-r.done:
		return r.outcome
	default:
		return 0
	}
}

// When is the frame time of the window frame that drew it (its FrameEvent's Now), for an outcome
// of Drawn; zero otherwise. Pace by it; audio sync adds the display's own latency.
func (r *Receipt) When() time.Time {
	<-r.done
	return r.when
}

// payload is one picture a stream holds.
type payload interface {
	// add draws it scaled into dst (the slot's coordinates, after the slot's transform).
	add(ops *op.Ops, dst Rect, report func(error))
	// drop gives up the stream's hold on it, outside every lock.
	drop()
}

// stream is the state machine both stream kinds share.
type stream struct {
	b *Backend

	mu      sync.Mutex
	pending payload
	waiting *Receipt // pending's receipt
	shown   payload
	closed  bool
	err     error
	drawn   bool        // a slot drew it in the frame being presented
	report  func(error) // setErr, made once: a method value per slot drawn would allocate
}

func (s *stream) init(b *Backend) {
	s.b = b
	s.report = s.setErr
	b.streams.add(s)
}

// push admits p and returns its receipt; a pending picture it replaces is superseded. then, if
// not nil, runs once when the receipt completes; it is set before anyone can complete it.
func (s *stream) push(p payload, then func(Outcome)) *Receipt {
	r := newReceipt()
	r.then = then
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		p.drop()
		r.complete(Closed, time.Time{})
		return r
	}
	old, oldR := s.pending, s.waiting
	s.pending, s.waiting = p, r
	s.mu.Unlock()
	if old != nil {
		old.drop()
		oldR.complete(Superseded, time.Time{})
	}
	s.b.invalidate()
	return r
}

// current is the picture a slot draws now: the pending one if any (it is about to be shown),
// else the shown one. Gio goroutine, during present.
func (s *stream) current() payload {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drawn = true
	if s.pending != nil {
		return s.pending
	}
	return s.shown
}

// presented settles the stream after the window frame that drew (or did not draw) it was handed
// to the toolkit: a pending picture a slot drew becomes the shown one, and its receipt Drawn; one
// no slot drew is NotShown, and stays pending. Gio goroutine, after the toolkit's Frame.
func (s *stream) presented(now time.Time) {
	s.mu.Lock()
	drawn := s.drawn
	s.drawn = false
	if s.pending == nil {
		s.mu.Unlock()
		return
	}
	if !drawn {
		r := s.waiting
		s.mu.Unlock()
		r.complete(NotShown, time.Time{})
		return
	}
	old := s.shown
	s.shown, s.pending = s.pending, nil
	r := s.waiting
	s.waiting = nil
	s.mu.Unlock()
	if old != nil {
		old.drop()
	}
	r.complete(Drawn, now)
}

// close drops both pictures and completes a pending receipt Closed. A push after it is Closed.
func (s *stream) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	pending, shown, r := s.pending, s.shown, s.waiting
	s.pending, s.shown, s.waiting = nil, nil, nil
	s.mu.Unlock()
	s.b.streams.remove(s)
	if pending != nil {
		pending.drop()
		r.complete(Closed, time.Time{})
	}
	if shown != nil {
		shown.drop()
	}
}

func (s *stream) setErr(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

func (s *stream) lastErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// streamSet is a backend's live streams.
type streamSet struct {
	mu  sync.Mutex
	all map[*stream]struct{}
}

func (ss *streamSet) add(s *stream) {
	ss.mu.Lock()
	if ss.all == nil {
		ss.all = map[*stream]struct{}{}
	}
	ss.all[s] = struct{}{}
	ss.mu.Unlock()
}

func (ss *streamSet) remove(s *stream) {
	ss.mu.Lock()
	delete(ss.all, s)
	ss.mu.Unlock()
}

// each calls fn for every stream, outside the set's lock (fn may close one), listing them in buf;
// it returns buf for the caller's next call.
func (ss *streamSet) each(buf []*stream, fn func(*stream)) []*stream {
	ss.mu.Lock()
	buf = buf[:0]
	for s := range ss.all {
		buf = append(buf, s)
	}
	ss.mu.Unlock()
	for _, s := range buf {
		fn(s)
	}
	clear(buf) // holds no closed stream until the next call
	return buf
}

// Stream feeds a view with frames (op/extimage): decoded video, another GPU device's renders,
// pixels in memory. Make it with Backend.NewStream; draw it in a View with DrawStream.
type Stream struct{ s stream }

// NewStream is a stream of frames drawn in this window.
func (b *Backend) NewStream() *Stream {
	st := &Stream{}
	st.s.init(b)
	return st
}

// Push hands f to the window, which takes over the producer's reference: do not use f after.
// A frame the window draws no tuple of (FrameCaps) completes Unsupported at once.
func (st *Stream) Push(f *Frame) *Receipt { return st.push(f, nil) }

func (st *Stream) push(f *Frame, then func(Outcome)) *Receipt {
	if caps, ok := st.s.b.FrameCaps(); ok {
		if _, match := caps.Match(f.Desc()); !match {
			f.Drop()
			st.s.setErr(extimage.ErrUnsupported)
			return receiptOf(Unsupported)
		}
	}
	return st.s.push(&framePayload{f: f, s: &st.s}, then)
}

// Close releases the stream's frames; a pending push completes Closed.
func (st *Stream) Close() { st.s.close() }

// Err is the last import error the window reported for this stream's frames, nil after a
// successful import. ErrUnsupported means: read FrameCaps again and pick another tuple.
func (st *Stream) Err() error { return st.s.lastErr() }

// framePayload is a frame and the stream that reports its imports.
type framePayload struct {
	f *Frame
	s *stream
}

func (p *framePayload) add(ops *op.Ops, dst Rect, report func(error)) {
	size := p.f.Desc().Size
	if size.X <= 0 || size.Y <= 0 {
		return
	}
	tr := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(dst.W/float32(size.X), dst.H/float32(size.Y))).
		Offset(f32.Pt(dst.X, dst.Y))).Push(ops)
	extimage.NewImageOp(p.f, report).Add(ops)
	paint.PaintOp{}.Add(ops)
	tr.Pop()
}

func (p *framePayload) drop() { p.f.Drop() }

// FrameCaps is what this window draws through streams, and how; false before the window has a
// GPU, or after it lost one. Safe on any goroutine. A new Caps.Gen means the device changed.
func (b *Backend) FrameCaps() (extimage.Caps, bool) {
	if b.win == nil {
		return extimage.Caps{}, false
	}
	return b.win.ExternalCaps()
}

// invalidate asks the window for a frame; safe on any goroutine and before the window exists.
func (b *Backend) invalidate() {
	if b.win != nil {
		b.win.Invalidate()
	}
}

// streamSlot is where a frame draws a stream: the canvas's clip, opacity and transform when
// DrawStream ran, and the destination rectangle in the canvas's coordinates.
type streamSlot struct {
	s     *stream
	clip  image.Rectangle
	alpha float32
	xform f32.Affine2D
	dst   Rect
}

// add draws the stream's current picture into the slot. Gio goroutine, during present.
func (sl streamSlot) add(ops *op.Ops) {
	p := sl.s.current()
	if p == nil {
		return
	}
	cl := clip.Rect(sl.clip).Push(ops)
	defer cl.Pop()
	if sl.alpha < 1 {
		defer paint.PushOpacity(ops, sl.alpha).Pop()
	}
	defer op.Affine(sl.xform).Push(ops).Pop()
	p.add(ops, sl.dst, sl.s.report)
}

// streamDrawer is the optional Canvas capability DrawStream needs: the window's own canvas.
type streamDrawer interface {
	drawStream(s *stream, dst Rect) bool
}

// DrawStream records a slot in c where the window draws st's newest frame, scaled into dst, at
// every vsync after a push, with no tui flush. false: c can't (a recording or test canvas), or
// st belongs to another window; draw a placeholder.
func DrawStream(c Canvas, st *Stream, dst Rect) bool {
	d, ok := c.(streamDrawer)
	return ok && st != nil && d.drawStream(&st.s, dst)
}

// streamsPresented settles every stream after a window frame was handed to the toolkit.
func (b *Backend) streamsPresented(now time.Time) {
	b.presentList = b.streams.each(b.presentList, func(s *stream) { s.presented(now) })
}

// closeStreams closes every stream of a stopping window.
func (b *Backend) closeStreams() {
	b.streams.each(nil, (*stream).close)
}
