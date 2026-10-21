package xterm

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/key"
)

// Every key the tables send reads back as the same key: decode(encode(k)) == k, for the letters,
// the tilde codes, the C0 keys and the Ctrl keys a decoder reads.
func TestKeysReadBackAsSent(t *testing.T) {
	for _, l := range letterKeys {
		f, ok := KeyLetter(l.Key)
		if k, ok2 := LetterKey(f); !ok || !ok2 || k != l.Key {
			t.Errorf("letter key %d: sent as %q, read as %d", l.Key, f, k)
		}
	}
	sent := 0
	for _, tk := range tildeKeys {
		if !tk.Sent {
			if k, ok := TildeKey(tk.Code); !ok || k != tk.Key {
				t.Errorf("CSI %d ~ reads as %d, want %d", tk.Code, k, tk.Key)
			}
			continue
		}
		sent++
		code, ok := TildeCode(tk.Key)
		if k, ok2 := TildeKey(code); !ok || !ok2 || k != tk.Key || code != tk.Code {
			t.Errorf("tilde key %d: sent as %d, read as %d", tk.Key, code, k)
		}
	}
	if sent != 12 { // Insert, Delete, PgUp, PgDn, F5–F12
		t.Errorf("%d keys are sent as CSI n ~, want 12", sent)
	}
	for _, c := range c0Keys {
		b, ok := C0Byte(c.Key)
		if k, ok2 := C0Key(b); !ok || !ok2 || k != c.Key {
			t.Errorf("C0 key %d: sent as %#x, read as %d", c.Key, b, k)
		}
	}
	for r := 'a'; r <= 'z'; r++ {
		for _, sent := range []rune{r, r - 'a' + 'A'} {
			b, ok := CtrlByte(sent)
			if k, ok2 := CtrlKey(b); !ok || !ok2 || k != r {
				t.Errorf("Ctrl+%c: sent as %#x, read as %q", sent, b, k)
			}
		}
	}
	for _, c := range ctrlPunct {
		b, ok := CtrlByte(c.Key)
		if !ok || b != c.Byte {
			t.Errorf("Ctrl+%q sent as %#x, want %#x", c.Key, b, c.Byte)
		}
		k, read := CtrlKey(b)
		if c.Read && (!read || k != c.Key) {
			t.Errorf("%#x reads as Ctrl+%q, want Ctrl+%q", b, k, c.Key)
		}
		if _, isC0 := C0Key(b); read && isC0 {
			t.Errorf("%#x reads as both a C0 key and Ctrl+%q", b, k)
		}
	}
}

// A modifier parameter reads back as the modifiers it was written for: kitty's for every modifier
// it carries, xterm's for Shift, Alt and Ctrl (xterm's meta, 8, reads as kitty's super).
func TestModifierParametersReadBack(t *testing.T) {
	kitty := key.Shift | key.Alt | key.Ctrl | key.Super | key.Hyper | key.Meta
	for m := key.Mods(0); m <= kitty; m++ {
		if m&^kitty != 0 {
			continue
		}
		if got := ParamMods(KittyModParam(m)); got != m {
			t.Errorf("kitty %b: parameter %d reads as %b", m, KittyModParam(m), got)
		}
	}
	for _, m := range []key.Mods{0, key.Shift, key.Alt, key.Ctrl, key.Shift | key.Alt | key.Ctrl} {
		if got := ParamMods(ModParam(m)); got != m {
			t.Errorf("xterm %b: parameter %d reads as %b", m, ModParam(m), got)
		}
	}
	if ModParam(key.Meta) != 9 || ParamMods(0) != 0 {
		t.Errorf("xterm meta is %d (want 9); parameter 0 reads as %b (want none)", ModParam(key.Meta), ParamMods(0))
	}
}

// A mouse report's button and modifiers read back as they were written.
func TestMouseReportsReadBack(t *testing.T) {
	for _, b := range mouseButtons {
		code, ok := MouseButtonCode(b)
		if got, ok2 := MouseButtonOf(code); !ok || !ok2 || got != b {
			t.Errorf("button %v: code %d reads as %v", b, code, got)
		}
	}
	if b, ok := MouseButtonOf(MouseNoButton); ok || b != tui.MouseNone {
		t.Errorf("the no-button code reads as %v", b)
	}
	for _, w := range wheelButtons {
		code, ok := WheelCode(w)
		if !ok || code&MouseWheelBit == 0 || WheelOf(code) != w {
			t.Errorf("wheel %v: code %d reads as %v", w, code, WheelOf(code))
		}
	}
	for _, m := range []key.Mods{0, key.Shift, key.Alt, key.Ctrl, key.Shift | key.Alt | key.Ctrl} {
		if got := MouseMods(MouseModBits(m)); got != m {
			t.Errorf("mouse mods %b: bits %d read as %b", m, MouseModBits(m), got)
		}
	}
	if MouseModBits(key.Meta) != MouseAltBit {
		t.Error("Meta is not reported as xterm's meta bit")
	}
}
