package gui

import (
	"context"
	"image"
	"image/color"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/gio/app"
	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/gpu/headless"
	"github.com/yongjohnlee80/gio/io/input"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/extimage"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/golib/tui"
)

// memFrame is a w×h memory frame of one colour whose releases are counted.
func memFrame(t *testing.T, w, h int, c [4]byte, released *int) *Frame {
	t.Helper()
	data := make([]byte, w*h*4)
	for i := 0; i < len(data); i += 4 {
		copy(data[i:], c[:])
	}
	f, err := extimage.NewFrame(extimage.Desc{Kind: extimage.KindMemory, Format: extimage.FormatRGBA8, Size: image.Pt(w, h),
		Planes: []extimage.Plane{{Data: data, Pitch: uint32(w * 4)}}}, func() { *released++ })
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// presentOnce is one window frame's handling of s: drawn into a slot when drawn is true, then
// settled as present settles it.
func presentOnce(s *stream, drawn bool, now time.Time) {
	if drawn {
		s.selection()
	}
	s.b.streamsPresented(now)
}

// pendingOf is s's pending picture, peeked without a window frame selecting it.
func pendingOf(s *stream) payload {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending
}

func TestAReceiptCompletedBeforeItsWaitIsNotMissed(t *testing.T) {
	b := NewBackend()
	st := b.NewStream()
	var rel int
	r := st.Push(memFrame(t, 2, 2, [4]byte{1, 2, 3, 255}, &rel))
	now := time.Unix(100, 0)
	presentOnce(&st.s, true, now) // completes before anyone waits
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	o, err := r.Wait(ctx)
	if err != nil || o != Drawn {
		t.Fatalf("outcome %v %v; want Drawn", o, err)
	}
	if !r.When().Equal(now) {
		t.Fatalf("drawn at %v; want the frame's time %v", r.When(), now)
	}
}

// A push replaced before it is drawn is Superseded and released once; the newer one is drawn.
// The shown frame is released when the next one is shown; every frame exactly once in all.
func TestPushesSupersedeAndEveryFrameIsReleasedOnce(t *testing.T) {
	b := NewBackend()
	st := b.NewStream()
	var ra, rb, rc int
	a := st.Push(memFrame(t, 2, 2, [4]byte{1, 0, 0, 255}, &ra))
	bb := st.Push(memFrame(t, 2, 2, [4]byte{2, 0, 0, 255}, &rb))
	if a.Outcome() != Superseded || ra != 1 {
		t.Fatalf("A: %v, released %d; want Superseded and released once", a.Outcome(), ra)
	}
	presentOnce(&st.s, true, time.Now())
	if bb.Outcome() != Drawn || rb != 0 {
		t.Fatalf("B: %v, released %d; want Drawn and still shown", bb.Outcome(), rb)
	}
	c := st.Push(memFrame(t, 2, 2, [4]byte{3, 0, 0, 255}, &rc))
	presentOnce(&st.s, true, time.Now())
	if c.Outcome() != Drawn || rb != 1 || rc != 0 {
		t.Fatalf("C %v; B released %d, C %d; want B released once when C was shown", c.Outcome(), rb, rc)
	}
	st.Close()
	if ra != 1 || rb != 1 || rc != 1 {
		t.Fatalf("releases after Close: %d %d %d; want 1 each", ra, rb, rc)
	}
}

// A hidden view's push is NotShown and stays pending; drawn later, it is shown.
func TestAPushNoSlotDrewIsNotShownAndStaysPending(t *testing.T) {
	b := NewBackend()
	st := b.NewStream()
	var rel int
	r := st.Push(memFrame(t, 2, 2, [4]byte{1, 2, 3, 255}, &rel))
	presentOnce(&st.s, false, time.Now())
	if r.Outcome() != NotShown || rel != 0 {
		t.Fatalf("hidden push: %v, released %d; want NotShown, kept", r.Outcome(), rel)
	}
	if pendingOf(&st.s) == nil {
		t.Fatal("the hidden push is no longer pending")
	}
}

func TestClosingAStreamReleasesItsFramesAndRefusesPushes(t *testing.T) {
	b := NewBackend()
	st := b.NewStream()
	var r1, r2 int
	p := st.Push(memFrame(t, 2, 2, [4]byte{1, 2, 3, 255}, &r1))
	st.Close()
	if p.Outcome() != Closed || r1 != 1 {
		t.Fatalf("pending at Close: %v, released %d; want Closed, released", p.Outcome(), r1)
	}
	late := st.Push(memFrame(t, 2, 2, [4]byte{1, 2, 3, 255}, &r2))
	if late.Outcome() != Closed || r2 != 1 {
		t.Fatalf("push after Close: %v, released %d; want Closed, released", late.Outcome(), r2)
	}
}

// One producer at a time: a concurrent Push and a re-entrant one are Busy and do not draw.
func TestCanvasStreamPushesAreOneAtATime(t *testing.T) {
	b := NewBackend()
	cs := b.NewCanvasStream()
	var inner *Receipt
	r := cs.Push(Size{W: 10, H: 10}, func(c Canvas) {
		inner = cs.Push(Size{W: 10, H: 10}, func(Canvas) { t.Error("a re-entrant draw ran") })
	})
	if inner.Outcome() != Busy {
		t.Fatalf("re-entrant push: %v; want Busy", inner.Outcome())
	}
	if r.Outcome() != 0 {
		t.Fatalf("the outer push completed %v before any frame", r.Outcome())
	}
	started, release := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cs.Push(Size{W: 10, H: 10}, func(Canvas) { close(started); <-release })
	}()
	<-started
	if o := cs.Push(Size{W: 10, H: 10}, func(Canvas) { t.Error("a concurrent draw ran") }).Outcome(); o != Busy {
		t.Fatalf("concurrent push: %v; want Busy", o)
	}
	cs.Close() // during the draw: does not wait for it
	close(release)
	wg.Wait()
}

