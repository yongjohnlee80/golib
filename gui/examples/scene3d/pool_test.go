package main

import (
	"image"
	"sync"
	"testing"
)

// fakeTarget is a target the pool test frees by name.
type fakeTarget struct {
	name string
	size image.Point
}

type poolLog struct {
	mu      sync.Mutex
	targets []string
	devices int
}

func newFakePool() (*targetPool[*fakeTarget], *poolLog) {
	l := &poolLog{}
	p := &targetPool[*fakeTarget]{
		sizeOf: func(t *fakeTarget) image.Point { return t.size },
		freeTarget: func(t *fakeTarget) {
			l.mu.Lock()
			l.targets = append(l.targets, t.name)
			l.mu.Unlock()
		},
		freeDevice: func() {
			l.mu.Lock()
			l.devices++
			l.mu.Unlock()
		},
	}
	return p, l
}

// The ordered interleaving that freed the device twice: close takes an empty batch while one
// target is leased; the target's last release frees it and the device; close's empty batch then
// finishes. The device is freed once.
func TestTheLastReleaseAndAnEmptyCloseFreeTheDeviceOnce(t *testing.T) {
	p, l := newFakePool()
	size := image.Pt(4, 4)
	p.take(size)
	held := &fakeTarget{name: "held", size: size}
	p.add() // made, and leased to a frame the window holds
	batch := p.closeTake()
	if len(batch) != 0 {
		t.Fatalf("close took %d targets; want none (the only one is leased)", len(batch))
	}
	p.put(held) // the frame's release, after close marked the pool
	if l.devices != 1 || len(l.targets) != 1 {
		t.Fatalf("after the last release: device freed %d times, targets %v; want once, [held]", l.devices, l.targets)
	}
	p.release(batch) // close resumes with its empty batch
	if l.devices != 1 {
		t.Fatalf("the device was freed %d times; want exactly once", l.devices)
	}
}

// A pool that never made a target frees its device on close, once.
func TestAnEmptyPoolFreesItsDeviceOnClose(t *testing.T) {
	p, l := newFakePool()
	p.close()
	if l.devices != 1 {
		t.Fatalf("device freed %d times; want once", l.devices)
	}
}

// close frees the free and stale targets but not the leased one; the leased one's release frees
// it and then the device, which no target outlives.
func TestCloseFreesWhatNoFrameHoldsAndTheLastReleaseTheDevice(t *testing.T) {
	p, l := newFakePool()
	a, b := image.Pt(4, 4), image.Pt(8, 8)
	p.take(a)
	free := &fakeTarget{name: "free", size: a}
	stale := &fakeTarget{name: "stale", size: a}
	held := &fakeTarget{name: "held", size: b}
	p.add()
	p.add()
	p.add()
	p.put(stale) // released at size a ...
	p.take(b)    // ... the size moved on: freed here, by take
	p.put(free)  // released after the move: stale too
	p.close()
	if l.devices != 0 {
		t.Fatal("the device was freed while a frame still holds a target")
	}
	p.put(held)
	if l.devices != 1 || len(l.targets) != 3 {
		t.Fatalf("device %d, targets %v; want once, all three", l.devices, l.targets)
	}
}

// Releases racing close free every target once and the device once.
func TestConcurrentReleasesAndCloseFreeEverythingOnce(t *testing.T) {
	for range 200 {
		p, l := newFakePool()
		size := image.Pt(2, 2)
		p.take(size)
		held := make([]*fakeTarget, 8)
		for i := range held {
			held[i] = &fakeTarget{name: string(rune('a' + i)), size: size}
			p.add()
		}
		var wg sync.WaitGroup
		for _, tg := range held {
			wg.Add(1)
			go func() { defer wg.Done(); p.put(tg) }()
		}
		wg.Add(1)
		go func() { defer wg.Done(); p.close() }()
		wg.Wait()
		if l.devices != 1 || len(l.targets) != len(held) {
			t.Fatalf("device %d, targets %d; want 1 and %d", l.devices, len(l.targets), len(held))
		}
	}
}
