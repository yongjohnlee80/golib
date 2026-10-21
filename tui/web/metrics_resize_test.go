package web

import (
	"context"
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// A reconnect whose resize the backend takes publishes the new client's cell size with it.
func TestReconnectPublishesItsCellMetricsWithItsResize(t *testing.T) {
	t.Parallel()
	b := started(t, hello(), EventQueue(1))
	h := hello()
	h.Metrics = Metrics{CellW: 12, CellH: 24}
	if err := b.attach(h); err != nil {
		t.Fatal(err)
	}
	if _, ok := (<-b.Events()).(tui.ResizeEvent); !ok {
		t.Fatal("the reconnect delivered no resize")
	}
	if got := b.CellPixels(); got != (tui.CellPixels{W: 12, H: 24}) {
		t.Errorf("after the reconnect: %+v, want 12 by 24", got)
	}
}

// A reconnect whose resize the full queue refuses changes nothing: CellPixels keeps the old size,
// since the App has no ResizeEvent to read a new one for.
func TestARefusedReconnectKeepsTheOldCellMetrics(t *testing.T) {
	t.Parallel()
	b := started(t, hello(), EventQueue(1))
	before := b.CellPixels()
	if err := b.submit(tui.KeyEvent{Code: 'x'}); err != nil {
		t.Fatal(err)
	}
	h := hello()
	h.Metrics = Metrics{CellW: 12, CellH: 24}
	if err := b.attach(h); !errors.Is(err, ErrEventOverflow) {
		t.Fatalf("Attach = %v, want the overflow", err)
	}
	if _, ok := (<-b.Events()).(tui.KeyEvent); !ok {
		t.Fatal("the queued input was lost")
	}
	select {
	case ev := <-b.Events():
		t.Fatalf("a refused reconnect emitted %#v", ev)
	default:
	}
	if got := b.CellPixels(); got != before {
		t.Errorf("a refused reconnect changed CellPixels from %+v to %+v", before, got)
	}
}

// A browser's resize message carries its measured cell size, and CellPixels follows it with the
// ResizeEvent.
func TestAWireResizeCarriesItsCellMetrics(t *testing.T) {
	t.Parallel()
	b := started(t, hello())
	c := &fakeConn{inbound: []clientMessage{{T: msgResize, Cols: 20, Rows: 5, CellW: 12, CellH: 24}}}
	l := &sessionLoop{limits: DefaultLimits().normalize(), log: nopLogger{}}
	if err := l.readPump(context.Background(), c, &Session{backend: b}); err == nil {
		t.Fatal("want the transport error once the script runs out")
	}
	ev, ok := (<-b.Events()).(tui.ResizeEvent)
	if !ok || ev.W != 20 || ev.H != 5 {
		t.Fatalf("the resize did not reach the backend: %#v", ev)
	}
	if got := b.CellPixels(); got != (tui.CellPixels{W: 12, H: 24}) {
		t.Errorf("after the wire resize: %+v, want 12 by 24", got)
	}
}
