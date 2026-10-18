package gui

import (
	"image"
	"image/draw"
	"sync"
	"sync/atomic"

	"github.com/yongjohnlee80/gio/op/extimage"
)

// MutableImage is an RGBA image an app edits in place, a paint canvas: each Update uploads the
// rectangle it changed, not the image. Draw it in a View with DrawMutableImage.
//
// An Update never writes memory a pushed frame can still read. The image keeps a few full
// buffers, each leased to the frames that point at it; an Update writes into a buffer no frame
// holds, after bringing it up to date by copying only the damage it missed. Its frame's Base is
// the newest revision a receipt saw drawn and its Dirty all damage since, so a revision that was
// superseded before it was drawn still reaches the screen.
type MutableImage struct {
	st   *Stream
	src  extimage.SourceID
	size image.Point

	mu     sync.Mutex
	bufs   []*imageBuf
	rev    uint64        // the newest revision
	damage []damage      // what each revision after the oldest a buffer or the screen holds changed
	drawn  atomic.Uint64 // the newest revision a receipt saw Drawn
	closed bool
}

// imageBuf is one full copy of the image at revision rev. leases counts the frames that point at
// it; it is written only at zero.
type imageBuf struct {
	pix    []byte
	rev    uint64
	leases atomic.Int32
}

type damage struct {
	rev uint64
	r   image.Rectangle
}

// maxIdleBufs is how many buffers an image keeps once their frames are gone.
const maxIdleBufs = 3

// NewMutableImage is a size image, transparent, drawn in this window.
func (b *Backend) NewMutableImage(size image.Point) *MutableImage {
	m := &MutableImage{st: b.NewStream(), src: extimage.NewSource(), size: size}
	m.bufs = []*imageBuf{{pix: make([]byte, size.X*size.Y*4)}}
	return m
}

// Size is the image's size in pixels.
func (m *MutableImage) Size() image.Point { return m.size }

// Update copies r of src (in the image's coordinates) into the image and pushes the revision.
// Updates are serialized; any goroutine.
func (m *MutableImage) Update(r image.Rectangle, src image.Image) *Receipt {
	r = r.Intersect(image.Rectangle{Max: m.size})
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return receiptOf(Closed)
	}
	cur := m.current()
	buf := m.freeBuf(cur)
	// Bring buf up to cur's revision: copy what changed since buf's own, from cur.
	if buf != cur {
		for _, d := range m.damage {
			if d.rev > buf.rev {
				copyRect(buf.pix, cur.pix, d.r, m.size.X)
			}
		}
	}
	if !r.Empty() {
		dst := &image.RGBA{Pix: buf.pix, Stride: m.size.X * 4, Rect: image.Rectangle{Max: m.size}}
		draw.Draw(dst, r, src, r.Min, draw.Src)
	}
	m.rev++
	buf.rev = m.rev
	m.damage = append(m.damage, damage{rev: m.rev, r: r})
	base := m.drawn.Load()
	dirty := image.Rectangle{}
	for _, d := range m.damage {
		if d.rev > base {
			dirty = dirty.Union(d.r)
		}
	}
	if base == 0 {
		dirty = image.Rectangle{} // nothing drawn yet: the whole image
	}
	m.prune()
	rev := m.rev
	buf.leases.Add(1)
	// Publication is inside the lock, so revisions reach the stream in the order they were
	// assigned. Nothing push calls back takes m.mu: a lease drop and the Drawn follow-up are
	// atomic.
	defer m.mu.Unlock()
	f, err := extimage.NewFrame(extimage.Desc{
		Kind: extimage.KindMemory, Format: extimage.FormatRGBA8, Size: m.size,
		Planes: []extimage.Plane{{Data: buf.pix, Pitch: uint32(m.size.X * 4)}},
		Source: m.src, Rev: rev, Base: base, Dirty: dirty,
	}, func() { buf.leases.Add(-1) })
	if err != nil {
		buf.leases.Add(-1)
		return receiptOf(Unsupported)
	}
	if testHookPublish != nil {
		testHookPublish(m)
	}
	return m.st.push(f, func(o Outcome) {
		if o != Drawn {
			return
		}
		for { // the newest drawn revision only rises
			old := m.drawn.Load()
			if rev <= old || m.drawn.CompareAndSwap(old, rev) {
				return
			}
		}
	})
}

// testHookPublish, set only by tests, runs as Update publishes, with m.mu held.
var testHookPublish func(*MutableImage)

// current is the buffer at the newest revision. m.mu held.
func (m *MutableImage) current() *imageBuf {
	cur := m.bufs[0]
	for _, b := range m.bufs {
		if b.rev > cur.rev {
			cur = b
		}
	}
	return cur
}

// freeBuf is a buffer no frame holds: cur itself when free, else another, else a new copy of cur.
// m.mu held.
func (m *MutableImage) freeBuf(cur *imageBuf) *imageBuf {
	if cur.leases.Load() == 0 {
		return cur
	}
	for _, b := range m.bufs {
		if b != cur && b.leases.Load() == 0 {
			return b
		}
	}
	nb := &imageBuf{pix: append([]byte(nil), cur.pix...), rev: cur.rev}
	m.bufs = append(m.bufs, nb)
	return nb
}

// prune forgets damage every buffer and the screen already have, and buffers past the idle cap
// that no frame holds. m.mu held.
func (m *MutableImage) prune() {
	oldest := m.drawn.Load()
	for _, b := range m.bufs {
		oldest = min(oldest, b.rev)
	}
	keep := m.damage[:0]
	for _, d := range m.damage {
		if d.rev > oldest {
			keep = append(keep, d)
		}
	}
	m.damage = keep
	if len(m.bufs) > maxIdleBufs {
		cur := m.current()
		kept := m.bufs[:0]
		for _, b := range m.bufs {
			if b == cur || b.leases.Load() > 0 || len(kept) < maxIdleBufs {
				kept = append(kept, b)
			}
		}
		m.bufs = kept
	}
}

// Close ends the image: a pending update completes Closed. The image lets its buffers go: one a
// frame still leases lives as long as that frame, untouched, and the rest at once, though the
// closed handle is kept. An Update after Close completes Closed and writes nothing.
func (m *MutableImage) Close() {
	m.mu.Lock()
	m.closed = true
	m.bufs, m.damage = nil, nil
	m.mu.Unlock()
	m.st.Close()
}

// DrawMutableImage records a slot in c where the window draws m's newest revision, scaled into
// dst. false: c can't, or m belongs to another window.
func DrawMutableImage(c Canvas, m *MutableImage, dst Rect) bool { return DrawStream(c, m.st, dst) }

// copyRect copies rectangle r between two RGBA buffers of the same width.
func copyRect(dst, src []byte, r image.Rectangle, width int) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i, j := (y*width+r.Min.X)*4, (y*width+r.Max.X)*4
		copy(dst[i:j], src[i:j])
	}
}