// A View's DrawStream slot is drawn by the window in paint order: over what the view drew
// before it, under what it drew after.
func TestAStreamSlotIsDrawnInPaintOrder(t *testing.T) {
	m := metrics{window: image.Pt(64, 64), cell: image.Pt(8, 16), ppem: 14, baseline: 12, scale: 1, grid: tui.Size{W: 8, H: 4}}
	hw, err := headless.NewWindow(64, 64)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	b := NewBackend()
	b.render = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	st := b.NewStream()
	var rel int
	st.Push(memFrame(t, 4, 4, [4]byte{0, 0xc0, 0, 255}, &rel)) // green
	red, blue := color.NRGBA{R: 0xc0, A: 255}, color.NRGBA{B: 0xc0, A: 255}
	view := ViewFunc(func(c Canvas) {
		c.FillRect(Rect{W: 64, H: 64}, Brush{Color: red})
		if !DrawStream(c, st, Rect{X: 0, Y: 0, W: 32, H: 32}) {
			t.Error("DrawStream refused the window's canvas")
		}
		c.FillRect(Rect{X: 0, Y: 0, W: 8, H: 8}, Brush{Color: blue}) // after the slot: on top
	})
	g := &grid{}
	g.resize(8, 4)
	b.render.m = m
	draw, parts := b.render.frameParts(g, m, cursorState{}, []tui.NativePlacement{{View: view, X: 0, Y: 0, Cols: 8, Rows: 4}}, nil, true)
	if len(parts) != 1 {
		t.Fatalf("%d slots recorded; want 1", len(parts))
	}
	ops := new(op.Ops)
	draw.Add(ops)
	for _, p := range parts {
		p.slot.add(ops)
		p.seg.Add(ops)
	}
	if err := hw.Frame(ops); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	if err := hw.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	at := func(x, y int) color.RGBA { return img.RGBAAt(x, y) }
	if c := at(20, 20); c.G < 0xa0 || c.R > 0x20 {
		t.Errorf("(20,20) %v; want the stream's green over the red drawn before it", c)
	}
	if c := at(4, 4); c.B < 0xa0 || c.G > 0x20 {
		t.Errorf("(4,4) %v; want the blue drawn after the slot, on top", c)
	}
	if c := at(50, 50); c.R < 0xa0 {
		t.Errorf("(50,50) %v; want the red outside the slot", c)
	}
}

