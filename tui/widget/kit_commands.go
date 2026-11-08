package widget

import (
	"slices"

	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// cmd declares one command.
func cmd(id string, scope keymap.Scope, keys ...string) keymap.Command {
	c := keymap.Command{ID: id, Scope: scope, Keys: tuikey.MustParse(keys...)}
	declared = append(declared, c)
	return c
}

// declared is every key command a widget declares through cmd, in the order the package initializes
// them (its files by name). KeyCommands lists them, so a new widget's commands are in keymap.Build
// without another list to edit.
var declared []keymap.Command

// isKey reports whether e is one of keys, by the one matching model (key.Of, then its candidates). A
// grammar's continuation is read with it: the second g, the key after the Terminal's prefix. They are
// the keys the grammar names, so a map moving the prefix moves the prefix alone.
func isKey(e tui.KeyEvent, keys ...tuikey.Seq) bool {
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

// with is each text and its Shift form, for a functional key the old handlers took either way.
func with(texts ...string) []string {
	out := make([]string, 0, 2*len(texts))
	for _, t := range texts {
		out = append(out, t, "Shift+"+t)
	}
	return out
}

// mods is key under every set of Shift, Ctrl, Alt and Super that keep accepts, Super always free: a
// handler that tested one modifier and ignored the rest took the key under all of them.
func mods(key string, keep func(shift, ctrl, alt bool) bool) []string {
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

var anyCombo = func(bool, bool, bool) bool { return true }
