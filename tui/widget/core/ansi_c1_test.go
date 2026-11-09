package widget

import "testing"

// TestSGRInterpDropsC1Controls: a C1 control (U+009B is CSI by itself) is dropped as C0 controls
// are, never passed on: what followed it is plain text, inert; a rune split across two chunks still
// comes out whole.
func TestSGRInterpDropsC1Controls(t *testing.T) {
	interp := &sgrInterp{passthrough: true}
	var text string
	emit := func(ev sgrEvent) {
		if ev.kind == sgrText {
			text += ev.text
		}
	}
	interp.feed([]byte("a\u009b2Jb\u0085c"), emit)
	if text != "a2Jbc" {
		t.Errorf("C1 in the text: %q, want %q", text, "a2Jbc")
	}
	text = ""
	e := []byte("é") // two bytes, fed one at a time
	interp.feed(e[:1], emit)
	interp.feed(e[1:], emit)
	if text != "é" {
		t.Errorf("a rune split across chunks: %q", text)
	}
}
