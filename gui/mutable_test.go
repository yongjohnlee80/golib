package gui

import (
	"bytes"
	"image"
	"image/color"
	"sync"
	"testing"
	"time"
)

func solid(c color.RGBA) *image.Uniform { return image.NewUniform(c) }

// pendingFrame is the frame m's stream would draw now.
func pendingFrame(t *testing.T, m *MutableImage) *Frame {
	t.Helper()
	p, ok := m.st.s.current().(*framePayload)
	if !ok {
		t.Fatal("nothing pending")
	}
	m.st.s.mu.Lock()
	m.st.s.drawn = false // a peek, not a draw
	m.st.s.mu.Unlock()
	return p.f
}

// A frame keeps the revision it was made of: an Update while it is held writes another buffer,
// even over the same pixels and while another goroutine reads the held frame.
func TestAnUpdateNeverWritesAFrameStillHeld(t *testing.T) {
	b := NewBackend()
	m := b.NewMutableImage(image.Pt(16, 16))
	m.Update(image.Rect(0, 0, 16, 16), solid(color.RGBA{R: 255, A: 255}))
	f := pendingFrame(t, m).Retain() // a reader holding revision 1 (as the CPU import would)
	want := append([]byte(nil), f.Desc().Planes[0].Data...)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // reads revision 1 while the next Update runs
		defer wg.Done()
		for range 200 {
			_ = bytes.Equal(f.Desc().Planes[0].Data, want)
		}
	}()
	m.Update(image.Rect(4, 4, 12, 12), solid(color.RGBA{B: 255, A: 255}))
	wg.Wait()
	if !bytes.Equal(f.Desc().Planes[0].Data, want) {
		t.Fatal("an Update wrote the buffer a held frame reads")
	}
	f.Drop()
}

// A drawn, B superseded before it was drawn, C pushed: C's frame is based on A and its dirty
// rectangle covers B's change as well as its own, and its pixels hold both.
func TestASupersededRevisionStillReachesTheScreen(t *testing.T) {
	b := NewBackend()
	m := b.NewMutableImage(image.Pt(16, 16))
	m.Update(image.Rect(0, 0, 16, 16), solid(color.RGBA{A: 255}))
	presentOnce(&m.st.s, true, time.Now()) // A drawn
	if m.drawn.Load() != 1 {
		t.Fatalf("drawn revision %d after A; want 1", m.drawn.Load())
	}
	rb := m.Update(image.Rect(0, 0, 2, 2), solid(color.RGBA{R: 255, A: 255}))
	m.Update(image.Rect(10, 10, 12, 12), solid(color.RGBA{G: 255, A: 255}))
	if rb.Outcome() != Superseded {
		t.Fatalf("B: %v; want Superseded", rb.Outcome())
	}
	d := pendingFrame(t, m).Desc()
	if d.Base != 1 || d.Rev != 3 {
		t.Fatalf("C is revision %d on base %d; want 3 on base 1", d.Rev, d.Base)
	}
	if !image.Rect(0, 0, 2, 2).In(d.Dirty) || !image.Rect(10, 10, 12, 12).In(d.Dirty) {
		t.Fatalf("C's dirty %v; want it to cover B's (0,0)-(2,2) and its own (10,10)-(12,12)", d.Dirty)
	}
	px := func(x, y int) []byte { i := (y*16 + x) * 4; return d.Planes[0].Data[i : i+4] }
	if px(1, 1)[0] != 255 || px(11, 11)[1] != 255 {
		t.Fatalf("C's pixels lack B's red (%v) or its own green (%v)", px(1, 1), px(11, 11))
	}
}

func TestClosingAMutableImageCompletesAndRefuses(t *testing.T) {
	b := NewBackend()
	m := b.NewMutableImage(image.Pt(8, 8))
	r := m.Update(image.Rect(0, 0, 8, 8), solid(color.RGBA{R: 255, A: 255}))
	m.Close()
	if r.Outcome() != Closed {
		t.Fatalf("pending update at Close: %v; want Closed", r.Outcome())
	}
	for _, buf := range m.bufs {
		if n := buf.leases.Load(); n != 0 {
			t.Fatalf("a buffer still leased %d times after Close", n)
		}
	}
	if o := m.Update(image.Rect(0, 0, 1, 1), solid(color.RGBA{A: 255})).Outcome(); o != Closed {
		t.Fatalf("Update after Close: %v; want Closed", o)
	}
}

// A free buffer older than the newest revision is brought up to date before it is written: the
// damage it missed is replayed into it, so its frame holds every earlier change.
func TestAReusedBufferCatchesUpOnTheDamageItMissed(t *testing.T) {
	b := NewBackend()
	m := b.NewMutableImage(image.Pt(16, 16))
	m.Update(image.Rect(0, 0, 16, 16), solid(color.RGBA{A: 255}))       // revision 1, in buffer 0
	held := pendingFrame(t, m).Retain()                                 // a reader holds buffer 0
	m.Update(image.Rect(0, 0, 2, 2), solid(color.RGBA{R: 255, A: 255})) // revision 2, in buffer 1 (0 is held)
	held.Drop()                                                         // buffer 0 is free again, still at revision 1
	if len(m.bufs) != 2 {
		t.Fatalf("fixture: %d buffers; want 2", len(m.bufs))
	}
	// Revision 2's frame is pending, so buffer 1 is leased: revision 3 reuses buffer 0, which
	// missed revision 2.
	m.Update(image.Rect(10, 10, 12, 12), solid(color.RGBA{G: 255, A: 255}))
	d := pendingFrame(t, m).Desc()
	px := func(x, y int) []byte { i := (y*16 + x) * 4; return d.Planes[0].Data[i : i+4] }
	if d.Rev != 3 || px(1, 1)[0] != 255 || px(11, 11)[1] != 255 {
		t.Fatalf("rev %d: (1,1) %v, (11,11) %v; want rev 2's red kept and rev 3's green", d.Rev, px(1, 1), px(11, 11))
	}
}
