package widget

import "github.com/yongjohnlee80/golib/tui"

// THE EDITOR'S KEY DECLARATIONS. Each keyset's table is a list of declarations: a
// binding class, an action, its default chords, and whether it is an editing command. Its
// keymap.Commands (editorCommandsOf), the live table a key map binds (keymapFrom) and the legacy
// Keymap (VimKeymap, NanoKeymap, StandardKeymap: the projection) are all read from it.
//
// An editing command is LATE: in Insert it runs after the escape chord, Replace and the assists,
// where the raw editing keys ran, so a table's Tab still moves between cells. Every other binding
// is early and runs first, as every binding always has. The phase is the declaration's, never the
// action's: an overlay binding a motion is early.

// keyDecl is one command of a keyset's table.
type keyDecl struct {
	class  EditorMode // ModeNormal, ModeVisual or ModeInsert
	act    Action
	chords []KeyChord // its default keys, in the order they were declared
	late   bool       // an editing command
}

// keyDecls is a keyset's table: at most one declaration per class and action.
type keyDecls []keyDecl

// bind declares kc for act, early. A chord declared twice keeps the later action, as a map
// assignment would.
func (d *keyDecls) bind(kc KeyChord, act Action) { d.add(kc, act, false) }

func (d *keyDecls) add(kc KeyChord, act Action, late bool) {
	class := modeClass(kc.Mode)
	for i := range *d {
		x := &(*d)[i]
		for j := 0; j < len(x.chords); j++ {
			if x.chords[j] == kc {
				x.chords = append(x.chords[:j], x.chords[j+1:]...)
				j--
			}
		}
	}
	for i := range *d {
		// one declaration per class and action: editingDecls adds a late one only where the
		// table has none, so a chord joining one keeps its phase
		if x := &(*d)[i]; x.class == class && x.act == act {
			x.chords = append(x.chords, kc)
			return
		}
	}
	*d = append(*d, keyDecl{class: class, act: act, chords: []KeyChord{kc}, late: late})
}

// projection is the early declarations as a Keymap: the table the legacy constructors return.
func (d keyDecls) projection() Keymap {
	km := Keymap{}
	for _, x := range d {
		if !x.late {
			for _, kc := range x.chords {
				km[kc] = x.act
			}
		}
	}
	return km
}

// editingDecls adds the editing commands to a keyset's table: Insert's Tab, Enter, Backspace,
// Delete, Esc and caret keys, late, where the table does not already bind the action in Insert
// (Nano and Standard bind Home, End and the Page keys there, early). No Shift twin is declared:
// Shift reaches an editing command as a qualifier (resolveInsert).
func editingDecls(d keyDecls) keyDecls {
	has := map[Action]bool{}
	for _, x := range d {
		if x.class == ModeInsert {
			has[x.act] = true
		}
	}
	for _, e := range editingKeys {
		if !has[e.act] {
			d.add(KeyChord{Mode: ModeInsert, Code: e.code}, e.act, true)
		}
	}
	return d
}

// editingKeys are Insert's editing keys, each with the editing command it is.
var editingKeys = [...]struct {
	code rune
	act  Action
}{
	{tui.KeyTab, ActInsertTab}, {tui.KeyEnter, ActNewline}, {tui.KeyBackspace, ActBackspace},
	{tui.KeyDelete, ActDeleteForward}, {tui.KeyEscape, ActEscape},
	{tui.KeyLeft, ActLeft}, {tui.KeyRight, ActRight}, {tui.KeyUp, ActUp}, {tui.KeyDown, ActDown},
	{tui.KeyHome, ActLineStart}, {tui.KeyEnd, ActLineEnd}, {tui.KeyPageUp, ActPageUp}, {tui.KeyPageDown, ActPageDown},
}

// editingActOf is the editing command of Insert's editing key code.
func editingActOf(code rune) (Action, bool) {
	for _, e := range editingKeys {
		if e.code == code {
			return e.act, true
		}
	}
	return ActUnbound, false
}

// declsOf is keyset ks's whole table: its early bindings and its editing commands.
func declsOf(ks Keyset) keyDecls {
	switch normalizeKeyset(ks) {
	case KeysetNano:
		return editingDecls(nanoDecls())
	case KeysetStandard:
		return editingDecls(standardDecls())
	}
	return editingDecls(vimDecls())
}

