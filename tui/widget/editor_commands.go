package widget

import (
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// THE EDITOR'S KEY COMMANDS. Each keyset's binding class is a scope of its own,
// so Vim's and TextEdit's keys never conflict: "tui.editor.vim.normal", "tui.editor.vim.visual",
// "tui.editor.vim.insert", "tui.editor.standard.insert", "tui.editor.nano.insert". Every chord of the
// three base tables (VimKeymap, StandardKeymap, NanoKeymap) is a command's default, so a key map moves
// the editor's keys by id: "tui.editor.vim.normal.undo".
//
// The multi-key grammar stays code: a count, an operator's motion, a text object, f's character, a
// register's or a mark's name, and Esc cancelling whatever is pending. The FIRST key of each is a
// command (d, g, f, ", m), so a key map can move the prefix.

// EditorContextMenu opens the editor's context menu, in every keyset and mode.
var EditorContextMenu = cmd("tui.editor.contextMenu", "tui.editor", "Shift+F10", "Menu")

// editorScope is the scope of one keyset's binding class.
func editorScope(ks Keyset, class EditorMode) keymap.Scope {
	return keymap.Scope("tui.editor." + keysetID(ks) + "." + classID(class))
}

func keysetID(ks Keyset) string {
	switch normalizeKeyset(ks) {
	case KeysetNano:
		return "nano"
	case KeysetStandard:
		return "standard"
	}
	return "vim"
}

func classID(class EditorMode) string {
	switch modeClass(class) {
	case ModeVisual:
		return "visual"
	case ModeInsert:
		return "insert"
	}
	return "normal"
}

// editorCommand is one command and the table entry it stands for.
type editorCommand struct {
	cmd   keymap.Command // a named field, not embedded: an embedded Command would make every type of that name look embeddable
	class EditorMode
	act   Action
}

// editorCommandsOf is every command of keyset ks, derived from its base table once. A functional key's
// Shift twin is a default too: the editor's chord ignored Shift on a functional key before chords had
// Shift, so a table chord Down answered Shift+Down as well, and still does.
var editorCommandsOf = func() func(Keyset) []editorCommand {
	type built struct {
		once sync.Once
		cmds []editorCommand
	}
	tables := map[Keyset]*built{KeysetVim: {}, KeysetNano: {}, KeysetStandard: {}}
	base := map[Keyset]func() Keymap{KeysetVim: VimKeymap, KeysetNano: NanoKeymap, KeysetStandard: StandardKeymap}
	return func(ks Keyset) []editorCommand {
		ks = normalizeKeyset(ks)
		b := tables[ks]
		b.once.Do(func() { b.cmds = deriveEditorCommands(ks, base[ks]()) })
		return b.cmds
	}
}()

func deriveEditorCommands(ks Keyset, table Keymap) []editorCommand {
	type slot struct {
		class EditorMode
		act   Action
	}
	keys := map[slot][]tuikey.Seq{}
	for kc, act := range table {
		if act == ActUnbound {
			continue
		}
		sl := slot{modeClass(kc.Mode), act}
		s := seqOfChord(kc)
		keys[sl] = append(keys[sl], s)
		if twin := (tuikey.Seq{Code: s.Code, Mods: s.Mods | tuikey.Shift}); functionalCode(s.Code) && s.Mods&tuikey.Shift == 0 {
			keys[sl] = append(keys[sl], twin)
		}
	}
	out := make([]editorCommand, 0, len(keys))
	for sl, ks2 := range keys {
		slices.SortFunc(ks2, func(a, b tuikey.Seq) int { return strings.Compare(a.String(), b.String()) })
		ks2 = slices.Compact(ks2)
		id := "tui.editor." + keysetID(ks) + "." + classID(sl.class) + "." + lowerFirst(sl.act.String())
		out = append(out, editorCommand{
			cmd:   keymap.Command{ID: id, Scope: editorScope(ks, sl.class), Keys: ks2},
			class: sl.class,
			act:   sl.act,
		})
	}
	slices.SortFunc(out, func(a, b editorCommand) int { return strings.Compare(a.cmd.ID, b.cmd.ID) })
	return out
}

func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToLower(r[0])
	}
	return string(r)
}