// present puts a stream's newest frame in its slot, draws the rest around it, and settles the
// stream after the toolkit's Frame: the receipt is Drawn at the FrameEvent's time.
func TestPresentDrawsTheSlotAndSettlesTheStream(t *testing.T) {
	hw, err := headless.NewWindow(64, 64)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	m := metrics{window: image.Pt(64, 64), cell: image.Pt(8, 16), ppem: 14, baseline: 12, scale: 1, grid: tui.Size{W: 8, H: 4}}
	b := NewBackend()
	b.render = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	st := b.NewStream()
	var rel int
	r := st.Push(memFrame(t, 4, 4, [4]byte{0, 0xc0, 0, 255}, &rel))
	view := ViewFunc(func(c Canvas) { DrawStream(c, st, Rect{W: 32, H: 32}) })
	g := &grid{}
	g.resize(8, 4)
	b.render.m = m
	draw, parts := b.render.frameParts(g, m, cursorState{}, []tui.NativePlacement{{View: view, Cols: 8, Rows: 4}}, nil, true)
	b.latest = &frame{draw: draw, parts: parts, m: m}
	var captured *op.Ops
	now := time.Unix(200, 0)
	router := new(input.Router)
	b.present(app.FrameEvent{Now: now, Size: image.Pt(64, 64), Source: router.Source(), Frame: func(o *op.Ops) { captured = o }}, &b.gio, m, b.font.Load())
	if captured == nil {
		t.Fatal("present did not hand the toolkit a frame")
	}
	if err := hw.Frame(captured); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	if err := hw.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	if c := img.RGBAAt(10, 10); c.G < 0xa0 || c.R > 0x20 {
		t.Errorf("(10,10) %v; want the stream's green in its slot", c)
	}
	// Outcome never blocks: a stream that was never settled fails here, it does not hang.
	if o := r.Outcome(); o != Drawn {
		t.Fatalf("receipt %v; want Drawn", o)
	}
	if !r.When().Equal(now) {
		t.Fatalf("drawn at %v; want the frame's time %v", r.When(), now)
	}
}

// A window frame that draws a shown stream picture again, with no new push, allocates nothing:
// a game's or a player's vsync is garbage-free on the window's side.
func TestARedrawnStreamFrameAllocatesNothing(t *testing.T) {
	m := metrics{window: image.Pt(64, 64), cell: image.Pt(8, 16), ppem: 14, baseline: 12, scale: 1, grid: tui.Size{W: 8, H: 4}}
	b := NewBackend()
	b.render = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	st := b.NewStream()
	defer st.Close()
	var rel int
	st.Push(memFrame(t, 4, 4, [4]byte{0, 0xc0, 0, 255}, &rel))
	view := ViewFunc(func(c Canvas) { DrawStream(c, st, Rect{W: 32, H: 32}) })
	g := &grid{}
	g.resize(8, 4)
	draw, parts := b.render.frameParts(g, m, cursorState{}, []tui.NativePlacement{{View: view, Cols: 8, Rows: 4}}, nil, true)
	ops := new(op.Ops)
	vsync := func() {
		ops.Reset()
		draw.Add(ops)
		for _, p := range parts {
			p.slot.add(ops)
			p.seg.Add(ops)
		}
		b.streamsPresented(time.Unix(1, 0))
	}
	vsync() // the push is drawn and becomes the shown picture; ops grows to its size
	if n := testing.AllocsPerRun(100, vsync); n != 0 {
		t.Fatalf("a redrawn stream frame allocated %v times; want 0", n)
	}
}

// presentRig is a Backend with one stream drawn by a view in two slots, and a headless window that
// draws what present hands the toolkit.
type presentRig struct {
	t   *testing.T
	b   *Backend
	st  *Stream
	hw  *headless.Window
	m   metrics
	img *image.RGBA
}

