package main

import (
	"image"
	"sync"
)

// targetPool is a GPU producer's render targets, and the device's lifetime with them. Each target
// is made for one size. It is free for the next render once the window releases the frame over
// it, and stale once the size moves on. The device is freed once: after close, when the last
// target is freed.
//
// render (take, add) runs on one goroutine; a frame's release (put) may run on any.
type targetPool[T any] struct {
	sizeOf     func(T) image.Point
	freeTarget func(T)
	freeDevice func()

	mu          sync.Mutex
	free        []T // targets no frame holds
	stale       []T // released targets of another size, freed by take
	size        image.Point
	live        int  // targets made and not yet freed
	closed      bool // close ran: a target released after it is freed
	deviceFreed bool // the device's free was claimed, by whoever saw the last target go
}

// take is a free target of size, if any. Targets of another size are freed here, on the render
// goroutine, never in a frame's release.
func (p *targetPool[T]) take(size image.Point) (T, bool) {
	p.mu.Lock()
	if size != p.size {
		p.stale = append(p.stale, p.free...)
		p.free, p.size = nil, size
	}
	stale := p.stale
	p.stale = nil
	var tg T
	ok := false
	if n := len(p.free); n > 0 {
		tg, ok = p.free[n-1], true
		p.free = p.free[:n-1]
	}
	p.mu.Unlock()
	p.release(stale)
	return tg, ok
}

// add counts a target the producer just made.
func (p *targetPool[T]) add() {
	p.mu.Lock()
	p.live++
	p.mu.Unlock()
}

// put is a frame's release: the target is free again, or stale if the size moved on, or freed
// once the pool is closed.
func (p *targetPool[T]) put(tg T) {
	p.mu.Lock()
	switch {
	case p.closed:
		p.mu.Unlock()
		p.release([]T{tg})
		return
	case p.sizeOf(tg) == p.size:
		p.free = append(p.free, tg)
	default:
		p.stale = append(p.stale, tg)
	}
	p.mu.Unlock()
}

// close frees every target no frame holds; a target the window still holds is freed as its frame
// is released, and the last one frees the device. Call it once, after the last render.
func (p *targetPool[T]) close() { p.release(p.closeTake()) }

// closeTake marks the pool closed and takes the targets no frame holds, to be freed.
func (p *targetPool[T]) closeTake() []T {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	all := append(p.free, p.stale...)
	p.free, p.stale = nil, nil
	return all
}

// release frees ts, then counts them gone. The device is freed by the one call that claims it
// under the mutex, once the pool is closed and no target is left, however the calls interleave,
// an empty batch included.
func (p *targetPool[T]) release(ts []T) {
	for _, tg := range ts {
		p.freeTarget(tg)
	}
	p.mu.Lock()
	p.live -= len(ts)
	claim := p.closed && p.live == 0 && !p.deviceFreed
	if claim {
		p.deviceFreed = true
	}
	p.mu.Unlock()
	if claim {
		p.freeDevice()
	}
}