// EditorCommands is every editor command of every keyset, and the context menu's.
func EditorCommands() []keymap.Command {
	out := []keymap.Command{EditorContextMenu}
	for _, ks := range []Keyset{KeysetVim, KeysetStandard, KeysetNano} {
		for _, c := range editorCommandsOf(ks) {
			out = append(out, c.cmd)
		}
	}
	return out
}

// keymapFrom is keyset ks's table as m binds it: each of its commands at m's keys (its defaults where m
// does not declare it). A command a scheme unbinds has no entry.
func keymapFrom(m *keymap.Map, ks Keyset) Keymap {
	out := Keymap{}
	for _, c := range editorCommandsOf(ks) {
		for _, s := range m.KeysOf(c.cmd) {
			out[chordOfSeq(c.class, s)] = c.act
		}
	}
	return out
}

// chordOfSeq is a key as the editor's chord. It is total: every key a key map can spell has a chord.
// A letter's Shift becomes its case, so a scheme's Shift+G and an old overlay's 'G' are one chord.
func chordOfSeq(class EditorMode, s tuikey.Seq) KeyChord {
	kc := KeyChord{
		Mode:  class,
		Code:  s.Code,
		Ctrl:  s.Mods&tuikey.Ctrl != 0,
		Alt:   s.Mods&tuikey.Alt != 0,
		Super: s.Mods&tuikey.Super != 0,
		Shift: s.Mods&tuikey.Shift != 0,
	}
	if unicode.IsLetter(kc.Code) && kc.Shift {
		kc.Code, kc.Shift = unicode.ToUpper(kc.Code), false
	}
	return kc
}

// seqOfChord is chordOfSeq's inverse, for the table chords a command's defaults are written from.
func seqOfChord(kc KeyChord) tuikey.Seq {
	s := tuikey.Seq{Code: kc.Code}
	if unicode.IsUpper(kc.Code) {
		s.Code, s.Mods = unicode.ToLower(kc.Code), tuikey.Shift
	}
	if kc.Ctrl {
		s.Mods |= tuikey.Ctrl
	}
	if kc.Alt {
		s.Mods |= tuikey.Alt
	}
	if kc.Super {
		s.Mods |= tuikey.Super
	}
	if kc.Shift {
		s.Mods |= tuikey.Shift
	}
	return s
}

// functionalCode reports whether code is a functional key (Tab, Enter, an arrow, F1…) rather than one
// that types a character.
func functionalCode(code rune) bool {
	return code < 0x20 || code == tui.KeyBackspace || (code >= 0xE000 && code <= 0xF8FF)
}

// keyBindings is the live table of the class the core is in, as a help card reads it: after the host
// overlay, so an overlay's replacement is listed (Source FromOverlay) and a chord it unbinds is not.
// The grammar after a prefix is not a binding and is not listed.
func (c *EditorCore) keyBindings(m *keymap.Map) []keymap.Binding {
	class := modeClass(c.keys.mode)
	byAct := map[Action][]KeyChord{}
	for kc, act := range c.keys.keymap {
		if modeClass(kc.Mode) == class {
			byAct[act] = append(byAct[act], kc)
		}
	}
	cmds := map[Action]keymap.Command{}
	for _, ec := range editorCommandsOf(c.keys.keyset) {
		if ec.class == class {
			cmds[ec.act] = ec.cmd
		}
	}
	scope := editorScope(c.keys.keyset, class)
	out := make([]keymap.Binding, 0, len(byAct))
	for act, chords := range byAct {
		cmd, ok := cmds[act]
		if !ok { // an overlay bound an action the keyset's table does not have here
			cmd = keymap.Command{ID: string(scope) + "." + lowerFirst(act.String()), Scope: scope}
		}
		src := m.SourceOf(cmd.ID)
		keys := make([]tuikey.Seq, 0, len(chords))
		for _, kc := range chords {
			keys = append(keys, seqOfChord(kc))
			if ov, in := c.keys.overlay[kc]; in && ov == act {
				src = keymap.Source{Kind: keymap.FromOverlay}
			}
		}
		slices.SortFunc(keys, func(a, b tuikey.Seq) int { return strings.Compare(a.String(), b.String()) })
		out = append(out, keymap.Binding{Command: cmd, Keys: keys, Scope: scope, Source: src})
	}
	slices.SortFunc(out, func(a, b keymap.Binding) int { return strings.Compare(a.Command.ID, b.Command.ID) })
	return out
}
