package widget

import "unicode"

// EDIT REGISTERS — a text widget's registers, as Vim keeps them: the unnamed register (what the
// last yank or delete left), a–z (A–Z append), 0 (the last yank no register was named for), 1–9
// (deletes of a line or more, newest first), - (smaller deletes), _ (the black hole), and + and *
// (what golib last wrote to the system clipboard: it can't read one back yet). Each holds text and
// whether it is whole lines; and whether yanking is allowed at all. Exporting to the system
// clipboard is the widget's (it holds the Context); the registers only keep the text.
//
// Which registers a yank or a delete writes is Vim's (register.c get_yank_register, ops.c
// op_delete): a yank writes the register named, else 0; a delete writes the register named, then 1
// (shifting 1–8 down) when it is linewise, spans lines or used a motion Vim lists for it, else -
// when no register was named. The unnamed register is whichever was written last, as Vim's
// y_previous points at it.

// regEntry is one register's content. atoms is a recorded macro's (editor_vimmacros.go), kept
// only while text is their encoding: every other write makes a new entry without them.
type regEntry struct {
	text     string
	linewise bool
	atoms    []dotAtom
}

// holds reports whether a put would insert anything: text, or an empty line-wise yank.
func (e regEntry) holds() bool { return e.text != "" || e.linewise }

// appended is e with more appended, as "A–"Z do: whole lines when either part is.
func (e regEntry) appended(more regEntry) regEntry {
	switch {
	case !e.holds():
		return more
	case e.linewise || more.linewise:
		return regEntry{text: e.text + "\n" + more.text, linewise: true}
	}
	return regEntry{text: e.text + more.text}
}

type editRegister struct {
	unnamed  regEntry
	named    [26]regEntry // a–z
	numbered [10]regEntry // 0, then 1–9
	small    regEntry     // -
	clip     [2]regEntry  // +, *
	yank     bool         // yanking is allowed: copies reach the registers and the system clipboard
}

// set fills the unnamed register, as SetRegister does.
func (r *editRegister) set(text string, linewise bool) {
	r.unnamed = regEntry{text: text, linewise: linewise}
}

// content is what the unnamed register holds.
func (r *editRegister) content() (text string, linewise bool) {
	return r.unnamed.text, r.unnamed.linewise
}

// holds reports whether a put from the unnamed register would insert anything.
func (r *editRegister) holds() bool { return r.unnamed.holds() }

// yankAllowed reports whether yanking is on.
func (r *editRegister) yankAllowed() bool { return r.yank }

// validRegister reports whether name names a register: " a–z A–Z 0–9 - _ + *.
func validRegister(name rune) bool {
	switch {
	case name == '"', name == '-', name == '_', name == '+', name == '*':
		return true
	case name >= '0' && name <= '9', name >= 'a' && name <= 'z', name >= 'A' && name <= 'Z':
		return true
	}
	return false
}

// slot is the register a write to name fills (A–Z the same as a–z), and whether it appends; nil
// for the unnamed register (0 or "), the black hole and anything else.
func (r *editRegister) slot(name rune) (s *regEntry, appending bool) {
	switch {
	case name >= 'a' && name <= 'z':
		return &r.named[name-'a'], false
	case name >= 'A' && name <= 'Z':
		return &r.named[unicode.ToLower(name)-'a'], true
	case name >= '0' && name <= '9':
		return &r.numbered[name-'0'], false
	case name == '-':
		return &r.small, false
	case name == '+':
		return &r.clip[0], false
	case name == '*':
		return &r.clip[1], false
	}
	return nil, false
}

// get is register name's content; 0 or " is the unnamed register. ok is false for the black hole
// and for a name that is no register.
func (r *editRegister) get(name rune) (e regEntry, ok bool) {
	if name == 0 || name == '"' {
		return r.unnamed, true
	}
	if s, _ := r.slot(name); s != nil {
		return *s, true
	}
	return regEntry{}, false
}

// put writes e into register name, appending for A–Z; the unnamed register then holds what it
// holds. It reports false for the unnamed register, the black hole and a name that is no register.
func (r *editRegister) put(name rune, e regEntry) bool {
	s, appending := r.slot(name)
	if s == nil {
		return false
	}
	if appending {
		*s = s.appended(e)
	} else {
		*s = e
	}
	r.unnamed = *s
	return true
}

// setClip keeps e as + or *'s content, what was last read from the system clipboard, without
// touching the unnamed register: a put reads it, it isn't a yank.
func (r *editRegister) setClip(name rune, e regEntry) {
	if s, _ := r.slot(name); s != nil {
		*s = e
	}
}

// yanked records a yank into register name (0: none named): that register, else 0; the unnamed
// register follows. The black hole keeps nothing.
func (r *editRegister) yanked(name rune, e regEntry) {
	switch name {
	case '_':
	case 0, '"':
		r.numbered[0], r.unnamed = e, e
	default:
		r.put(name, e)
	}
}

// deleted records a delete into register name (0: none named). big is a delete of a line or more,
// or one over a motion Vim keeps in 1 (%, (, ), `, /, ?, n, N, {, }): it shifts 1–8 down to 2–9 and
// fills 1, as the unnamed register then is, unless it appended to A–Z. A smaller delete with no
// register named fills -. The black hole keeps nothing, and the unnamed register keeps what it had.
func (r *editRegister) deleted(name rune, e regEntry, big bool) {
	if name == '_' {
		return
	}
	named := name != 0 && name != '"'
	appending := false
	if named {
		r.put(name, e)
		_, appending = r.slot(name)
	}
	switch {
	case big:
		copy(r.numbered[2:], r.numbered[1:9])
		r.numbered[1] = e
		if !appending {
			r.unnamed = e
		}
	case !named:
		r.small, r.unnamed = e, e
	}
}