func newPresentRig(t *testing.T) *presentRig {
	t.Helper()
	hw, err := headless.NewWindow(64, 64)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	g := &presentRig{t: t, b: NewBackend(), hw: hw}
	t.Cleanup(g.release)
	g.m = metrics{window: image.Pt(64, 64), cell: image.Pt(8, 16), ppem: 14, baseline: 12, scale: 1, grid: tui.Size{W: 8, H: 4}}
	g.b.render = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	g.st = g.b.NewStream()
	view := ViewFunc(func(c Canvas) {
		DrawStream(c, g.st, Rect{W: 32, H: 32})
		DrawStream(c, g.st, Rect{X: 32, Y: 32, W: 32, H: 32})
	})
	grid := &grid{}
	grid.resize(8, 4)
	draw, parts := g.b.render.frameParts(grid, g.m, cursorState{}, []tui.NativePlacement{{View: view, Cols: 8, Rows: 4}}, nil, true)
	g.b.latest = &frame{draw: draw, parts: parts, m: g.m}
	return g
}

// present runs one real present at now; during calls back from inside the toolkit's Frame, after
// the window drew the frame's ops into the screenshot.
func (g *presentRig) present(now time.Time, during func()) {
	g.t.Helper()
	router := new(input.Router)
	g.b.present(app.FrameEvent{Now: now, Size: image.Pt(64, 64), Source: router.Source(), Frame: func(o *op.Ops) {
		if err := g.hw.Frame(o); err != nil {
			g.t.Fatal(err)
		}
		g.img = image.NewRGBA(image.Rect(0, 0, 64, 64))
		if err := g.hw.Screenshot(g.img); err != nil {
			g.t.Fatal(err)
		}
		if during != nil {
			during()
		}
	}}, &g.b.gio, g.m, g.b.font.Load())
}

// release releases the headless window, and with it its texture cache's references to the frames
// it imported: what still holds a frame after it is the stream. Idempotent.
func (g *presentRig) release() {
	if g.hw != nil {
		g.hw.Release()
		g.hw = nil
	}
}

// slots is the colour in each of the view's two slots.
func (g *presentRig) slots() (color.RGBA, color.RGBA) {
	return g.img.RGBAAt(10, 10), g.img.RGBAAt(50, 50)
}

func red(c color.RGBA) bool   { return c.R > 0xa0 && c.G < 0x20 }
func green(c color.RGBA) bool { return c.G > 0xa0 && c.R < 0x20 }

// A push while the toolkit draws a frame waits for the next one: the frame's picture is the one
// both slots drew, its receipt is Drawn at that frame's time, and the newer push stays pending
// until the next frame draws it.
func TestAPushDuringAPresentationWaitsForTheNextFrame(t *testing.T) {
	g := newPresentRig(t)
	var relA, relB int
	rA := g.st.Push(memFrame(t, 4, 4, [4]byte{0xc0, 0, 0, 255}, &relA))
	var rB *Receipt
	t1 := time.Unix(100, 0)
	g.present(t1, func() { rB = g.st.Push(memFrame(t, 4, 4, [4]byte{0, 0xc0, 0, 255}, &relB)) })
	if a, b := g.slots(); !red(a) || !red(b) {
		t.Fatalf("slots %v %v; want A (red) in both", a, b)
	}
	if o := rA.Outcome(); o != Drawn || !rA.When().Equal(t1) {
		t.Fatalf("A: %v at %v; want Drawn at %v (it was drawn)", o, rA.When(), t1)
	}
	if o := rB.Outcome(); o != 0 {
		t.Fatalf("B: %v; want pending (pushed during the frame)", o)
	}
	if relA != 0 {
		t.Fatalf("A released %d times while it is the shown picture", relA)
	}
	t2 := time.Unix(101, 0)
	g.present(t2, nil)
	if a, b := g.slots(); !green(a) || !green(b) {
		t.Fatalf("next frame's slots %v %v; want B (green) in both", a, b)
	}
	if o := rB.Outcome(); o != Drawn || !rB.When().Equal(t2) {
		t.Fatalf("B: %v at %v; want Drawn at %v", o, rB.When(), t2)
	}
	g.release()
	if relA != 1 || relB != 0 {
		t.Fatalf("released A %d, B %d; want A once (replaced), B kept (shown)", relA, relB)
	}
	g.st.Close()
	if relB != 1 {
		t.Fatalf("B released %d times after Close; want once", relB)
	}
}