// vimDecls declares the modal Vim keyset's early bindings: every binding but the editing
// commands (editingDecls).
func vimDecls() keyDecls {
	n := func(code rune) KeyChord { return KeyChord{Mode: ModeNormal, Code: code} }
	v := func(code rune) KeyChord { return KeyChord{Mode: ModeVisual, Code: code} }
	d := keyDecls{}

	// Motions (Normal + Visual): each bound once, here.
	mv := d.bind
	for _, m := range []EditorMode{ModeNormal, ModeVisual} {
		ch := func(code rune) KeyChord { return KeyChord{Mode: m, Code: code} }
		ctrl := func(code rune) KeyChord { return KeyChord{Mode: m, Code: code, Ctrl: true} }
		mv(ch('h'), ActLeft)
		mv(ch(tui.KeyLeft), ActLeft)
		mv(ch('l'), ActRight)
		mv(ch(tui.KeyRight), ActRight)
		mv(ch('k'), ActUp)
		mv(ch(tui.KeyUp), ActUp)
		mv(ch('j'), ActDown)
		mv(ch(tui.KeyDown), ActDown)
		mv(ch('0'), ActLineStart)
		mv(ch(tui.KeyHome), ActLineStart)
		mv(ch('$'), ActLineEnd)
		mv(ch(tui.KeyEnd), ActLineEnd)
		mv(ch('w'), ActWordForward)
		mv(ch('b'), ActWordBack)
		mv(ch('e'), ActWordEnd)
		mv(ch('}'), ActParaForward)
		mv(ch('{'), ActParaBack)
		mv(ch(']'), ActParaForward) // v1 aliases of } and {
		mv(ch('['), ActParaBack)
		mv(ch('G'), ActGoBottom)
		mv(ch('g'), ActGoPrefix)
		mv(ctrl('b'), ActPageUp)
		mv(ch(tui.KeyPageUp), ActPageUp)
		mv(ctrl('f'), ActPageDown)
		mv(ch(tui.KeyPageDown), ActPageDown)
		// line and character motions (Space stays the host's: AutoDoc's leader)
		mv(ch(tui.KeyEnter), ActLineDownFirst)
		mv(ch('+'), ActLineDownFirst)
		mv(ch(')'), ActSentenceForward)
		mv(ch('('), ActSentenceBack)
		mv(ch('-'), ActLineUpFirst)
		mv(ch('^'), ActFirstNonBlank)
		mv(ch(tui.KeyBackspace), ActLeftWrap)
		mv(ch('f'), ActFindCharForward)
		mv(ch('F'), ActFindCharBack)
		mv(ch('t'), ActTillCharForward)
		mv(ch('T'), ActTillCharBack)
		mv(ch(';'), ActFindCharRepeat)
		mv(ch(','), ActFindCharReverse)
		mv(ch('%'), ActMatchPair)
		mv(ch('H'), ActViewTop)
		mv(ch('M'), ActViewMiddle)
		mv(ch('L'), ActViewBottom)
		mv(ctrl('d'), ActHalfPageDown)
		mv(ctrl('u'), ActHalfPageUp)
		mv(ch('W'), ActWORDForward)
		mv(ch('B'), ActWORDBack)
		mv(ch('E'), ActWORDEnd)
	}

	// Normal-only commands.
	d.bind(n('i'), ActInsert)
	d.bind(n('a'), ActAppend)
	d.bind(n('I'), ActInsertLineStart)
	d.bind(n('A'), ActAppendLineEnd)
	d.bind(n('o'), ActOpenBelow)
	d.bind(n('O'), ActOpenAbove)
	d.bind(n('x'), ActDeleteChar)
	d.bind(n('D'), ActDeleteToEnd)
	d.bind(n('d'), ActDeletePrefix)
	d.bind(n('y'), ActYankPrefix)
	d.bind(n('c'), ActChangePrefix)
	d.bind(n('C'), ActChangeToEnd)
	d.bind(n('s'), ActSubstChar)
	d.bind(n('S'), ActSubstLine)
	d.bind(n('Y'), ActYankToEnd)
	d.bind(n('r'), ActReplaceChar)
	d.bind(n('J'), ActJoin)
	d.bind(n('~'), ActToggleCase)
	d.bind(n('>'), ActIndentPrefix)
	d.bind(n('<'), ActOutdentPrefix)
	d.bind(n('.'), ActRepeat)
	d.bind(n('"'), ActRegisterPrefix)
	d.bind(v('"'), ActRegisterPrefix)
	d.bind(n('m'), ActMarkSet)
	d.bind(v('m'), ActMarkSet)
	d.bind(n('\''), ActMarkLine)
	d.bind(v('\''), ActMarkLine)
	d.bind(n('`'), ActMarkExact)
	d.bind(v('`'), ActMarkExact)
	d.bind(n('q'), ActMacroRecord)
	d.bind(n('@'), ActMacroPlay)
	d.bind(n('*'), ActStarForward)
	d.bind(n('#'), ActStarBack)
	d.bind(n('R'), ActReplaceMode)
	d.bind(n('p'), ActPasteAfter)
	d.bind(n('P'), ActPasteBefore)
	d.bind(n('u'), ActUndo)
	d.bind(KeyChord{Mode: ModeNormal, Code: 'r', Ctrl: true}, ActRedo)
	d.bind(KeyChord{Mode: ModeInsert, Code: 'w', Ctrl: true}, ActDeleteWordBack)
	d.bind(KeyChord{Mode: ModeInsert, Code: 'u', Ctrl: true}, ActDeleteLineBack)
	d.bind(KeyChord{Mode: ModeInsert, Code: 'o', Ctrl: true}, ActInsertOneShot)
	d.bind(KeyChord{Mode: ModeNormal, Code: 'o', Ctrl: true}, ActJumpBack)
	d.bind(KeyChord{Mode: ModeNormal, Code: 'i', Ctrl: true}, ActJumpForward) // Tab isn't: in a terminal it is Ctrl-i, and focus's
	d.bind(n('n'), ActFindNext)
	d.bind(v('n'), ActFindNext)
	d.bind(n('N'), ActFindPrev)
	d.bind(v('N'), ActFindPrev)
	d.bind(n(':'), ActCommandLine)
	d.bind(v(':'), ActCommandLine)
	d.bind(n('v'), ActVisual)
	d.bind(n('V'), ActVisualLine)

	// Visual-only commands.
	d.bind(v('y'), ActVisualYank)
	d.bind(v('d'), ActVisualDelete)
	d.bind(v('x'), ActVisualDelete)
	d.bind(v('c'), ActVisualChange)
	d.bind(v('s'), ActVisualChange)
	d.bind(v('r'), ActVisualReplace)
	d.bind(v('~'), ActVisualToggleCase)
	d.bind(v('u'), ActVisualLower)
	d.bind(v('U'), ActVisualUpper)
	d.bind(v('>'), ActVisualIndent)
	d.bind(v('<'), ActVisualOutdent)
	d.bind(v('o'), ActVisualOther)
	d.bind(v('O'), ActVisualOtherCol)
	d.bind(v('I'), ActVisualInsert)
	d.bind(v('A'), ActVisualAppend)
	d.bind(KeyChord{Mode: ModeNormal, Code: 'v', Ctrl: true}, ActVisualBlock)
	d.bind(KeyChord{Mode: ModeVisual, Code: 'v', Ctrl: true}, ActVisualBlock)
	d.bind(v('v'), ActVisual)
	d.bind(v('V'), ActVisualLine)

	toggleRendered(&d)
	return d
}

