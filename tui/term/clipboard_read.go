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

var _ tui.ClipboardReader = (*Backend)(nil)

// ErrClipboardTooLarge is a terminal's answer as long as the reader takes (vtparse.MaxClipboardData)
// or longer: it may have been cut, so it is not given as the clipboard's text.
var ErrClipboardTooLarge = errs.Sentinel(errs.ErrUnsupported, "term: the clipboard's answer is too large to read whole")

// ErrClipboardAnswer is a terminal's answer that isn't base64: not the clipboard's text.
var ErrClipboardAnswer = errs.Sentinel(errs.ErrProtocol, "term: the terminal's clipboard answer isn't base64")

// clipWaiter is one read waiting for the terminal's answer.
type clipWaiter struct{ answer func(string, error) }

// clipboardQuery asks the terminal for the clipboard's text (OSC 52, the clipboard selection).
const clipboardQuery = "\x1b]52;c;?\x1b\\"

// ReadClipboard asks the terminal for the system clipboard's text (tui.ClipboardReader). It fails at
// once with tui.ErrClipboardUnsupported unless WithClipboardRead is on, and once the terminal has
// left a read unanswered; with ErrClosed on a stopped backend. answer runs at most once, on the
// input goroutine, never after cancel returns.
func (b *Backend) ReadClipboard(answer func(text string, err error)) (cancel func(), err error) {
	if b.stopped.Load() {
		return nil, ErrClosed
	}
	w := &clipWaiter{answer: answer}
	b.clipMu.Lock()
	if !b.cfg.clipRead || b.silent {
		b.clipMu.Unlock()
		return nil, tui.ErrClipboardUnsupported
	}
	b.clipWait = append(b.clipWait, w)
	b.clipMu.Unlock()
	if err := b.write([]byte(clipboardQuery)); err != nil {
		b.dropClipWaiter(w, false)
		return nil, err
	}
	return func() { b.dropClipWaiter(w, true) }, nil
}

// dropClipWaiter withdraws w; unanswered, a withdrawn read marks the terminal silent: it didn't
// answer in the time the App gave it.
func (b *Backend) dropClipWaiter(w *clipWaiter, unanswered bool) {
	b.clipMu.Lock()
	defer b.clipMu.Unlock()
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
