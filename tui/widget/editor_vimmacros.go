package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
)

// VIM MACROS — q{a–z} records into the register, q stops; q{A–Z} appends. @{a–z} plays it, @@ the
// last played, N@a N times. A macro is recorded as . records a change (editor_vimrepeat.go): Normal
// keys as keys, Insert as typed text, pasted text and keys, so the escape chord's tick and a paste
// play back as they happened. The register holds the macro's text too ("ap shows it), and its text
// is what counts: the atoms are a cache, kept only while the text is theirs encoded, and any other
// write to the register drops them, so @a then plays the text as keys, as Vim plays a register.
// Each change a macro makes is its own undo step, as in Vim. A macro playing itself, or one that
// plays it, stops at a depth of 100. On a viewer q and @ bubble: a macro there could change nothing,
// and the key is the host's (#285's rule).

// macroDepth is how deep macros may play one another.
const macroDepth = 100

// macroRecorder is the macro being recorded, and what plays.
type macroRecorder struct {
	reg     rune // the register being recorded into (A–Z appends); 0 for none
	atoms   []dotAtom
	playing int  // how many macros are playing, one inside another
	last    rune // the register last played, for @@
}

// macroAtom records a into the macro being recorded, unless a macro is playing: its keys are its
// own, and the @ that started it is recorded already.
func (c *EditorCore) macroAtom(a dotAtom) {
	if m := &c.macro; m.reg != 0 && m.playing == 0 {
		m.atoms = append(m.atoms, a)
	}
}

// macroKey records key k.
func (c *EditorCore) macroKey(k tui.KeyEvent) { c.macroAtom(dotAtom{kind: dotKey, key: k}) }

// macroRecordKey completes an armed q with the key's text: a–z starts a recording into that
// register, A–Z one appended to it; anything else cancels.
func (c *EditorCore) macroRecordKey(name rune) {
	if isMarkName(name) || name >= 'A' && name <= 'Z' {
		c.macro.reg, c.macro.atoms = name, nil
	}
}

// macroStop is q while recording: the recording, less the q that stops it, goes into its register.
func (c *EditorCore) macroStop() {
	m := &c.macro
	atoms := m.atoms
	if n := len(atoms); n > 0 && atoms[n-1].kind == dotKey {
		atoms = atoms[:n-1] // the q
	}
	c.reg.putMacro(m.reg, atoms)
	m.reg, m.atoms = 0, nil
}

// macroPlay is N@{reg} (@@ the last played): the register's atoms when they are its text's, else
// its text as keys, count times. A depth past macroDepth stops it.
func (c *EditorCore) macroPlay(name rune, count int) {
	m := &c.macro
	if name == '@' {
		name = m.last
	}
	e, ok := c.reg.get(name)
	if !ok || !e.holds() || name == 0 || m.playing >= macroDepth {
		return
	}
	m.last = name
	atoms := e.atoms
	if atoms == nil {
		atoms = decodeMacro(e.text)
	}
	m.playing++
	defer func() { m.playing-- }()
	for range max(count, 1) {
		for _, a := range atoms {
			switch a.kind {
			case dotKey:
				c.HandleKey(a.key)
			case dotTyped:
				c.typeText(a.text)
				c.dotTypedText(a.text) // typed in a macro, it is part of the change . repeats
			case dotPasted:
				c.HandlePaste(a.text)
			}
		}
	}
}

// putMacro writes a recording into register name (A–Z appends), as text and as atoms: the atoms
// are kept only while they are the whole text, so an append to a register holding text alone
// leaves it text alone. The unnamed register then holds it, as Vim's y_previous points at it.
func (r *editRegister) putMacro(name rune, atoms []dotAtom) {
	s, appending := r.slot(name)
	if s == nil {
		return
	}
	switch {
	case !appending:
		*s = regEntry{text: encodeMacro(atoms), atoms: atoms}
	case s.atoms != nil || !s.holds():
		all := append(append([]dotAtom(nil), s.atoms...), atoms...)
		*s = regEntry{text: encodeMacro(all), atoms: all}
	default:
		*s = regEntry{text: s.text + encodeMacro(atoms), linewise: s.linewise}
	}
	r.unnamed = *s
}

// macroKeyNames are the keys with no character, as a register shows them (Vim's <Left> names).
var macroKeyNames = map[rune]string{
	tui.KeyLeft: "<Left>", tui.KeyRight: "<Right>", tui.KeyUp: "<Up>", tui.KeyDown: "<Down>",
	tui.KeyHome: "<Home>", tui.KeyEnd: "<End>", tui.KeyDelete: "<Del>",
	tui.KeyPageUp: "<PageUp>", tui.KeyPageDown: "<PageDown>",
}

// encodeMacro is a recording as a register's text: typed and pasted text as it is, a key with a
// character as the character, Esc Enter Tab Backspace and Ctrl-letter as their control characters
// (as Vim's register holds them), and a key with none by its <Name>.
func encodeMacro(atoms []dotAtom) string {
	var b strings.Builder
	for _, a := range atoms {
		if a.kind != dotKey {
			b.WriteString(a.text)
			continue
		}
		k := a.key
		ctrl := k.Mods&tui.ModCtrl != 0
		switch {
		case ctrl && k.Code >= 'a' && k.Code <= 'z':
			b.WriteRune(k.Code - 'a' + 1)
		case k.Code == tui.KeyEscape:
			b.WriteByte(0x1b)
		case k.Code == tui.KeyEnter:
			b.WriteByte('\r')
		case k.Code == tui.KeyTab:
			b.WriteByte('\t')
		case k.Code == tui.KeyBackspace:
			b.WriteByte(0x08)
		case k.Text != "":
			b.WriteString(k.Text)
		default:
			b.WriteString(macroKeyNames[k.Code])
		}
	}
	return b.String()
}

// decodeMacro is a register's text as keys: each character a key, each control character its key
// (Esc, Enter for \r and \n, Tab, Backspace, Ctrl-letter). A <Name> is not read back: it plays as
// its characters, as Vim plays the bytes a register holds.
func decodeMacro(text string) []dotAtom {
	var atoms []dotAtom
	for _, r := range text {
		var k tui.KeyEvent
		switch {
		case r == 0x1b:
			k = tui.KeyEvent{Code: tui.KeyEscape}
		case r == '\r' || r == '\n':
			k = tui.KeyEvent{Code: tui.KeyEnter}
		case r == '\t':
			k = tui.KeyEvent{Code: tui.KeyTab}
		case r == 0x08:
			k = tui.KeyEvent{Code: tui.KeyBackspace}
		case r >= 1 && r <= 26:
			k = tui.KeyEvent{Code: 'a' + r - 1, Mods: tui.ModCtrl}
		default:
			k = tui.KeyEvent{Code: r, Text: string(r)}
		}
		atoms = append(atoms, dotAtom{kind: dotKey, key: k})
	}
	return atoms
}
