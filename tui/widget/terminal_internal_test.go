package widget

import (
	"io"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/pty"
	"github.com/yongjohnlee80/golib/tui/vt"
)

// slowProc is a program whose hang-up takes as long as the test says: Close
// blocks until release, like a child ignoring SIGHUP through the grace
// period.
type slowProc struct {
	r       *io.PipeReader
	w       *io.PipeWriter
	release chan struct{}
	once    sync.Once
	done    chan struct{}
}

func newSlowProc() *slowProc {
	r, w := io.Pipe()
	return &slowProc{r: r, w: w, release: make(chan struct{}), done: make(chan struct{})}
}

func (p *slowProc) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *slowProc) Write(b []byte) (int, error) { return len(b), nil }
func (p *slowProc) Resize(int, int) error       { return nil }
func (p *slowProc) Wait() (int, error)          { <-p.done; return 0, nil }
func (p *slowProc) end() {
	p.once.Do(func() {
		p.w.Close()
		close(p.done)
	})
}

func (p *slowProc) Close() error {
	<-p.release
	p.end()
	return nil
}

// mountTerminal runs a Terminal over proc; stop ends the app (once, whether
// the test or its cleanup calls it).
func mountTerminal(t *testing.T, proc TerminalProcess) (term *Terminal, h *internalHarness, stop func()) {
	t.Helper()
	term = NewTerminal(WithStarter(func(pty.Cmd) (TerminalProcess, error) { return proc, nil }))
	h = startAppInternal(t, term, 20, 4)
	stop = sync.OnceFunc(h.stopInternal)
	t.Cleanup(stop)
	h.onLoopInternal(func() {
		if err := term.Start(); err != nil {
			t.Fatal(err)
		}
	})
	return term, h, stop
}

// pumpsGone waits for the reader goroutine to end.
func pumpsGone(t *testing.T, term *Terminal) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		term.pumps.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the reader goroutine outlived its program")
	}
}

func TestTerminalReaderEndsOnStop(t *testing.T) {
	p := newSlowProc()
	close(p.release)
	term, h, _ := mountTerminal(t, p)
	h.onLoopInternal(term.Stop)
	pumpsGone(t, term)
}

func TestTerminalReaderEndsOnExit(t *testing.T) {
	p := newSlowProc()
	term, h, _ := mountTerminal(t, p)
	p.end()
	pumpsGone(t, term)
	h.syncInternal()
	var running bool
	h.onLoopInternal(func() { running = term.Running() })
	if running {
		t.Error("still running after the exit")
	}
}

func TestTerminalReaderEndsOnUnmount(t *testing.T) {
	p := newSlowProc()
	close(p.release)
	term, _, stop := mountTerminal(t, p)
	stop() // the app's exit unmounts the tree
	pumpsGone(t, term)
}

// While a program takes its whole grace period to end, the UI keeps
// answering: Stop's hang-up runs off the loop.
func TestTerminalStopNeverBlocksTheLoop(t *testing.T) {
	p := newSlowProc()
	term, h, _ := mountTerminal(t, p)
	start := time.Now()
	h.onLoopInternal(term.Stop)
	for range 5 {
		h.syncInternal() // the loop still turns
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("the loop stalled %v behind the hang-up", d)
	}
	close(p.release)
	pumpsGone(t, term)
}

