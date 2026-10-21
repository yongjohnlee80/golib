package widget

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/internal/xterm"
	"github.com/yongjohnlee80/golib/tui/vt"
)

// encodeKey turns a decoded key back into the bytes xterm sends for it,
// under the program's cursor-key and keypad modes. It returns nil for a key
// with no encoding (a release, a lone modifier).
func encodeKey(k tui.KeyEvent, m vt.Modes) []byte {
	if k.Kind == tui.KeyRelease {
		return nil
	}
	mods := k.Mods.Chord()
	alt := mods&(tui.ModAlt|tui.ModMeta) != 0
	ctrl := mods&tui.ModCtrl != 0
	shift := mods&tui.ModShift != 0

	if seq := functionKey(k.Code, mods, m); seq != nil {
		return seq
	}
	if m.KeyboardFlags&vt.KittyDisambiguate != 0 {
		if seq := kittyKey(k, mods); seq != nil {
			return seq
		}
	}

	var b []byte
	switch c0, isC0 := xterm.C0Byte(k.Code); {
	case k.Code == tui.KeyTab && shift:
		return []byte("\x1b[Z")
	case k.Code == tui.KeyBackspace && ctrl:
		b = []byte{0x08}
	case isC0: // Enter, Tab, Backspace, Escape
		b = []byte{c0}
	default:
		if ctrl {
			c, ok := xterm.CtrlByte(k.Code)
			if !ok {
				return nil
			}
			b = []byte{c}
		} else if k.Text != "" {
			b = []byte(k.Text)
		} else if k.Code >= 0x20 && k.Code < 0xE000 {
			b = []byte(string(k.Code))
		} else {
			return nil
		}
	}
	if alt {
		return append([]byte{0x1b}, b...)
	}
	return b
}

// kittyKey is the kitty keyboard protocol's CSI code;mods u for a key whose
// legacy bytes are ambiguous, once the program has asked to disambiguate:
// Escape, Enter, Tab and Backspace with a modifier (Shift+Enter is not Enter),
// and a text key with Ctrl or Alt (Ctrl+i is not Tab, Alt+x is not Escape
// then x). It returns nil for every other key, which keeps its legacy bytes.
func kittyKey(k tui.KeyEvent, mods tui.Mods) []byte {
	code := k.Code // tui's key codes are kitty's own: Escape 27, Enter 13, Tab 9, Backspace 127
	switch k.Code {
	case tui.KeyEscape:
	case tui.KeyEnter, tui.KeyTab, tui.KeyBackspace:
		if mods == 0 {
			return nil
		}
	default:
		// Shift alone types the shifted text; any other modifier makes the key ambiguous.
		if mods&^tui.ModShift == 0 || k.Code < 0x20 || k.Code >= 0xE000 {
			return nil
		}
		// The key's own code, unshifted: Ctrl+Shift+A is a with Ctrl and Shift.
		code = unicode.ToLower(k.Code)
	}
	if mod := xterm.KittyModParam(mods); mod > 1 {
		return []byte("\x1b[" + strconv.Itoa(int(code)) + ";" + strconv.Itoa(mod) + "u")
	}
	return []byte("\x1b[" + strconv.Itoa(int(code)) + "u")
}

// functionKey encodes cursor, editing and function keys, or returns nil.
func functionKey(code rune, mods tui.Mods, m vt.Modes) []byte {
	mod := xterm.ModParam(mods)
	// Keys sent as CSI/SS3 + a final letter.
	if f, ok := xterm.KeyLetter(code); ok {
		if mod > 1 {
			return []byte("\x1b[1;" + strconv.Itoa(mod) + string(f))
		}
		fkey := code >= tui.KeyF1 && code <= tui.KeyF4
		if fkey || m.AppCursor {
			return []byte{0x1b, 'O', f}
		}
		return []byte{0x1b, '[', f}
	}
	// Keys sent as CSI n ~.
	if n, ok := xterm.TildeCode(code); ok {
		if mod > 1 {
			return []byte("\x1b[" + strconv.Itoa(n) + ";" + strconv.Itoa(mod) + "~")
		}
		return []byte("\x1b[" + strconv.Itoa(n) + "~")
	}
	return nil
}

// pasteEndMarker ends a bracketed paste (mode 2004).
const pasteEndMarker = xterm.PasteEnd

// encodePaste brackets a paste when the program asked for it (2004), and
// otherwise sends it as typed, with newlines as Enter.
func encodePaste(text string, m vt.Modes) []byte {
	if m.BracketedPaste {
		return []byte(xterm.PasteStart + withoutPasteEnd(text) + pasteEndMarker)
	}
	return []byte(strings.ReplaceAll(text, "\n", "\r"))
}

// withoutPasteEnd is text with every end marker removed, including one that removing another
// forms: a single strings.ReplaceAll leaves "\x1b[20" + marker + "1~" as a new marker, which would
// end the paste early and send the rest as typed keys. The marker has no prefix that is also its
// suffix, so dropping it whenever the output ends with it leaves none, in one pass.
func withoutPasteEnd(text string) string {
	if !strings.Contains(text, pasteEndMarker) {
		return text
	}
	out := make([]byte, 0, len(text))
	for i := 0; i < len(text); i++ {
		out = append(out, text[i])
		if n := len(out) - len(pasteEndMarker); n >= 0 && string(out[n:]) == pasteEndMarker {
			out = out[:n]
		}
	}
	return string(out)
}

// mouseTracker remembers the held button, which drag reporting needs.
type mouseTracker struct {
	held tui.MouseButton
}

// encode returns the report for a mouse event under the program's mouse
// mode, or nil when the mode does not report it.
func (t *mouseTracker) encode(e tui.MouseEvent, m vt.Modes) []byte {
	if m.Mouse == vt.MouseOff {
		return nil
	}
	var b int
	release := false
	switch e.Kind {
	case tui.MousePress:
		code, ok := xterm.MouseButtonCode(e.Button)
		if !ok {
			return nil
		}
		b = code
		t.held = e.Button
	case tui.MouseRelease:
		if m.Mouse == vt.MouseX10 {
			return nil
		}
		release = true
		b, _ = xterm.MouseButtonCode(t.held) // none held: 0, as before
		t.held = tui.MouseNone
	case tui.MouseWheel:
		code, ok := xterm.WheelCode(e.Button)
		if !ok {
			return nil
		}
		b = code
		if m.Mouse == vt.MouseX10 {
			return nil
		}
	case tui.MouseMotion:
		switch {
		case m.Mouse == vt.MouseMotion:
		case m.Mouse == vt.MouseDrag && t.held != tui.MouseNone:
		default:
			return nil
		}
		held, ok := xterm.MouseButtonCode(t.held)
		if !ok {
			held = xterm.MouseNoButton
		}
		b = xterm.MouseMotionBit + held
	default:
		return nil
	}
	if m.Mouse != vt.MouseX10 {
		b += xterm.MouseModBits(e.Mods)
	}
	x, y := e.X+1, e.Y+1
	if m.MouseSGR {
		final := byte('M')
		if release {
			final = 'm'
		}
		return []byte("\x1b[<" + strconv.Itoa(b) + ";" + strconv.Itoa(x) + ";" + strconv.Itoa(y) + string(final))
	}
	if release {
		b = xterm.MouseNoButton | (b &^ 3)
	}
	if x > 223 || y > 223 {
		return nil // the legacy encoding cannot say it
	}
	return []byte{0x1b, '[', 'M', byte(32 + b), byte(32 + x), byte(32 + y)}
}
