package decl

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/tui"
)

// SHORTCUTS.
//
//	Window {
//	    Shortcut { sequence: "Ctrl+Q"; onActivated: App.exit() }
//	}
//
// QML's own type, spelled as Qt spells it: a `sequence` and an `onActivated`.
// A Shortcut is DATA, like a menu row — the Window that contains it owns the
// keys, because a shortcut has to fire whichever widget holds focus, and only
// an ancestor of every widget sees every key.

// keySequence is one key and its modifiers.
type keySequence struct {
	code rune
	mods tui.Mods
	text string // as written, for diagnostics
}

// shortcutMods are the modifiers a sequence can name. Lock keys are not among
// them: a shortcut that stopped working because Num Lock was on would be
// indistinguishable from one that was never bound.
const shortcutMods = tui.ModShift | tui.ModAlt | tui.ModCtrl

var modifierNames = map[string]tui.Mods{
	"ctrl": tui.ModCtrl, "control": tui.ModCtrl,
	"alt": tui.ModAlt, "meta": tui.ModAlt,
	"shift": tui.ModShift,
}

// keyNames maps a named key, lower-cased, onto the code the terminal decoder
// delivers. A new key is a row here.
var keyNames = map[string]rune{
	"esc": tui.KeyEscape, "escape": tui.KeyEscape,
	"enter": tui.KeyEnter, "return": tui.KeyEnter,
	"tab": tui.KeyTab, "backspace": tui.KeyBackspace, "space": ' ',
	"up": tui.KeyUp, "down": tui.KeyDown, "left": tui.KeyLeft, "right": tui.KeyRight,
	"home": tui.KeyHome, "end": tui.KeyEnd,
	"pgup": tui.KeyPageUp, "pageup": tui.KeyPageUp,
	"pgdown": tui.KeyPageDown, "pagedown": tui.KeyPageDown,
	"ins": tui.KeyInsert, "insert": tui.KeyInsert,
	"del": tui.KeyDelete, "delete": tui.KeyDelete,
	"f1": tui.KeyF1, "f2": tui.KeyF2, "f3": tui.KeyF3, "f4": tui.KeyF4,
	"f5": tui.KeyF5, "f6": tui.KeyF6, "f7": tui.KeyF7, "f8": tui.KeyF8,
	"f9": tui.KeyF9, "f10": tui.KeyF10, "f11": tui.KeyF11, "f12": tui.KeyF12,
}

// parseSequence reads Qt's spelling of a key sequence: `Ctrl+Q`, `Alt+F`,
// `F10`, `Ctrl+Shift+S`. Names are case-insensitive, as they are in Qt.
func parseSequence(s string) (keySequence, error) {
	seq := keySequence{text: s}
	parts := strings.Split(s, "+")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		last := i == len(parts)-1
		if p == "" {
			return keySequence{}, fmt.Errorf("the key sequence %q has an empty part", s)
		}
		if !last {
			m, ok := modifierNames[strings.ToLower(p)]
			if !ok {
				return keySequence{}, fmt.Errorf("%q in %q is not a modifier (Ctrl, Alt or Shift)", p, s)
			}
			seq.mods |= m
			continue
		}
		if code, ok := keyNames[strings.ToLower(p)]; ok {
			seq.code = code
			continue
		}
		r, size := utf8.DecodeRuneInString(p)
		if size != len(p) {
			return keySequence{}, fmt.Errorf("%q in %q is not a key name or a single character", p, s)
		}
		// A letter is matched as the terminal delivers it — Ctrl+Q arrives as
		// 'q' with Ctrl — so it is stored lower-case.
		seq.code = unicode.ToLower(r)
	}
	return seq, nil
}

// matches reports whether a key event is this sequence.
//
// A LETTER'S CASE IS ITS SHIFT, as in Qt, where "C" is the key and "Shift+C"
// the capital. A terminal sends a capital as the character itself, with no
// Shift, so a capital letter with neither Ctrl nor Alt is read as Shift and the
// letter: "C" matches c, and "Shift+C" matches C. With Ctrl or Alt a terminal
// cannot be relied on to report case, and the letter matches either way.
func (k keySequence) matches(ev tui.KeyEvent) bool {
	if ev.Kind != tui.KeyPress {
		return false
	}
	code, mods := ev.Code, ev.Mods&shortcutMods
	if unicode.IsUpper(code) {
		if mods&(tui.ModCtrl|tui.ModAlt) == 0 {
			mods |= tui.ModShift
		}
		code = unicode.ToLower(code)
	}
	if mods&(tui.ModCtrl|tui.ModAlt) != 0 && k.mods&tui.ModShift == 0 {
		mods &^= tui.ModShift // Ctrl+C and Ctrl+Shift+C are one to a terminal
	}
	return code == k.code && mods == k.mods
}

// shortcutNode is a declared Shortcut, before a Window adopts it.
type shortcutNode struct {
	seq     keySequence
	trigger func()
}

func (*shortcutNode) Init(*tui.Context)               {}
func (*shortcutNode) Layout(tui.Constraints) tui.Size { return tui.Size{} }
func (*shortcutNode) declarationOnly()                {}
func (*shortcutNode) Render(tui.Surface)              {}
func (*shortcutNode) HandleEvent(tui.Event) bool      { return false }

func buildShortcut(b Build) (tui.Component, []string, error) {
	var text string
	consumed, err := readProps(b.Props, map[string]field{"sequence": into(&text, stringOf)})
	if err != nil {
		return nil, nil, err
	}
	if text == "" {
		return nil, nil, fmt.Errorf("Shortcut needs a sequence (at %s)", b.Pos)
	}
	seq, err := parseSequence(text)
	if err != nil {
		return nil, nil, fmt.Errorf("%w (at %s)", err, b.Pos)
	}
	return &shortcutNode{seq: seq, trigger: b.Emitter("activated")}, consumed, nil
}