func TestEncodeKey(t *testing.T) {
	plain, app := vt.Modes{}, vt.Modes{AppCursor: true}
	kitty := vt.Modes{KeyboardFlags: vt.KittyDisambiguate}
	for _, c := range []struct {
		k    tui.KeyEvent
		m    vt.Modes
		want string
	}{
		{tui.KeyEvent{Code: 'a', Text: "a"}, plain, "a"},
		{tui.KeyEvent{Code: 'A', Text: "A", Mods: tui.ModShift}, plain, "A"},
		{tui.KeyEvent{Code: 0x4e16, Text: "世"}, plain, "世"},
		{tui.KeyEvent{Code: 'a', Mods: tui.ModCtrl}, plain, "\x01"},
		{tui.KeyEvent{Code: 'Z', Mods: tui.ModCtrl}, plain, "\x1a"},
		{tui.KeyEvent{Code: ' ', Mods: tui.ModCtrl}, plain, "\x00"},
		{tui.KeyEvent{Code: '[', Mods: tui.ModCtrl}, plain, "\x1b"},
		{tui.KeyEvent{Code: '\\', Mods: tui.ModCtrl}, plain, "\x1c"},
		{tui.KeyEvent{Code: ']', Mods: tui.ModCtrl}, plain, "\x1d"},
		{tui.KeyEvent{Code: '^', Mods: tui.ModCtrl}, plain, "\x1e"},
		{tui.KeyEvent{Code: '_', Mods: tui.ModCtrl}, plain, "\x1f"},
		{tui.KeyEvent{Code: '?', Mods: tui.ModCtrl}, plain, "\x7f"},
		{tui.KeyEvent{Code: ';', Mods: tui.ModCtrl}, plain, ""},
		{tui.KeyEvent{Code: 'x', Mods: tui.ModAlt}, plain, "\x1bx"},
		{tui.KeyEvent{Code: 'c', Mods: tui.ModAlt | tui.ModCtrl}, plain, "\x1b\x03"},
		{tui.KeyEvent{Code: tui.KeyEnter}, plain, "\r"},
		{tui.KeyEvent{Code: tui.KeyEnter, Mods: tui.ModAlt}, plain, "\x1b\r"},
		{tui.KeyEvent{Code: tui.KeyTab}, plain, "\t"},
		{tui.KeyEvent{Code: tui.KeyTab, Mods: tui.ModShift}, plain, "\x1b[Z"},
		{tui.KeyEvent{Code: tui.KeyBackspace}, plain, "\x7f"},
		{tui.KeyEvent{Code: tui.KeyBackspace, Mods: tui.ModCtrl}, plain, "\x08"},
		{tui.KeyEvent{Code: tui.KeyBackspace, Mods: tui.ModAlt}, plain, "\x1b\x7f"},
		{tui.KeyEvent{Code: tui.KeyEscape}, plain, "\x1b"},
		{tui.KeyEvent{Code: tui.KeyUp}, plain, "\x1b[A"},
		{tui.KeyEvent{Code: tui.KeyDown}, app, "\x1bOB"},
		{tui.KeyEvent{Code: tui.KeyRight, Mods: tui.ModShift}, app, "\x1b[1;2C"},
		{tui.KeyEvent{Code: tui.KeyLeft, Mods: tui.ModCtrl | tui.ModAlt}, plain, "\x1b[1;7D"},
		{tui.KeyEvent{Code: tui.KeyHome}, plain, "\x1b[H"},
		{tui.KeyEvent{Code: tui.KeyEnd}, app, "\x1bOF"},
		{tui.KeyEvent{Code: tui.KeyF1}, plain, "\x1bOP"},
		{tui.KeyEvent{Code: tui.KeyF4, Mods: tui.ModMeta}, plain, "\x1b[1;9S"},
		{tui.KeyEvent{Code: tui.KeyF5}, plain, "\x1b[15~"},
		{tui.KeyEvent{Code: tui.KeyF12, Mods: tui.ModShift}, plain, "\x1b[24;2~"},
		{tui.KeyEvent{Code: tui.KeyInsert}, plain, "\x1b[2~"},
		{tui.KeyEvent{Code: tui.KeyDelete}, plain, "\x1b[3~"},
		{tui.KeyEvent{Code: tui.KeyPageUp}, plain, "\x1b[5~"},
		{tui.KeyEvent{Code: tui.KeyPageDown, Mods: tui.ModCtrl}, plain, "\x1b[6;5~"},
		{tui.KeyEvent{Code: 'a', Text: "a", Kind: tui.KeyRelease}, plain, ""},
		{tui.KeyEvent{Code: 0xE100}, plain, ""}, // a private-use key with no text
		// The kitty keyboard protocol, once a program asks to disambiguate: a key whose legacy
		// bytes are ambiguous is CSI code;mods u, and every other key keeps its legacy bytes.
		{tui.KeyEvent{Code: tui.KeyEnter, Mods: tui.ModShift}, plain, "\r"},
		{tui.KeyEvent{Code: tui.KeyEnter, Mods: tui.ModShift}, kitty, "\x1b[13;2u"},
		{tui.KeyEvent{Code: tui.KeyEnter}, kitty, "\r"},
		{tui.KeyEvent{Code: tui.KeyEnter, Mods: tui.ModAlt}, kitty, "\x1b[13;3u"},
		{tui.KeyEvent{Code: tui.KeyTab}, kitty, "\t"},
		{tui.KeyEvent{Code: tui.KeyTab, Mods: tui.ModShift}, kitty, "\x1b[9;2u"},
		{tui.KeyEvent{Code: tui.KeyBackspace}, kitty, "\x7f"},
		{tui.KeyEvent{Code: tui.KeyBackspace, Mods: tui.ModCtrl}, kitty, "\x1b[127;5u"},
		{tui.KeyEvent{Code: tui.KeyEscape}, kitty, "\x1b[27u"},
		{tui.KeyEvent{Code: 'c', Mods: tui.ModCtrl}, kitty, "\x1b[99;5u"},
		{tui.KeyEvent{Code: 'A', Mods: tui.ModCtrl | tui.ModShift}, kitty, "\x1b[97;6u"},
		{tui.KeyEvent{Code: 'x', Text: "x", Mods: tui.ModAlt}, kitty, "\x1b[120;3u"},
		{tui.KeyEvent{Code: 'a', Text: "a"}, kitty, "a"},
		{tui.KeyEvent{Code: 'A', Text: "A", Mods: tui.ModShift}, kitty, "A"},
		{tui.KeyEvent{Code: tui.KeyUp, Mods: tui.ModCtrl}, kitty, "\x1b[1;5A"},
		{tui.KeyEvent{Code: tui.KeyF5}, kitty, "\x1b[15~"},
	} {
		if got := string(encodeKey(c.k, c.m)); got != c.want {
			t.Errorf("encodeKey(%+v) = %q, want %q", c.k, got, c.want)
		}
	}
}

