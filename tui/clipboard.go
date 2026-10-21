package tui

import (
	"slices"
	"strings"
	"time"

	"github.com/yongjohnlee80/golib/errs"
)

// clipboardReads are the clipboard reads waiting for an answer, by ClipboardRead.ID.
type clipboardReads struct {
	clipReads  map[uint64]*clipRead
	nextClipID uint64
}

// ClipboardWriter is an OPTIONAL Backend capability: pushing text into the
// host terminal's system clipboard (OSC 52 on ANSI terminals). Backends that
// don't implement it simply don't support copy; callers must treat a false
// return from CopyToClipboard as "not supported / not delivered".
//
// OSC 52 is the only clipboard mechanism that works everywhere a terminal
// does — including over SSH and inside terminal multiplexers and editor
// :terminal windows — because the TERMINAL performs the copy, not the app.
type ClipboardWriter interface {
	// WriteClipboard sets the system clipboard to p (UTF-8 text).
	WriteClipboard(p []byte) error
}

// CopyToClipboard pushes s into the system clipboard via the backend's
// ClipboardWriter capability. Returns false when the backend doesn't support
// clipboard writes or the write failed. Loop goroutine only.
func (a *App) CopyToClipboard(s string) bool {
	cw := a.caps.clipW
	if cw == nil {
		return false
	}
	return cw.WriteClipboard([]byte(s)) == nil
}

// CopyToClipboard pushes s into the system clipboard (see App.CopyToClipboard).
func (c *Context) CopyToClipboard(s string) bool { return c.app.CopyToClipboard(s) }

// ClipboardReader is an OPTIONAL Backend capability: reading the system clipboard's text. A backend
// that can't read doesn't implement it, and App.ReadClipboard answers ErrClipboardUnsupported.
type ClipboardReader interface {
	// ReadClipboard asks for the clipboard's text. It returns an error when it can't ask, and then
	// never calls answer. Otherwise it calls answer at most once, from any goroutine, unless cancel
	// runs first: cancel withdraws the read (the App's timeout, or its stop), and answer is not
	// called after cancel returns. cancel is safe from any goroutine, and more than once.
	ReadClipboard(answer func(text string, err error)) (cancel func(), err error)
}

var (
	// ErrClipboardUnsupported is a read from a backend that can't read the clipboard.
	ErrClipboardUnsupported = errs.Sentinel(errs.ErrUnsupported, "tui: this backend can't read the clipboard")
	// ErrClipboardTimeout is a read the clipboard didn't answer in time.
	ErrClipboardTimeout = errs.Sentinel(errs.ErrTimeout, "tui: the clipboard didn't answer in time")
	// ErrClipboardClosed is a read still waiting when the App stopped.
	ErrClipboardClosed = errs.Sentinel(errs.ErrClosed, "tui: the app stopped before the clipboard answered")
)

// DefaultClipboardTimeout is how long a read waits when its caller gives no timeout.
const DefaultClipboardTimeout = 2 * time.Second

// ClipboardRead is one read: its ID tells its answer from another read's.
type ClipboardRead struct{ ID uint64 }

// clipRead is a read waiting on the loop: its callback, its deadline's cancel and the backend's.
type clipRead struct {
	done          func(ClipboardRead, string, error)
	stopDeadline  func()
	cancelBackend func()
}

// ReadClipboard reads the system clipboard's text. Loop goroutine only. It returns an error at once
// (ErrClipboardUnsupported with no reader, or the backend's) and done never runs. Otherwise done
// runs exactly once, on the loop and never inside this call: with the text (line breaks as a paste
// has them; "" for an empty clipboard, or one holding no text where the platform says so), with
// the backend's error, with ErrClipboardTimeout once timeout has passed (0:
// DefaultClipboardTimeout), or with ErrClipboardClosed if the App stops first. A timeout withdraws
// the backend's read, and an answer after it is dropped.
//
// The answer is the first the platform gives after the read was asked: when reads overlap it may be
// the platform's reply to an earlier one, still the clipboard's text. A caller that wants only its
// latest read compares the ClipboardRead it was given with the one done is called with.
func (a *App) ReadClipboard(timeout time.Duration, done func(read ClipboardRead, text string, err error)) (ClipboardRead, error) {
	cr := a.caps.clipR
	switch {
	case a.stoppingCalled: // teardown has answered the reads waiting; none after would be answered
		return ClipboardRead{}, ErrClipboardClosed
	case cr == nil:
		return ClipboardRead{}, ErrClipboardUnsupported
	}
	if timeout <= 0 {
		timeout = DefaultClipboardTimeout
	}
	a.clip.nextClipID++
	read := ClipboardRead{ID: a.clip.nextClipID}
	r := &clipRead{done: done}
	if a.clip.clipReads == nil {
		a.clip.clipReads = map[uint64]*clipRead{}
	}
	// registered before the backend is asked, so an answer the backend gives at once finds it
	a.clip.clipReads[read.ID] = r
	cancel, err := cr.ReadClipboard(func(text string, err error) {
		a.Update(func() { a.answerClipboard(read, NormalizeNewlines(text), err) })
	})
	if err != nil {
		delete(a.clip.clipReads, read.ID)
		return ClipboardRead{}, err
	}
	r.cancelBackend = cancel
	r.stopDeadline = a.addDeadline(timeout, func() {
		r.cancelBackend()
		a.answerClipboard(read, "", ErrClipboardTimeout)
	})
	return read, nil
}

// answerClipboard runs read's done once: the first answer, deadline or stop to arrive wins, and
// the others find nothing waiting.
func (a *App) answerClipboard(read ClipboardRead, text string, err error) {
	r, ok := a.clip.clipReads[read.ID]
	if !ok {
		return
	}
	delete(a.clip.clipReads, read.ID)
	r.stopDeadline()
	if err != nil {
		text = ""
	}
	r.done(read, text, err)
}

// closeClipboardReads answers every read still waiting with ErrClipboardClosed, withdrawing it
// from the backend, in the order they were asked. Teardown runs it.
func (a *App) closeClipboardReads() {
	ids := make([]uint64, 0, len(a.clip.clipReads))
	for id := range a.clip.clipReads {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if r, ok := a.clip.clipReads[id]; ok {
			r.cancelBackend()
			a.answerClipboard(ClipboardRead{ID: id}, "", ErrClipboardClosed)
		}
	}
}

// NormalizeNewlines makes text's line breaks \n: a \r\n pair first, so it is one break, then a lone
// \r. Pasted and clipboard text comes from sources that break lines differently (a terminal's
// bracketed paste, a browser, a window's drop or clipboard); each source normalizes it here, so a
// widget is always given \n.
func NormalizeNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// NewPasteEvent is the PasteEvent for text, its line breaks normalized (NormalizeNewlines): a
// backend builds its paste with it.
func NewPasteEvent(text string) PasteEvent { return PasteEvent{Text: NormalizeNewlines(text)} }

// ReadClipboard reads the system clipboard (see App.ReadClipboard). Loop goroutine only.
func (c *Context) ReadClipboard(timeout time.Duration, done func(read ClipboardRead, text string, err error)) (ClipboardRead, error) {
	return c.app.ReadClipboard(timeout, done)
}
