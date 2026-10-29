package tui

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// TestACellNeverHoldsAControl: whatever a widget writes, SetCell and Fill keep a cell drawable:
// each C0 control is its Control Picture (ESC is ␛), DEL is ␡, and a C1 control or a byte that is
// not UTF-8 is U+FFFD; text, wide or combined, is untouched. A terminal sequence written into cells
// (ESC]52, which sets the clipboard) reaches the diff with no control byte in it.
func TestACellNeverHoldsAControl(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b": "\u241b", "\x07": "\u2407", "\x00": "\u2400", "\r\n": "\u240d\u240a", "\x7f": "\u2421",
		"\u009b": "\ufffd", "\x9b": "\ufffd", "a": "a", "é": "é", "e\u0301": "e\u0301", "日": "日",
	} {
		root, buf := newTestSurface(4, 2, WidthPolicyDefault)
		root.SetCell(0, 0, in, style.New())
		root.Fill(Rect{X: 0, Y: 1, W: 2, H: 1}, in, style.New()) // two columns: a wide cluster fits
		for _, y := range []int{0, 1} {
			if got := cellAt(buf, 0, y).Content; got != want {
				t.Errorf("%q written at row %d: the cell holds %q, want %q", in, y, got, want)
			}
		}
	}

	root, buf := newTestSurface(40, 1, WidthPolicyDefault)
	for i, r := range "\x1b]52;c;cGF5bG9hZA==\x07" {
		root.SetCell(i, 0, string(r), style.New())
	}
	for _, u := range buf.diff() {
		if strings.ContainsFunc(u.Cell.Content, func(r rune) bool { return r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) }) {
			t.Errorf("the diff carries a control at %d: %q", u.X, u.Cell.Content)
		}
	}
}