func TestEncodePaste(t *testing.T) {
	if got := string(encodePaste("a\nb", vt.Modes{})); got != "a\rb" {
		t.Errorf("plain paste %q", got)
	}
	for _, c := range []struct{ name, text, want string }{
		{"a marker", "x\x1b[201~y", "xy"},
		{"a marker split around another", "x\x1b[20\x1b[201~1~y", "xy"},
		{"nested twice", "\x1b[20\x1b[20\x1b[201~1~1~; touch pwned", "; touch pwned"},
		{"a marker at each end", "\x1b[201~a\x1b[201~", "a"},
		{"other escapes stay", "\x1b[200~a\x1b[31m", "\x1b[200~a\x1b[31m"},
	} {
		got := string(encodePaste(c.text, vt.Modes{BracketedPaste: true}))
		if got != "\x1b[200~"+c.want+"\x1b[201~" {
			t.Errorf("%s: bracketed paste %q, want the body %q", c.name, got, c.want)
		}
	}
}

// A marker nested two hundred thousand deep, 2.2 MB, is removed whole in one pass over the text.
func TestADeeplyNestedEndMarkerIsRemovedWhole(t *testing.T) {
	const depth = 200000
	text := strings.Repeat("\x1b[20", depth) + "\x1b[201~" + strings.Repeat("1~", depth) + "z"
	if got := string(encodePaste(text, vt.Modes{BracketedPaste: true})); got != "\x1b[200~z\x1b[201~" {
		t.Errorf("a nested marker left %d bytes before z", len(got)-len("\x1b[200~z\x1b[201~"))
	}
}

// No text, however its pieces of the end marker are arranged, ends a bracketed paste before its
// own end: the body between the brackets never holds the marker.
func TestABracketedPasteBodyNeverHoldsTheEndMarker(t *testing.T) {
	pieces := []string{"\x1b", "[", "2", "0", "1", "~", "\x1b[201~", "\x1b[20", "1~", "a"}
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		for n := rng.IntN(12); n >= 0; n-- {
			b.WriteString(pieces[rng.IntN(len(pieces))])
		}
		out := string(encodePaste(b.String(), vt.Modes{BracketedPaste: true}))
		body := strings.TrimSuffix(strings.TrimPrefix(out, "\x1b[200~"), "\x1b[201~")
		if strings.Contains(body, "\x1b[201~") {
			t.Fatalf("%q: the body %q ends the paste early", b.String(), body)
		}
	}
}

func TestEncodePasteCannotReconstructAnEndMarker(t *testing.T) {
	const end = "\x1b[201~"
	for split := 0; split <= len(end); split++ {
		payload := "before" + end[:split] + end + end[split:] + "after"
		if got := string(encodePaste(payload, vt.Modes{BracketedPaste: true})); got != "\x1b[200~beforeafter"+end {
			t.Errorf("split %d reconstructed an end marker: %q", split, got)
		}
	}
	nested := end
	for range 32 {
		nested = end[:3] + nested + end[3:]
	}
	if got := string(encodePaste("가\n"+nested+"b\x1b[31m", vt.Modes{BracketedPaste: true})); got != "\x1b[200~가\nb\x1b[31m"+end {
		t.Errorf("nested marker escaped the paste or changed other text: %q", got)
	}
}