// A Close while the toolkit draws a frame leaves the frame's picture leased until the frame was
// handed over: nothing it draws is released early, the drawn picture's receipt is Drawn, and each
// picture is released exactly once afterwards.
func TestAPresentationLeasesItsPictureAcrossAClose(t *testing.T) {
	for _, shown := range []bool{false, true} {
		g := newPresentRig(t)
		var rel int
		r := g.st.Push(memFrame(t, 4, 4, [4]byte{0xc0, 0, 0, 255}, &rel))
		if shown { // A is the shown picture, not the pending one, when the closing frame selects it
			g.present(time.Unix(99, 0), nil)
		}
		inFrame := -1
		g.present(time.Unix(100, 0), func() {
			g.st.Close()
			inFrame = rel
		})
		if a, b := g.slots(); !red(a) || !red(b) {
			t.Fatalf("shown=%v: slots %v %v; want A in both", shown, a, b)
		}
		if inFrame != 0 {
			t.Fatalf("shown=%v: A released %d times during the frame that draws it", shown, inFrame)
		}
		g.release()
		if rel != 1 {
			t.Fatalf("shown=%v: A released %d times after the frame; want exactly once", shown, rel)
		}
		if r.Outcome() != Drawn {
			t.Fatalf("shown=%v: A's receipt %v; want Drawn", shown, r.Outcome())
		}
		if len(g.b.presenting) != 0 {
			t.Fatalf("shown=%v: %d streams still listed for presentation", shown, len(g.b.presenting))
		}
	}
}

// Every slot of a frame draws the frame's selection, though a push lands between them.
func TestEverySlotOfAFrameDrawsOneSelection(t *testing.T) {
	b := NewBackend()
	st := b.NewStream()
	defer st.Close()
	var relA, relB int
	st.Push(memFrame(t, 2, 2, [4]byte{1, 0, 0, 255}, &relA))
	first := st.s.selection()
	st.Push(memFrame(t, 2, 2, [4]byte{0, 1, 0, 255}, &relB))
	if second := st.s.selection(); second != first {
		t.Fatal("a later slot of the frame drew the newer push")
	}
	b.streamsPresented(time.Now())
	if pendingOf(&st.s) == nil {
		t.Fatal("the push made during the frame is no longer pending")
	}
	if next := st.s.selection(); next == first {
		t.Fatal("the next frame did not select the newer push")
	}
	b.streamsPresented(time.Now())
}

// A Close between a frame's selection and its settling releases nothing the frame selected, with
// no GPU cache holding a reference to hide it: the frame's lease does. Each picture is released
// exactly once when the frame settles.
func TestASelectedPictureOutlivesACloseUntilItsFrameSettles(t *testing.T) {
	for _, shown := range []bool{false, true} {
		b := NewBackend()
		st := b.NewStream()
		var rel int
		r := st.Push(memFrame(t, 2, 2, [4]byte{1, 2, 3, 255}, &rel))
		if shown {
			presentOnce(&st.s, true, time.Now())
		}
		if st.s.selection() == nil {
			t.Fatalf("shown=%v: nothing selected", shown)
		}
		st.Close()
		if rel != 0 {
			t.Fatalf("shown=%v: a Close released the frame's selection %d times before the frame settled", shown, rel)
		}
		b.streamsPresented(time.Unix(5, 0))
		if rel != 1 {
			t.Fatalf("shown=%v: released %d times after the frame settled; want once", shown, rel)
		}
		if r.Outcome() != Drawn {
			t.Fatalf("shown=%v: receipt %v; want Drawn", shown, r.Outcome())
		}
	}
}
