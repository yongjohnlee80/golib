package widget

import (
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
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

	var b []byte
	switch k.Code {
	case tui.KeyEnter:
		b = []byte{'\r'}
	case tui.KeyTab:
		if shift {
			return []byte("\x1b[Z")
		}
		b = []byte{'\t'}
	case tui.KeyBackspace:
		if ctrl {
			b = []byte{0x08}
		} else {
			b = []byte{0x7f}
		}
	case tui.KeyEscape:
		b = []byte{0x1b}
	default:
		if ctrl {
			c, ok := ctrlByte(k.Code)
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

// ctrlByte is the C0 byte Ctrl+key sends.
func ctrlByte(r rune) (byte, bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return byte(r - 'a' + 1), true
	case r >= 'A' && r <= 'Z':
		return byte(r - 'A' + 1), true
	}
	switch r {
	case ' ', '@', '2':
		return 0x00, true
	case '[', '3':
		return 0x1b, true
	case '\\', '4':
		return 0x1c, true
	case ']', '5':
		return 0x1d, true
	case '^', '6':
		return 0x1e, true
	case '_', '-', '/', '7':
		return 0x1f, true
	case '?', '8':
		return 0x7f, true
	}
	return 0, false
}

// xtermMod is the modifier parameter xterm appends: 1 + shift(1) + alt(2) +
// ctrl(4) + meta(8). 1 means none.
func xtermMod(m tui.Mods) int {
	n := 1
	if m&tui.ModShift != 0 {
		n += 1
	}
	if m&tui.ModAlt != 0 {
		n += 2
	}
	if m&tui.ModCtrl != 0 {
		n += 4
	}
	if m&tui.ModMeta != 0 {
		n += 8
	}
	return n
}

// functionKey encodes cursor, editing and function keys, or returns nil.
func functionKey(code rune, mods tui.Mods, m vt.Modes) []byte {
	mod := xtermMod(mods)
	// Keys sent as CSI/SS3 + a final letter.
	letter := map[rune]byte{
		tui.KeyUp: 'A', tui.KeyDown: 'B', tui.KeyRight: 'C', tui.KeyLeft: 'D',
		tui.KeyHome: 'H', tui.KeyEnd: 'F',
		tui.KeyF1: 'P', tui.KeyF2: 'Q', tui.KeyF3: 'R', tui.KeyF4: 'S',
	}
	if f, ok := letter[code]; ok {
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
	tilde := map[rune]int{
		tui.KeyInsert: 2, tui.KeyDelete: 3, tui.KeyPageUp: 5, tui.KeyPageDown: 6,
		tui.KeyF5: 15, tui.KeyF6: 17, tui.KeyF7: 18, tui.KeyF8: 19,
		tui.KeyF9: 20, tui.KeyF10: 21, tui.KeyF11: 23, tui.KeyF12: 24,
	}
	if n, ok := tilde[code]; ok {
		if mod > 1 {
			return []byte("\x1b[" + strconv.Itoa(n) + ";" + strconv.Itoa(mod) + "~")
		}
		return []byte("\x1b[" + strconv.Itoa(n) + "~")
	}
	return nil
}

// encodePaste brackets a paste when the program asked for it (2004), and
// otherwise sends it as typed, with newlines as Enter.
func encodePaste(text string, m vt.Modes) []byte {
	if m.BracketedPaste {
		// A paste must not be able to end the bracket early.
		text = strings.ReplaceAll(text, "\x1b[201~", "")
		return []byte("\x1b[200~" + text + "\x1b[201~")
	}
	return []byte(strings.ReplaceAll(text, "\n", "\r"))
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
		switch e.Button {
		case tui.MouseLeft:
			b = 0
		case tui.MouseMiddle:
			b = 1
		case tui.MouseRight:
			b = 2
		default:
			return nil
		}
		t.held = e.Button
	case tui.MouseRelease:
		if m.Mouse == vt.MouseX10 {
			return nil
		}
		release = true
		b = map[tui.MouseButton]int{tui.MouseLeft: 0, tui.MouseMiddle: 1, tui.MouseRight: 2}[t.held]
		t.held = tui.MouseNone
	case tui.MouseWheel:
		switch e.Button {
		case tui.WheelUp:
			b = 64
		case tui.WheelDown:
			b = 65
		case tui.WheelLeft:
			b = 66
		case tui.WheelRight:
			b = 67
		default:
			return nil
		}
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
		b = 32 + map[tui.MouseButton]int{tui.MouseNone: 3, tui.MouseLeft: 0, tui.MouseMiddle: 1, tui.MouseRight: 2}[t.held]
	default:
		return nil
	}
	if m.Mouse != vt.MouseX10 {
		if e.Mods&tui.ModShift != 0 {
			b += 4
		}
		if e.Mods&(tui.ModAlt|tui.ModMeta) != 0 {
			b += 8
		}
		if e.Mods&tui.ModCtrl != 0 {
			b += 16
		}
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
		b = 3 | (b &^ 3)
	}
	if x > 223 || y > 223 {
		return nil // the legacy encoding cannot say it
	}
	return []byte{0x1b, '[', 'M', byte(32 + b), byte(32 + x), byte(32 + y)}
}
