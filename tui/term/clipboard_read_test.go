package term

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/internal/vtparse"
)

// answered is one answer a read was given.
type answered struct {
	text string
	err  error
}

// clipRig is a backend with clipboard reads on (unless off), the writer it writes to, and a
// decoder wired as the input loop wires it, to feed the terminal's answers through.
func clipRig(t *testing.T, on bool) (*Backend, *countingWriter, *decoder) {
	t.Helper()
	w := &countingWriter{}
	opts := []Option{WithEnv(noEnv)}
	if on {
		opts = append(opts, WithClipboardRead())
	}
	b := newHarness(strings.NewReader(""), w, opts...)
	return b, w, &decoder{emit: func(tui.Event) {}, clip: b.clipboardAnswered}
}

// read starts a read whose answers land in got.
func read(t *testing.T, b *Backend, got *[]answered) func() {
	t.Helper()
	cancel, err := b.ReadClipboard(func(text string, err error) { *got = append(*got, answered{text, err}) })
	if err != nil {
		t.Fatalf("ReadClipboard: %v", err)
	}
	return cancel
}

func osc52(text, term string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + term
}

// Off by default: a read fails at once as unsupported, and nothing is written to the terminal.
func TestAClipboardReadIsOffByDefault(t *testing.T) {
	b, w, _ := clipRig(t, false)
	_, err := b.ReadClipboard(func(string, error) { t.Error("an answer to a refused read") })
	if !errors.Is(err, tui.ErrClipboardUnsupported) || w.String() != "" {
		t.Fatalf("a read with reads off: %v, wrote %q; want ErrClipboardUnsupported and nothing", err, w.String())
	}
}

// On, a read asks the terminal (OSC 52's query), and the terminal's answer, ended by BEL or by ST,
// is the clipboard's text; two reads take two answers in the order they asked.
func TestAClipboardReadTakesTheTerminalsAnswer(t *testing.T) {
	b, w, d := clipRig(t, true)
	var first, second []answered
	read(t, b, &first)
	if w.String() != clipboardQuery {
		t.Fatalf("a read wrote %q; want the query %q", w.String(), clipboardQuery)
	}
	read(t, b, &second)
	d.feedBytes([]byte(osc52("first ünïcode", "\x07")))
	d.feedBytes([]byte(osc52("second\nline", "\x1b\\")))
	if len(first) != 1 || first[0] != (answered{"first ünïcode", nil}) || len(second) != 1 || second[0] != (answered{"second\nline", nil}) {
		t.Fatalf("answers %+v then %+v; want the first read the first answer, the second the second", first, second)
	}
	d.feedBytes([]byte(osc52("nobody asked", "\x07")))
	if len(first)+len(second) != 2 {
		t.Fatalf("an answer with no read waiting was given: %+v %+v", first, second)
	}
}

// A clipboard far larger than an OSC string's usual cap arrives whole, split across reads of the
// input at any byte; one at the reader's cap may be cut, and fails as too large.
func TestALargeClipboardArrivesWhole(t *testing.T) {
	b, _, d := clipRig(t, true)
	var got []answered
	read(t, b, &got)
	big := strings.Repeat("0123456789abcdef", 8<<10) // 128 KiB
	seq := []byte(osc52(big, "\x1b\\"))
	for len(seq) > 0 {
		n := min(len(seq), 4093)
		d.feedBytes(seq[:n])
		seq = seq[n:]
	}
	if len(got) != 1 || got[0].err != nil || got[0].text != big {
		t.Fatalf("a 128 KiB clipboard: %d answers; want one, the text whole", len(got))
	}
	read(t, b, &got)
	d.feedBytes([]byte("\x1b]52;c;" + strings.Repeat("QUFB", vtparse.MaxClipboardData/4+10) + "\x07"))
	if len(got) != 2 || !errors.Is(got[1].err, ErrClipboardTooLarge) || got[1].text != "" {
		t.Fatalf("an answer past the cap: %v; want ErrClipboardTooLarge and no text", got[1:])
	}
}

// An answer that isn't base64 fails the read; the terminal's echo of the query answers none.
func TestABadAnswerFailsTheRead(t *testing.T) {
	b, _, d := clipRig(t, true)
	var got []answered
	read(t, b, &got)
	d.feedBytes([]byte("\x1b]52;c;?\x07"))
	if len(got) != 0 {
		t.Fatalf("the query's echo answered: %+v", got)
	}
	d.feedBytes([]byte("\x1b]52;c;not*base64\x07"))
	if len(got) != 1 || !errors.Is(got[0].err, ErrClipboardAnswer) {
		t.Fatalf("a bad answer: %+v; want ErrClipboardAnswer", got)
	}
}

// A read withdrawn unanswered (the App's timeout) marks the terminal silent: its late answer is
// dropped, and the reads after it fail at once as unsupported, writing nothing.
func TestASilentTerminalCostsOneWait(t *testing.T) {
	b, w, d := clipRig(t, true)
	var got []answered
	cancel := read(t, b, &got)
	cancel()
	cancel() // safe twice
	d.feedBytes([]byte(osc52("late", "\x07")))
	if len(got) != 0 {
		t.Fatalf("a withdrawn read was answered: %+v", got)
	}
	w.Reset()
	if _, err := b.ReadClipboard(func(string, error) {}); !errors.Is(err, tui.ErrClipboardUnsupported) || w.String() != "" {
		t.Fatalf("a read after a silent one: %v, wrote %q; want ErrClipboardUnsupported at once, nothing written", err, w.String())
	}
}

// A read answered and then withdrawn leaves the terminal answering; a stopped backend refuses.
func TestAnAnsweredReadLeavesTheTerminalAnswering(t *testing.T) {
	b, _, d := clipRig(t, true)
	var got []answered
	cancel := read(t, b, &got)
	d.feedBytes([]byte(osc52("x", "\x07")))
	cancel()
	read(t, b, &got)
	d.feedBytes([]byte(osc52("y", "\x07")))
	if len(got) != 2 || got[1].text != "y" {
		t.Fatalf("answers %+v; want x then y", got)
	}
	_ = b.Stop()
	if _, err := b.ReadClipboard(func(string, error) {}); err != ErrClosed {
		t.Fatalf("a read after Stop: %v; want ErrClosed", err)
	}
}
