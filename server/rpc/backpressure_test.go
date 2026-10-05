package rpc_test

import (
	"bufio"
	"errors"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/msgpack"
	"github.com/yongjohnlee80/golib/server/rpc"
)

// burstServer pushes n notifications ("evt", i) at once, then holds the connection open until the
// client goes.
func burstServer(t *testing.T, n int) string {
	t.Helper()
	return fakeServer(t, func(conn net.Conn, br *bufio.Reader) {
		for i := range n {
			if _, err := conn.Write(rawFrame(t, []any{int64(2), "evt", []any{int64(i)}})); err != nil {
				return
			}
		}
		for {
			if _, err := msgpack.Decode(br, nil); err != nil {
				return
			}
		}
	})
}

// heldCallback records each notification's number, the first one held at gate until it is
// closed: the dispatcher is stuck there, so the queue behind it fills, however the goroutines are
// scheduled.
type heldCallback struct {
	gate    chan struct{}
	mu      sync.Mutex
	got     []int64
	arrived chan struct{} // closed when want have arrived
	want    int
}

func newHeld(want int) *heldCallback {
	return &heldCallback{gate: make(chan struct{}), arrived: make(chan struct{}), want: want}
}

func (h *heldCallback) on(_ string, params []any) {
	h.mu.Lock()
	first := len(h.got) == 0
	h.got = append(h.got, params[0].(int64))
	if len(h.got) == h.want {
		close(h.arrived)
	}
	h.mu.Unlock()
	if first {
		<-h.gate
	}
}

// TestABurstOverflowsTheQueueByDefault: notifications past a full queue poison the client with
// ErrNotificationOverflow, the reader never waiting on the callback.
func TestABurstOverflowsTheQueueByDefault(t *testing.T) {
	t.Parallel()
	h := newHeld(50)
	defer close(h.gate)
	c := dialClient(t, burstServer(t, 50), rpc.OnNotification(h.on), rpc.NotificationBuffer(4))
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a burst past a full queue did not end the client")
	}
	if !errors.Is(c.Err(), rpc.ErrNotificationOverflow) {
		t.Fatalf("Err = %v, want ErrNotificationOverflow", c.Err())
	}
}

// TestBackpressureDeliversABurstWhole: with NotificationBackpressure, a burst far past the queue
// waits for the callback, and every notification arrives, in order, the client still serving.
func TestBackpressureDeliversABurstWhole(t *testing.T) {
	t.Parallel()
	const n = 50
	h := newHeld(n)
	c := dialClient(t, burstServer(t, n), rpc.OnNotification(h.on), rpc.NotificationBuffer(4), rpc.NotificationBackpressure())
	// the callback is held on the first: the reader waits on the full queue, and the client lives
	select {
	case <-c.Done():
		t.Fatalf("the client ended while the queue was full: %v", c.Err())
	case <-time.After(200 * time.Millisecond):
	}
	close(h.gate)
	select {
	case <-h.arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("the burst did not arrive whole")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, v := range h.got {
		if v != int64(i) {
			t.Fatalf("notification %d is %d: out of order (%v)", i, v, h.got)
		}
	}
	select {
	case <-c.Done():
		t.Fatalf("the client ended after the burst: %v", c.Err())
	default:
	}
}

// readers counts the clients' reader goroutines running.
func readers() int {
	buf := make([]byte, 1<<20)
	return strings.Count(string(buf[:runtime.Stack(buf, true)]), "rpc.(*Client).readLoop(")
}

// TestBackpressureEndsWithTheClient: a reader waiting on a full queue returns when the client is
// closed, never left waiting on a queue no dispatcher drains, and nothing more is dispatched. Not
// parallel: it counts the readers, so no other test's client may be running.
func TestBackpressureEndsWithTheClient(t *testing.T) {
	before := readers()
	h := newHeld(50)
	c := dialClient(t, burstServer(t, 50), rpc.OnNotification(h.on), rpc.NotificationBuffer(4), rpc.NotificationBackpressure())
	time.Sleep(100 * time.Millisecond) // the reader is waiting on the full queue, the callback held
	_ = c.Close()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not end a client whose reader waits on a full queue")
	}
	if !errors.Is(c.Err(), rpc.ErrClientClosed) {
		t.Fatalf("Err = %v, want ErrClientClosed", c.Err())
	}
	for deadline := time.Now().Add(5 * time.Second); readers() > before; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the reader is still running after Close: %d readers, %d before", readers(), before)
		}
	}
	// the callback is let go only now: had it been first, the slot it frees would release a
	// reader that never looked at the client's end
	close(h.gate)
	time.Sleep(100 * time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.got) > 1+4+1 { // the held one, at most the queue's four, and one the dispatcher had taken
		t.Errorf("%d notifications dispatched after Close", len(h.got))
	}
}