func TestEncodeMouse(t *testing.T) {
	press := tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 4, Y: 2}
	release := tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 4, Y: 2}
	motion := tui.MouseEvent{Kind: tui.MouseMotion, X: 5, Y: 2}
	wheel := tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelUp, X: 0, Y: 0, Mods: tui.ModCtrl}
	sgr := func(m vt.MouseMode) vt.Modes { return vt.Modes{Mouse: m, MouseSGR: true} }
	for _, c := range []struct {
		name string
		m    vt.Modes
		evs  []tui.MouseEvent
		want string
	}{
		{"off", vt.Modes{}, []tui.MouseEvent{press}, ""},
		{"x10 press only", vt.Modes{Mouse: vt.MouseX10}, []tui.MouseEvent{press, release, wheel}, "\x1b[M %#"},
		{"click legacy", vt.Modes{Mouse: vt.MouseClick}, []tui.MouseEvent{press, release}, "\x1b[M %#\x1b[M#%#"},
		{"click sgr", sgr(vt.MouseClick), []tui.MouseEvent{press, release}, "\x1b[<0;5;3M\x1b[<0;5;3m"},
		{"click ignores motion", sgr(vt.MouseClick), []tui.MouseEvent{motion}, ""},
		{"drag reports held motion", sgr(vt.MouseDrag), []tui.MouseEvent{motion, press, motion}, "\x1b[<0;5;3M\x1b[<32;6;3M"},
		{"motion reports all motion", sgr(vt.MouseMotion), []tui.MouseEvent{motion}, "\x1b[<35;6;3M"},
		{"wheel with ctrl", sgr(vt.MouseClick), []tui.MouseEvent{wheel}, "\x1b[<80;1;1M"},
		{"legacy cannot say far columns", vt.Modes{Mouse: vt.MouseClick}, []tui.MouseEvent{{Kind: tui.MousePress, Button: tui.MouseLeft, X: 300}}, ""},
		{"right and middle", sgr(vt.MouseClick), []tui.MouseEvent{{Kind: tui.MousePress, Button: tui.MouseRight}, {Kind: tui.MousePress, Button: tui.MouseMiddle, Mods: tui.ModShift | tui.ModAlt}}, "\x1b[<2;1;1M\x1b[<13;1;1M"},
	} {
		var mt mouseTracker
		var got string
		for _, e := range c.evs {
			got += string(mt.encode(e, c.m))
		}
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// floodProc writes output as fast as it is read, until it is closed: with no
// loop draining the bridge, its reader soon blocks on the bridge's budget.
type floodProc struct {
	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

func newFloodProc() *floodProc { return &floodProc{done: make(chan struct{})} }

func (p *floodProc) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, io.EOF
	}
	for i := range b {
		b[i] = 'x'
	}
	return len(b), nil
}
func (p *floodProc) Write(b []byte) (int, error) { return len(b), nil }
func (p *floodProc) Resize(int, int) error       { return nil }
func (p *floodProc) Wait() (int, error)          { <-p.done; return 0, nil }
func (p *floodProc) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.done)
	}
	return nil
}

// The App's end releases the input queue's goroutine, which waits for keys
// no loop will send again.
func TestTerminalInputWriterEndsWithTheApp(t *testing.T) {
	p := newSlowProc()
	close(p.release)
	term, _, stop := mountTerminal(t, p)
	var in *inputQueue
	stop()
	in = term.in // the loop has ended; its fields are settled
	select {
	case <-in.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the input queue's goroutine outlived the App")
	}
}

// The App's end releases a reader blocked on the output bridge's budget,
// which no loop will drain again.
func TestTerminalBlockedReaderEndsWithTheApp(t *testing.T) {
	p := newFloodProc()
	term, h, stop := mountTerminal(t, p)
	h.onLoopInternal(func() {
		term.wr.mu.Lock()
		term.wr.budget = 1 // the first chunk fills it
		term.wr.mu.Unlock()
	})
	time.Sleep(50 * time.Millisecond) // let output flow through a turn or two
	stop()
	pumpsGone(t, term)
}
