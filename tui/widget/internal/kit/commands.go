package kit

import (
	"slices"

	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// Cmd declares one command.
func Cmd(id string, scope keymap.Scope, keys ...string) keymap.Command {
	c := keymap.Command{ID: id, Scope: scope, Keys: tuikey.MustParse(keys...)}
	Declared = append(Declared, c)
	return c
}

// Declared is every key command a widget declares through Cmd, in the order they are initialized
// (tui/widget's files by name). tui/widget's KeyCommands lists them, so a new widget's commands are
// in keymap.Build without another list to edit.
var Declared []keymap.Command

// IsKey reports whether e is one of keys, by the one matching model (key.Of, then its candidates). A
// grammar's continuation is read with it: the second g, the key after the Terminal's prefix. They are
// the keys the grammar names, so a map moving the prefix moves the prefix alone.
func IsKey(e tui.KeyEvent, keys ...tuikey.Seq) bool {
	if e.Kind == tui.KeyRelease {
		return false
	}
	chord, ok := tuikey.Of(e.Code, e.Mods, e.Text)
	if !ok {
		return false
	}
	for _, c := range tuikey.Candidates(chord) {
		if slices.Contains(keys, c) {
			return true
		}
	}
	return false
}

// With is each text and its Shift form, for a functional key the old handlers took either way.
func With(texts ...string) []string {
	out := make([]string, 0, 2*len(texts))
	for _, t := range texts {
		out = append(out, t, "Shift+"+t)
	}
	return out
}

// Mods is key under every set of Shift, Ctrl, Alt and Super that keep accepts, Super always free: a
// handler that tested one modifier and ignored the rest took the key under all of them.
func Mods(key string, keep func(shift, ctrl, alt bool) bool) []string {
	var out []string
	for _, shift := range []bool{false, true} {
		for _, ctrl := range []bool{false, true} {
			for _, alt := range []bool{false, true} {
				if !keep(shift, ctrl, alt) {
					continue
				}
				m := map[bool]string{true: "Ctrl+"}[ctrl] + map[bool]string{true: "Alt+"}[alt]
				for _, super := range []string{"", "Super+"} {
					out = append(out, m+super+map[bool]string{true: "Shift+"}[shift]+key)
				}
			}
		}
	}
	return out
}

var AnyCombo = func(bool, bool, bool) bool { return true }
