package term

import (
	"encoding/base64"
	"slices"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/internal/vtparse"
)

// Reading the system clipboard through the terminal: OSC 52's query (ESC ] 52 ; c ; ? ST), which
// the terminal answers with the clipboard's text in base64 (ESC ] 52 ; c ; <base64> ST), where it
// allows it. On only with WithClipboardRead.
//
// An answer carries no id: it is the oldest waiting read's. A read the App withdraws (its timeout,
// or its stop) before the answer comes leaves the terminal marked silent, and a late answer then
// finds no read and is dropped, or goes to a later read: either way it is the clipboard's text.
//
// A read waits from before its query is written, so an answer that comes during the write finds
// it: the answer is held until the write ends. A write that fails withdraws the read, its held
// answer dropped, so a read that returns an error is never answered (tui.ClipboardReader); one that
// succeeds hands the held answer over, on a goroutine of its own.

var _ tui.ClipboardReader = (*Backend)(nil)

// ErrClipboardTooLarge is a terminal's answer as long as the reader takes (vtparse.MaxClipboardData)
// or longer: it may have been cut, so it is not given as the clipboard's text.
var ErrClipboardTooLarge = errs.Sentinel(errs.ErrUnsupported, "term: the clipboard's answer is too large to read whole")

// ErrClipboardAnswer is a terminal's answer that isn't base64: not the clipboard's text.
var ErrClipboardAnswer = errs.Sentinel(errs.ErrProtocol, "term: the terminal's clipboard answer isn't base64")

// clipWaiter is one read waiting for the terminal's answer. All but answer are clipMu's.
type clipWaiter struct {
	answer  func(string, error)
	writing bool       // its query is being written: an answer is held, not given
	held    *clipReply // the answer that came while it was
	done    bool       // answered, or withdrawn: answer is never called again
}

// clipReply is a terminal's answer to a read.
type clipReply struct {
	text string
	err  error
}

// clipboardQuery asks the terminal for the clipboard's text (OSC 52, the clipboard selection).
const clipboardQuery = "\x1b]52;c;?\x1b\\"

// ReadClipboard asks the terminal for the system clipboard's text (tui.ClipboardReader). It fails at
// once with tui.ErrClipboardUnsupported unless WithClipboardRead is on, and once the terminal has
// left a read unanswered; with ErrClosed on a stopped backend; and with the write's error when the
// query can't be written. answer runs at most once, never after cancel returns, and never when
// ReadClipboard returns an error: on the input goroutine, or on one of its own for an answer that
// came while the query was being written.
func (b *Backend) ReadClipboard(answer func(text string, err error)) (cancel func(), err error) {
	if b.stopped.Load() {
		return nil, ErrClosed
	}
	w := &clipWaiter{answer: answer, writing: true}
	b.clipMu.Lock()
	if !b.cfg.clipRead || b.silent {
		b.clipMu.Unlock()
		return nil, tui.ErrClipboardUnsupported
	}
	b.clipWait = append(b.clipWait, w)
	b.clipMu.Unlock()
	werr := b.write([]byte(clipboardQuery))
	b.clipMu.Lock()
	w.writing = false
	held := w.held
	w.held = nil
	if werr != nil {
		b.clipWait = slices.DeleteFunc(b.clipWait, func(x *clipWaiter) bool { return x == w })
		w.done = true // never answered: the read failed
		b.clipMu.Unlock()
		return nil, werr
	}
	b.clipMu.Unlock()
	if held != nil {
		go b.giveHeld(w, *held)
	}
	return func() { b.dropClipWaiter(w, true) }, nil
}

// giveHeld gives w the answer held while its query was written, unless it was withdrawn meanwhile:
// under clipMu, as an answer from the input goroutine is.
func (b *Backend) giveHeld(w *clipWaiter, r clipReply) {
	b.clipMu.Lock()
	defer b.clipMu.Unlock()
	if !w.done {
		w.done = true
		w.answer(r.text, r.err)
	}
}

// dropClipWaiter withdraws w; unanswered, a withdrawn read marks the terminal silent: it didn't
// answer in the time the App gave it.
func (b *Backend) dropClipWaiter(w *clipWaiter, unanswered bool) {
	b.clipMu.Lock()
	defer b.clipMu.Unlock()
	if w.done {
		return
	}
	w.done = true
	if i := slices.Index(b.clipWait, w); i >= 0 {
		b.clipWait = slices.Delete(b.clipWait, i, i+1)
		if unanswered && !b.stopped.Load() {
			b.silent = true
		}
	}
}

// clipboardAnswered is the terminal's answer (decoder.clip): payload is OSC 52's after "52;", the
// selection, then the text in base64. It answers the oldest read waiting, under clipMu, so a
// cancel that has returned is never followed by its answer (the App's answer only posts to its
// loop). The terminal's echo of the query ("?") answers none.
func (b *Backend) clipboardAnswered(payload []byte) {
	_, data, ok := cutByte(payload, ';')
	if !ok || string(data) == "?" {
		return
	}
	var text string
	var err error
	switch dec, derr := base64.StdEncoding.DecodeString(string(data)); {
	case len(payload) >= vtparse.MaxClipboardData-len("52;"):
		err = ErrClipboardTooLarge
	case derr != nil:
		err = ErrClipboardAnswer
	default:
		text = string(dec)
	}
	b.clipMu.Lock()
	defer b.clipMu.Unlock()
	if len(b.clipWait) == 0 {
		return
	}
	w := b.clipWait[0]
	b.clipWait = b.clipWait[1:]
	if w.writing { // its query still being written: given when the write succeeds
		w.held = &clipReply{text, err}
		return
	}
	w.done = true
	w.answer(text, err)
}

// cutByte is s before and after its first sep, as bytes.Cut.
func cutByte(s []byte, sep byte) (before, after []byte, ok bool) {
	for i, c := range s {
		if c == sep {
			return s[:i], s[i+1:], true
		}
	}
	return s, nil, false
}
