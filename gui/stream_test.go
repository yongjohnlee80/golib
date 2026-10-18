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

// presentOnce is one window frame's handling of s: drawn into a slot when drawn is true.
func presentOnce(s *stream, drawn bool, now time.Time) {
	if drawn {
		s.current()
	}
	s.presented(now)
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
	if st.s.current() == nil {
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
