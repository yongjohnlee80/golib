package term

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// WriteClipboard sends OSC 52 in one write; a payload past the cap keeps its TAIL, the most recent
// lines; a stopped backend refuses.
func TestWriteClipboardSendsOSC52KeepingTheTail(t *testing.T) {
	w := &countingWriter{}
	b := newHarness(strings.NewReader(""), w, WithEnv(noEnv))
	if err := b.WriteClipboard([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("hello")) + "\x1b\\"; w.String() != want {
		t.Fatalf("wrote %q, want %q", w.String(), want)
	}

	w.Reset()
	big := append(bytes.Repeat([]byte("a"), 100), bytes.Repeat([]byte("z"), 72_000)...)
	if err := b.WriteClipboard(big); err != nil {
		t.Fatal(err)
	}
	out := w.String()
	payload, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(out, "\x1b]52;c;"), "\x1b\\"))
	if err != nil {
		t.Fatalf("the payload is not base64: %v", err)
	}
	if len(payload) != 72_000 || bytes.ContainsRune(payload, 'a') {
		t.Fatalf("an oversized payload sent %d bytes (any head: %v), want its last 72000", len(payload), bytes.ContainsRune(payload, 'a'))
	}

	_ = b.Stop()
	if err := b.WriteClipboard([]byte("x")); err != ErrClosed {
		t.Fatalf("WriteClipboard after Stop = %v, want ErrClosed", err)
	}
}
