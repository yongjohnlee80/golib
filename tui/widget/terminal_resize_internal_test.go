package widget

import (
	"io"
	"reflect"
	"sync"
	"testing"
	"time"
)

// gatedResizeProc is a program whose Resize blocks until the test lets it go, and which records
// each size in the order its Resize ran and the most Resizes it saw at once.
type gatedResizeProc struct {
	io.ReadWriter
	gate chan struct{}

	mu       sync.Mutex
	sizes    [][2]int
	inFlight int
	most     int
}

func (p *gatedResizeProc) Resize(rows, cols int) error {
	p.mu.Lock()
	p.inFlight++
	p.most = max(p.most, p.inFlight)
	p.sizes = append(p.sizes, [2]int{rows, cols})
	p.mu.Unlock()
	<-p.gate
	p.mu.Lock()
	p.inFlight--
	p.mu.Unlock()
	return nil
}

func (p *gatedResizeProc) Close() error       { return nil }
func (p *gatedResizeProc) Wait() (int, error) { return 0, nil }

func (p *gatedResizeProc) state() ([][2]int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][2]int(nil), p.sizes...), p.inFlight, p.most
}

// waitSizes waits until the program has been asked for n sizes in all.
func (p *gatedResizeProc) waitSizes(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if sizes, _, _ := p.state(); len(sizes) >= n {
			return
		}
		if time.Now().After(deadline) {
			sizes, _, _ := p.state()
			t.Fatalf("the program was sent %v, want %d sizes", sizes, n)
		}
		time.Sleep(time.Millisecond)
	}
}

// A storm of sizes while one Resize is in flight reaches the program as that one and then the
// newest: one Resize at a time, in order, the sizes in between dropped. The next size after the
// storm is sent in turn.
func TestPtyResizerSendsTheNewestSizeInOrder(t *testing.T) {
	p := &gatedResizeProc{gate: make(chan struct{})}
	r := &ptyResizer{proc: p}
	r.resize(10, 40)
	p.waitSizes(t, 1) // the first is in flight, held at the gate
	for i := range 50 {
		r.resize(11+i, 41+i)
	}
	close(p.gate)
	p.waitSizes(t, 2)
	r.resize(5, 20)
	p.waitSizes(t, 3)
	time.Sleep(20 * time.Millisecond) // anything sent late would show now
	sizes, inFlight, most := p.state()
	if want := [][2]int{{10, 40}, {60, 90}, {5, 20}}; !reflect.DeepEqual(sizes, want) {
		t.Errorf("the program was sent %v, want %v", sizes, want)
	}
	if most != 1 || inFlight != 0 {
		t.Errorf("Resizes at once: at most %d, now %d; want 1 and 0", most, inFlight)
	}
	r.mu.Lock()
	busy := r.busy
	r.mu.Unlock()
	if busy {
		t.Error("the resizer is still busy with nothing left to send")
	}
}
