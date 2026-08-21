package web

import (
	"sync"

	"github.com/yongjohnlee80/golib/tui"
)

// cursorState is the latched cursor, emitted with the next frame rather than
// immediately (ADR-0009 §2.2).
type cursorState struct {
	Visible bool
	X, Y    int
	Shape   tui.CursorShape
}

// Frame is one atomic screen update.
//
// A frame reaches the client whole or not at all: a half-applied frame is a
// half-painted screen, and the client has no way to know it is looking at one.
type Frame struct {
	// Rev increases monotonically. The client echoes it to acknowledge.
	Rev uint64

	// Full marks a resync snapshot: every cell, not a diff. A new connection or
	// any gap gets one, because a diff is only meaningful against a baseline the
	// client actually holds.
	Full bool

	// W and H are the grid dimensions this frame describes. Carried on every
	// frame so a client can never apply a diff against the wrong shape.
	W, H int

	// Updates are the changed cells, row-major.
	Updates []tui.CellUpdate

	// Cursor is the latched cursor state as of this frame.
	Cursor cursorState
}

// framer owns the two grids and produces frames.
//
// # Why two grids and not a merged diff list
//
// The pending frame must be the diff of the CURRENT server grid against the last
// baseline the client ACKNOWLEDGED (§2.4). Rev 0 of the ADR said a newer frame
// replaces an older one, which is a correctness bug rather than an
// optimisation: frames carry only dirty cells, so if the frame containing row A
// is dropped and the next frame changes only row B, a replacement carries B
// alone and row A never reaches the client again — the client diverges
// permanently with no way to notice.
//
// Holding an acknowledged baseline and recomputing the diff at publish time
// makes the aggregate cumulative BY CONSTRUCTION. There is nothing to merge and
// nothing to forget, and one slot is provably sufficient because the slot holds
// a dirty bit rather than a payload.
type framer struct {
	mu sync.Mutex

	current *grid // server truth
	acked   *grid // last baseline the client confirmed
	sent    *grid // grid as of the frame currently in flight, nil if none

	rev     uint64 // last emitted revision
	sentRev uint64 // revision in flight, 0 if none

	dirty    bool // content changed since the last emitted frame
	needFull bool // next frame must be a full snapshot

	cursor      cursorState
	sentCursor  cursorState
	ackedCursor cursorState

	// coalesced counts publications that were folded into a later frame. It is
	// reported rather than discarded: silently swallowing one is a debugging
	// trap (§2.4).
	coalesced uint64
}

func newFramer(w, h int) *framer {
	f := &framer{
		current:  newGrid(w, h),
		acked:    newGrid(w, h),
		needFull: true, // no client has acknowledged anything yet
	}
	return f
}

// publish applies a diff to the server grid and marks a frame pending.
//
// It never blocks on network I/O and never waits for a client: the App loop
// calls this once per frame and ADR-0003's one-write rule assumes it is fast
// (§2.4).
func (f *framer) publish(updates []tui.CellUpdate, cursor cursorState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current.apply(updates)
	f.cursor = cursor
	if f.dirty {
		// A frame was already pending, so this publication folds into it. That
		// is not a dropped update — the pending frame is recomputed from the
		// grid — but it IS an observation worth counting.
		f.coalesced++
	}
	f.dirty = true
}

// resize reshapes the server grid and forces the next frame to be a full
// snapshot, since a diff cannot cross a shape change.
func (f *framer) resize(w, h int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if w == f.current.w && h == f.current.h {
		return
	}
	f.current.resize(w, h)
	f.acked.resize(w, h)
	f.needFull = true
	f.dirty = true
}

// size reports the current grid dimensions.
func (f *framer) size() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current.w, f.current.h
}

// pending reports whether a frame is available to send.
func (f *framer) pending() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dirty || f.cursor != f.ackedCursor
}

// next produces the frame to send, or ok=false when there is nothing to send or
// a frame is already in flight.
//
// Only ONE frame is in flight at a time. That is what keeps the memory bound at
// two grids plus one snapshot: with a queue, each unacknowledged frame would
// need its own baseline snapshot to interpret a later acknowledgement. Waiting
// costs one round trip per frame under saturation, and under saturation the
// cumulative aggregate means the next frame carries everything that accumulated
// meanwhile — which is the intended behavior, not a compromise.
func (f *framer) next() (Frame, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sentRev != 0 {
		return Frame{}, false // awaiting acknowledgement
	}
	if !f.dirty && f.cursor == f.ackedCursor {
		return Frame{}, false
	}

	f.rev++
	fr := Frame{Rev: f.rev, W: f.current.w, H: f.current.h, Cursor: f.cursor}
	if f.needFull || !f.current.sameShape(f.acked) {
		fr.Full = true
		fr.Updates = f.current.snapshot()
	} else {
		fr.Updates = f.current.diff(f.acked)
	}
	f.sent = f.current.clone()
	f.sentCursor = f.cursor
	f.sentRev = f.rev
	f.dirty = false
	return fr, true
}

// ack advances the acknowledged baseline.
//
// An acknowledgement for anything other than the frame in flight is IGNORED. A
// stale or forged revision must not move the baseline forward: the baseline is
// the one thing that decides what the next diff omits, so believing a wrong ack
// is exactly how a client is left permanently missing a cell.
func (f *framer) ack(rev uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sentRev == 0 || rev != f.sentRev {
		return
	}
	f.acked = f.sent
	f.ackedCursor = f.sentCursor
	f.sent = nil
	f.sentRev = 0
	f.needFull = false
}

// reset abandons any in-flight frame and forces a full resync.
//
// Called when a client disconnects or reconnects. The acknowledged baseline is
// discarded rather than kept: the new client has seen nothing, and a diff
// against what the PREVIOUS client acknowledged would leave it permanently
// missing every cell that predates it.
func (f *framer) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = nil
	f.sentRev = 0
	f.acked = newGrid(f.current.w, f.current.h)
	f.ackedCursor = cursorState{}
	f.needFull = true
	f.dirty = true
}

// coalescedCount reports how many publications folded into a later frame.
func (f *framer) coalescedCount() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.coalesced
}

// serverGrid returns a copy of the server's truth, for tests and diagnostics.
func (f *framer) serverGrid() *grid {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current.clone()
}
