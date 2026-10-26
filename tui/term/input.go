package term

import (
	"io"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// The input side: exactly one goroutine (pump) owns the fd, as ws.go does, and hands its chunks
// to decodeLoop, which owns the decoder, the lone-ESC timer, the size re-checks and Events().

func (b *Backend) startReader() {
	readCh := make(chan []byte, 8)
	b.readerOn = true
	b.wg.Add(2)
	go b.pump(readCh)
	go b.decodeLoop(readCh)
}

// pump is the exactly-one goroutine reading the input fd. It forwards raw
// chunks to the decode loop and records the terminal error on abnormal exit.
func (b *Backend) pump(readCh chan<- []byte) {
	defer b.wg.Done()
	defer close(readCh)
	tmp := make([]byte, 4096)
	for {
		n, err := b.read(tmp)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, tmp[:n])
			select {
			case readCh <- chunk:
			case <-b.done:
				return
			}
		}
		if err != nil {
			select {
			case <-b.done:
				// Stop-induced unblock: clean exit, no error recorded.
			default:
				b.setReadErr(err)
			}
			return
		}
	}
}

func (b *Backend) read(p []byte) (int, error) {
	if b.inFile == nil {
		return b.input.Read(p)
	}
	return b.readFile(p)
}

// unblockReader releases a pump blocked in a read so Stop can join it.
func (b *Backend) unblockReader() {
	if b.inFile != nil {
		unblockFile(b.inFile)
		return
	}
	if c, ok := b.input.(io.Closer); ok {
		_ = c.Close()
	}
}

// decodeLoop owns the decoder and the Events() channel: it feeds chunks to
// the parser, materializes resize notifications through the same producer
// path (/, single logical producer), runs the legacy ESC
// disambiguation timer, and closes Events on exit.
func (b *Backend) decodeLoop(readCh <-chan []byte) {
	defer b.wg.Done()
	defer close(b.events)

	size := &sizeWatch{w: -1, h: -1}
	if sz, err := b.querySize(); err == nil {
		size.w, size.h = sz.W, sz.H
	}
	d := &decoder{
		emit:       b.emitEvent,
		probe:      b.probeReply,
		onFocusIn:  func() { size.check(b, false) }, // focus-in re-check
		optionFold: b.cfg.optionFold,
		erase:      b.erase,
	}
	var esc escTimer
	lastRead := time.Now()

	for {
		select {
		case chunk, ok := <-readCh:
			if !ok {
				esc.disarm()
				d.finish()
				return
			}
			esc.disarm()
			if time.Since(lastRead) > quietPeriod {
				size.check(b, false) // first-input-after-quiet re-check
			}
			lastRead = time.Now()
			d.feedBytes(chunk)
			// ESC disambiguation timeout ONLY when kitty is inactive:
			// flag 1's purpose is removing this ambiguity.
			if !b.kittyOn.Load() && d.awaitingEsc() {
				esc.arm(b.cfg.escTimeout)
			}
		case <-esc.C:
			esc.C = nil
			d.resolveEsc()
		case <-b.resizeCh:
			// OS notification: re-query fresh truth, emit ordered and
			// un-coalesced — the App intake owns coalescing.
			size.check(b, true)
		case <-b.done:
			esc.disarm()
			d.finish()
			return
		}
	}
}

// sizeWatch is the size decodeLoop last reported, so a re-check emits a ResizeEvent only when it
// changed (or when forced, for an OS notification).
type sizeWatch struct{ w, h int }

func (s *sizeWatch) check(b *Backend, force bool) {
	sz, err := b.querySize()
	if err != nil {
		return
	}
	if force || sz.W != s.w || sz.H != s.h {
		s.w, s.h = sz.W, sz.H
		b.emitEvent(tui.ResizeEvent{W: sz.W, H: sz.H})
	}
}

// escTimer is the lone-ESC disambiguation timer: C fires once the timeout passes, and is nil
// while the timer is disarmed, so a select on it waits forever.
type escTimer struct {
	t *time.Timer
	C <-chan time.Time
}

func (e *escTimer) arm(d time.Duration) {
	if e.t == nil {
		e.t = time.NewTimer(d)
	} else {
		e.t.Reset(d)
	}
	e.C = e.t.C
}

func (e *escTimer) disarm() {
	if e.t != nil {
		e.t.Stop()
	}
	e.C = nil
}

// emitEvent delivers on Events, blocking if the consumer stalls (events are
// never dropped) but never outliving Stop.
func (b *Backend) emitEvent(ev tui.Event) {
	select {
	case b.events <- ev:
	case <-b.done:
	}
}

// probeReply routes a decoded probe reply to the in-flight probe; replies
// after the fence are discarded harmlessly.
func (b *Backend) probeReply(r probeReply) {
	if !b.probing.Load() {
		return
	}
	select {
	case b.probeCh <- r:
	default:
	}
}