// toggleRendered binds Ctrl+T to ActToggleRendered in every mode: unbound in all three keysets
// (Nano's line end is Ctrl+E), so no profile loses a key to it.
func toggleRendered(d *keyDecls) {
	for _, m := range []EditorMode{ModeNormal, ModeVisual, ModeInsert} {
		d.bind(KeyChord{Mode: m, Code: 't', Ctrl: true}, ActToggleRendered)
	}
}

// nanoDecls declares the non-modal Nano-style keyset's early bindings: every binding but the editing
// commands (editingDecls).
func nanoDecls() keyDecls {
	ins := func(code rune, ctrl bool) KeyChord {
		return KeyChord{Mode: ModeInsert, Code: code, Ctrl: ctrl}
	}
	vis := func(code rune, ctrl bool) KeyChord {
		return KeyChord{Mode: ModeVisual, Code: code, Ctrl: ctrl}
	}
	d := keyDecls{}
	d.bind(ins('k', true), ActCut)
	d.bind(ins('u', true), ActPaste)
	d.bind(ins('a', true), ActLineStart)
	d.bind(ins('e', true), ActLineEnd)
	d.bind(ins('y', true), ActPageUp)
	d.bind(ins('v', true), ActPageDown)
	d.bind(ins('z', true), ActUndo)
	d.bind(vis('k', true), ActVisualDelete)
	d.bind(ins(tui.KeyHome, false), ActLineStart)
	d.bind(ins(tui.KeyEnd, false), ActLineEnd)
	d.bind(ins(tui.KeyPageUp, false), ActPageUp)
	d.bind(ins(tui.KeyPageDown, false), ActPageDown)
	toggleRendered(&d)
	return d
}

// standardDecls declares the standard GUI / TextEdit-style keyset's early bindings: every binding but the editing
// commands (editingDecls).
func standardDecls() keyDecls {
	ins := func(code rune, ctrl bool) KeyChord {
		return KeyChord{Mode: ModeInsert, Code: code, Ctrl: ctrl}
	}
	vis := func(code rune, ctrl bool) KeyChord {
		return KeyChord{Mode: ModeVisual, Code: code, Ctrl: ctrl}
	}
	d := keyDecls{}
	d.bind(ins('z', true), ActUndo)
	d.bind(ins('y', true), ActRedo)
	d.bind(ins('x', true), ActCut)
	d.bind(ins('c', true), ActCopy)
	d.bind(ins('v', true), ActPaste)
	d.bind(ins('a', true), ActSelectAll)
	d.bind(vis('c', true), ActVisualYank)
	d.bind(vis('x', true), ActVisualDelete)
	d.bind(vis('a', true), ActSelectAll)
	d.bind(ins(tui.KeyHome, false), ActLineStart)
	d.bind(ins(tui.KeyEnd, false), ActLineEnd)
	d.bind(ins(tui.KeyPageUp, false), ActPageUp)
	d.bind(ins(tui.KeyPageDown, false), ActPageDown)
	toggleRendered(&d)
	return d
}
