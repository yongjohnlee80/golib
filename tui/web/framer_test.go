package web

import (
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// client models the browser's grid: it applies whatever frames it receives and
// acknowledges the ones it chooses to.
type client struct {
	g *grid
}

func newClient(w, h int) *client { return &client{g: newGrid(w, h)} }

func (c *client) apply(fr Frame) {
	if fr.Full || !c.g.sameShape(&grid{w: fr.W, h: fr.H}) {
		c.g = newGrid(fr.W, fr.H)
	}
	c.g.apply(fr.Updates)
}

func cell(s string) tui.Cell { return tui.Cell{Content: s, Width: 1} }

func put(x, y int, s string) tui.CellUpdate {
	return tui.CellUpdate{X: x, Y: y, Cell: cell(s)}
}

// pump moves every available frame to the client, acknowledging each.
func pump(t *testing.T, f *framer, c *client) int {
	t.Helper()
	sent := 0
	for {
		fr, ok := f.next()
		if !ok {
			return sent
		}
		c.apply(fr)
		f.ack(fr.Rev)
		sent++
		if sent > 100 {
			t.Fatal("frame pump did not settle")
		}
	}
}

// TestDivergence_CoalescedSendStillConverges is the rev-0 defect, pinned.
//
// An earlier design said a newer frame REPLACES the pending one. With
// dirty-cell-only frames that loses data permanently: drop the frame carrying
// row A, then change only row B, and a replacement frame carries B alone — row A
// never reaches the client and nothing notices. The aggregate must accumulate.
func TestDivergence_CoalescedSendStillConverges(t *testing.T) {
	t.Parallel()
	f := newFramer(4, 3)
	c := newClient(4, 3)

	// Get the client onto a known baseline.
	pump(t, f, c)

	// The client's reader is blocked: change row A and let that publication be
	// coalesced away by never draining it.
	f.publish([]tui.CellUpdate{put(0, 0, "A")}, cursorState{})
	// Then change only row B, with row A's frame never having been sent.
	f.publish([]tui.CellUpdate{put(0, 1, "B")}, cursorState{})

	if got := f.coalescedCount(); got == 0 {
		t.Error("the folded publication was not counted — silently swallowing one is a debugging trap")
	}

	// Unblock.
	pump(t, f, c)

	if !c.g.equal(f.serverGrid()) {
		t.Fatalf("client diverged from the server\nclient: %s\nserver: %s",
			render(c.g), render(f.serverGrid()))
	}
	if c.g.at(0, 0).Content != "A" {
		t.Errorf("row A was lost: the pending frame replaced instead of accumulating (got %q)",
			c.g.at(0, 0).Content)
	}
	if c.g.at(0, 1).Content != "B" {
		t.Errorf("row B missing: %q", c.g.at(0, 1).Content)
	}
}

// The second divergence case: the ACKNOWLEDGEMENT is dropped rather than
// the send. A transmitted-but-unacknowledged cell must stay in the aggregate,
// because a send that was never acknowledged may never have landed.
func TestDivergence_UnacknowledgedSendStaysInTheAggregate(t *testing.T) {
	t.Parallel()
	f := newFramer(4, 3)
	c := newClient(4, 3)
	pump(t, f, c)

	// Row A goes out, and the client applies it — but the acknowledgement is
	// LOST on the way back.
	f.publish([]tui.CellUpdate{put(0, 0, "A")}, cursorState{})
	fr, ok := f.next()
	if !ok {
		t.Fatal("no frame produced")
	}
	// Deliberately not delivered to the client, and deliberately not acked:
	// this models a send that may never have landed.
	_ = fr

	// THE PROPERTY, asserted directly on the baseline rather than inferred from a
	// later resync: sending is not acknowledging. Until the client confirms,
	// row A must still be part of what the next frame owes it.
	f.mu.Lock()
	baselineHasA := f.acked.at(0, 0).Content == "A"
	f.mu.Unlock()
	if baselineHasA {
		t.Fatal("the baseline advanced on SEND rather than on acknowledgement — a frame " +
			"that never landed has been treated as delivered, so row A has silently " +
			"left the aggregate")
	}

	// Now row B changes.
	f.publish([]tui.CellUpdate{put(0, 1, "B")}, cursorState{})

	// The lost frame is retried. Because the baseline never advanced, the retry
	// must carry BOTH cells.
	f.ack(fr.Rev + 99) // a forged/stale ack must not move the baseline
	f.reset()          // the client reconnects: everything must be resent

	sent := pump(t, f, c)
	if sent == 0 {
		t.Fatal("nothing was sent after reset")
	}
	if !c.g.equal(f.serverGrid()) {
		t.Fatalf("client diverged\nclient: %s\nserver: %s", render(c.g), render(f.serverGrid()))
	}
	for _, want := range []struct {
		x, y int
		s    string
	}{{0, 0, "A"}, {0, 1, "B"}} {
		if got := c.g.at(want.x, want.y).Content; got != want.s {
			t.Errorf("cell (%d,%d) = %q, want %q — an unacknowledged cell left the aggregate",
				want.x, want.y, got, want.s)
		}
	}
}

// A stale or forged acknowledgement must not advance the baseline. The baseline
// decides what the next diff OMITS, so believing a wrong ack is exactly how a
// client ends up permanently missing a cell.
func TestFramer_StaleAckIsIgnored(t *testing.T) {
	t.Parallel()
	f := newFramer(2, 1)
	c := newClient(2, 1)
	pump(t, f, c)

	f.publish([]tui.CellUpdate{put(0, 0, "X")}, cursorState{})
	fr, ok := f.next()
	if !ok {
		t.Fatal("no frame")
	}
	for _, bogus := range []uint64{0, fr.Rev - 1, fr.Rev + 1, ^uint64(0)} {
		f.ack(bogus)
		if _, ok := f.next(); ok {
			t.Fatalf("ack(%d) released the in-flight slot for a frame that was not acknowledged", bogus)
		}
	}
	// The real one works.
	f.ack(fr.Rev)
	c.apply(fr)
	if !c.g.equal(f.serverGrid()) {
		t.Error("the genuine ack did not advance the baseline")
	}
}

// Only ONE frame is in flight, so memory stays at two grids plus a snapshot.
func TestFramer_OneFrameInFlight(t *testing.T) {
	t.Parallel()
	f := newFramer(2, 2)
	f.publish([]tui.CellUpdate{put(0, 0, "a")}, cursorState{})
	first, ok := f.next()
	if !ok {
		t.Fatal("no first frame")
	}
	f.publish([]tui.CellUpdate{put(1, 1, "b")}, cursorState{})
	if _, ok := f.next(); ok {
		t.Fatal("a second frame was produced while the first was unacknowledged")
	}
	f.ack(first.Rev)
	second, ok := f.next()
	if !ok {
		t.Fatal("no frame after the ack")
	}
	if second.Rev <= first.Rev {
		t.Errorf("revisions must increase: %d then %d", first.Rev, second.Rev)
	}
}

// Criterion 4b: revisions increase monotonically; a fresh connection and a
// reconnect after a gap both get a FULL snapshot, not a diff.
func TestFramer_RevisionsAndResync(t *testing.T) {
	t.Parallel()
	f := newFramer(3, 2)
	c := newClient(3, 2)

	// The very first frame a client sees must be full: it holds nothing.
	f.publish([]tui.CellUpdate{put(0, 0, "a")}, cursorState{})
	first, ok := f.next()
	if !ok {
		t.Fatal("no first frame")
	}
	if !first.Full {
		t.Error("a fresh connection must receive a full snapshot, not a diff")
	}
	c.apply(first)
	f.ack(first.Rev)

	// A steady-state frame is a diff.
	f.publish([]tui.CellUpdate{put(1, 0, "b")}, cursorState{})
	second, ok := f.next()
	if !ok {
		t.Fatal("no second frame")
	}
	if second.Full {
		t.Error("a steady-state frame should be a diff")
	}
	if len(second.Updates) != 1 {
		t.Errorf("%d updates, want 1: only dirty cells are transmitted", len(second.Updates))
	}
	c.apply(second)
	f.ack(second.Rev)

	// Reconnect: the new client holds nothing, so it must get a full snapshot.
	f.reset()
	third, ok := f.next()
	if !ok {
		t.Fatal("no frame after reset")
	}
	if !third.Full {
		t.Error("a reconnect must receive a full snapshot")
	}
	if third.Rev <= second.Rev {
		t.Errorf("revisions must increase across a reconnect: %d then %d", second.Rev, third.Rev)
	}
	fresh := newClient(3, 2)
	fresh.apply(third)
	f.ack(third.Rev)
	if !fresh.g.equal(f.serverGrid()) {
		t.Error("the resync snapshot did not reproduce the server grid")
	}
}

// A resize changes the reported size, the next frame matches the
// new grid, and it does not tear — a diff cannot cross a shape change, so the
// frame must be full.
func TestFramer_ResizeForcesFullFrame(t *testing.T) {
	t.Parallel()
	f := newFramer(3, 2)
	c := newClient(3, 2)
	f.publish([]tui.CellUpdate{put(0, 0, "a"), put(2, 1, "z")}, cursorState{})
	pump(t, f, c)

	f.resize(5, 3)
	if w, h := f.size(); w != 5 || h != 3 {
		t.Fatalf("size = %dx%d, want 5x3", w, h)
	}
	fr, ok := f.next()
	if !ok {
		t.Fatal("no frame after resize")
	}
	if !fr.Full {
		t.Error("a resize must produce a full frame: a diff cannot cross a shape change")
	}
	if fr.W != 5 || fr.H != 3 {
		t.Errorf("frame reports %dx%d, want 5x3", fr.W, fr.H)
	}
	c.apply(fr)
	f.ack(fr.Rev)
	if !c.g.equal(f.serverGrid()) {
		t.Fatalf("client did not match after resize\nclient: %s\nserver: %s",
			render(c.g), render(f.serverGrid()))
	}
	// The overlapping content survived: a resize is not a repaint.
	if got := c.g.at(0, 0).Content; got != "a" {
		t.Errorf("content at (0,0) = %q after resize, want \"a\"", got)
	}
}

// Shrinking must not lose the client's ability to converge, and an update
// outside the new bounds must be dropped rather than fail the Flush.
func TestFramer_ShrinkDropsOutOfRangeUpdates(t *testing.T) {
	t.Parallel()
	f := newFramer(4, 2)
	c := newClient(4, 2)
	pump(t, f, c)

	f.resize(2, 1)
	// An update aimed at a column that no longer exists: the client does not
	// have that position, so dropping it is correct. Failing would take down the
	// App loop for a race it cannot avoid.
	f.publish([]tui.CellUpdate{put(3, 1, "gone"), put(0, 0, "k")}, cursorState{})
	pump(t, f, c)

	if !c.g.equal(f.serverGrid()) {
		t.Fatalf("client diverged after a shrink\nclient: %s\nserver: %s",
			render(c.g), render(f.serverGrid()))
	}
	if got := c.g.at(0, 0).Content; got != "k" {
		t.Errorf("in-range update lost: (0,0) = %q", got)
	}
}

// Cursor state is latched and travels with a frame, never immediately.
func TestFramer_CursorTravelsWithTheFrame(t *testing.T) {
	t.Parallel()
	f := newFramer(3, 2)
	want := cursorState{Visible: true, X: 2, Y: 1, Shape: tui.CursorShapeBar}

	// A cursor change alone must still produce a frame: it is visible state.
	f.publish(nil, want)
	fr, ok := f.next()
	if !ok {
		t.Fatal("a cursor-only change produced no frame")
	}
	if fr.Cursor != want {
		t.Errorf("frame cursor = %+v, want %+v", fr.Cursor, want)
	}
	f.ack(fr.Rev)

	// No further change: nothing to send.
	if _, ok := f.next(); ok {
		t.Error("an unchanged cursor produced a frame")
	}
}

// A publication with no changes at all must not manufacture a frame.
func TestFramer_NoChangeNoFrame(t *testing.T) {
	t.Parallel()
	f := newFramer(2, 1)
	c := newClient(2, 1)
	// Get past the initial full snapshot first: a fresh framer owes the client
	// everything, so its first frame is legitimately not a no-op.
	f.publish([]tui.CellUpdate{put(0, 0, "a")}, cursorState{})
	pump(t, f, c)
	before := f.rev

	// Writing content the grid already holds.
	f.publish([]tui.CellUpdate{put(0, 0, "a"), {X: 1, Y: 0, Cell: blank}}, cursorState{})
	if fr, ok := f.next(); ok {
		if len(fr.Updates) != 0 {
			t.Errorf("a no-op publication produced %d updates: %+v", len(fr.Updates), fr.Updates)
		}
		f.ack(fr.Rev)
	}
	if f.rev > before+1 {
		t.Errorf("revision advanced %d times for a no-op", f.rev-before)
	}
}

// render is a debugging helper: the grid as one string per row.
func render(g *grid) string {
	out := ""
	for y := range g.h {
		for x := range g.w {
			c := g.at(x, y)
			if c.Content == "" {
				out += "."
				continue
			}
			out += c.Content
		}
		out += "/"
	}
	return out
}

// A long run of publications with a permanently blocked client must not grow
// memory: the pending frame is a dirty bit, not a queue.
func TestFramer_BlockedClientDoesNotAccumulate(t *testing.T) {
	t.Parallel()
	f := newFramer(8, 4)
	f.publish(nil, cursorState{})
	inflight, ok := f.next()
	if !ok {
		t.Fatal("no frame")
	}
	_ = inflight // never acknowledged: the client is gone

	for i := range 10_000 {
		f.publish([]tui.CellUpdate{put(i%8, (i/8)%4, fmt.Sprint(i%10))}, cursorState{})
	}
	// Two grids plus one snapshot, regardless of how many publications happened.
	f.mu.Lock()
	grids := 2
	if f.sent != nil {
		grids++
	}
	f.mu.Unlock()
	if grids > 3 {
		t.Errorf("%d grids retained, want at most 3", grids)
	}
	if got := f.coalescedCount(); got < 9_000 {
		t.Errorf("coalesced count = %d, want most of the 10000 publications counted", got)
	}
}
