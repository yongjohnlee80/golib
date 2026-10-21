package widget

import (
	"io"
	"sync"

	"github.com/yongjohnlee80/golib/tui"
)

// The Terminal's input: what a key does in input mode, and the bounded queue that writes to the
// program, off the loop.

// send queues bytes for the program without blocking the loop.
func (t *Terminal) send(b []byte) {
	if t.Running() && len(b) > 0 {
		if err := t.in.offer(b); err != nil {
			t.report(err)
		}
	}
}

// SendText accepts literal text into the running generation's bounded input queue.
// On the UI loop. Nil means queued, not written or processed by the child. A
// rejected send queues no bytes; no PTY writer is exposed to the caller.
func (t *Terminal) SendText(text string) error {
	if !t.Running() {
		return ErrTerminalNotRunning
	}
	if len(text) > TerminalInputLimit {
		return ErrTerminalInputFull
	}
	return t.in.offer([]byte(text))
}

// SendPaste sends text as a paste, as the terminal's own paste does: bracketed when the program
// asked for bracketed paste (mode 2004, which agent CLIs and shells with line editing set), so a
// multi-line text arrives whole rather than line by line as Enters; otherwise its newlines go as
// Enter. A text holding the bracket's end has it removed, so it cannot end the paste early. Like
// SendText it is queued, not written, and a rejected send queues nothing. On the UI loop.
func (t *Terminal) SendPaste(text string) error {
	if !t.Running() {
		return ErrTerminalNotRunning
	}
	b := encodePaste(text, t.scr.Modes())
	if len(b) > TerminalInputLimit {
		return ErrTerminalInputFull
	}
	return t.in.offer(b)
}

func isCtrl(k tui.KeyEvent, r rune) bool {
	return k.Mods.Chord()&^tui.ModShift == tui.ModCtrl && (k.Code == r || k.Code == r-'a'+'A')
}

// termInput is what a key does in input mode.
type termInput int

const (
	termNone       termInput = iota // not the Terminal's: it goes on to the host
	termSend                        // the program's: encoded and sent
	termRestart                     // a program started again, after an exit
	termPrefix                      // the escape prefix armed
	termToNormal                    // normal mode
	termSendPrefix                  // the prefix's own byte sent (the prefix twice)
	termSendBoth                    // the prefix's byte, then the key, sent (an unknown key after it)
)

// inputAct is what k does in input mode. Every key that is not one of the Terminal's commands, or is
// one whose condition does not hold, is the program's.
func inputAct(ctx tui.KeyResolver, k tui.KeyEvent, running, exited, prefix, vimKeys, altScreen bool) termInput {
	cmd, isCmd := ctx.KeyCommand(k, terminalCommands...)
	switch {
	case !running && exited && isCmd && cmd.ID == TerminalRestart.ID:
		return termRestart // after an exit or a failed start, Enter or i starts a program
	case !running:
		return termNone
	case prefix:
		// the key after the prefix is its grammar, the literal keys: Ctrl+n for normal mode, Ctrl+\ to
		// send Ctrl+\
		switch {
		case isCtrl(k, 'n'):
			return termToNormal
		case isKey(k, literalCtrlSlash...):
			return termSendPrefix
		}
		return termSendBoth
	case isCmd && cmd.ID == TerminalPrefix.ID:
		return termPrefix
	case isCmd && cmd.ID == TerminalToNormal.ID && vimKeys && !altScreen:
		return termToNormal
	}
	return termSend
}

// inputKey handles a key in input mode.
func (t *Terminal) inputKey(k tui.KeyEvent) bool {
	act := inputAct(t.Context(), k, t.running, t.exited, t.prefix, t.cfg.vimKeys, t.running && t.scr.Modes().AltScreen)
	if t.prefix && t.running {
		t.prefix = false
	}
	switch act {
	case termNone:
		return false
	case termRestart:
		_ = t.Start() // a failure shows on the screen again
	case termPrefix:
		t.prefix = true
	case termToNormal:
		t.setMode(TerminalNormal)
	case termSendPrefix:
		t.send([]byte{0x1c})
	case termSendBoth:
		t.send([]byte{0x1c})
		t.send(encodeKey(k, t.scr.Modes()))
	case termSend:
		b := encodeKey(k, t.scr.Modes())
		if b == nil {
			return false
		}
		t.send(b)
	}
	return true
}

// inputQueue writes keys to the program on its own goroutine, so a program
// that stops reading never blocks the UI loop.
type inputQueue struct {
	mu        sync.Mutex
	cond      *sync.Cond
	q         [][]byte
	pending   int           // queued and currently being written, bounded by TerminalInputLimit
	closed    bool          // drop what is queued and stop
	finishing bool          // write what is queued, then stop
	done      chan struct{} // closed when the writer has stopped
}

func newInputQueue(w io.Writer) *inputQueue {
	iq := &inputQueue{done: make(chan struct{})}
	iq.cond = sync.NewCond(&iq.mu)
	go func() {
		defer close(iq.done)
		for {
			iq.mu.Lock()
			for len(iq.q) == 0 && !iq.closed && !iq.finishing {
				iq.cond.Wait()
			}
			if iq.closed || len(iq.q) == 0 {
				iq.mu.Unlock()
				return
			}
			b := iq.q[0]
			iq.q[0] = nil
			iq.q = iq.q[1:]
			iq.mu.Unlock()
			n, err := w.Write(b)
			iq.mu.Lock()
			iq.pending -= len(b)
			iq.mu.Unlock()
			if err != nil || n != len(b) {
				iq.close()
				return
			}
		}
	}()
	return iq
}

func (iq *inputQueue) offer(b []byte) error {
	iq.mu.Lock()
	defer iq.mu.Unlock()
	if iq.closed || iq.finishing {
		return ErrTerminalNotRunning
	}
	if len(b) > TerminalInputLimit-iq.pending {
		return ErrTerminalInputFull
	}
	if len(b) > 0 {
		iq.q = append(iq.q, append([]byte(nil), b...))
		iq.pending += len(b)
		iq.cond.Signal()
	}
	return nil
}

// finish stops taking keys and returns a channel closed once the queued
// ones are written.
func (iq *inputQueue) finish() <-chan struct{} {
	iq.mu.Lock()
	iq.finishing = true
	iq.cond.Broadcast()
	iq.mu.Unlock()
	return iq.done
}

func (iq *inputQueue) close() {
	iq.mu.Lock()
	iq.closed = true
	for _, b := range iq.q {
		iq.pending -= len(b)
	}
	iq.q = nil
	iq.cond.Broadcast()
	iq.mu.Unlock()
}
