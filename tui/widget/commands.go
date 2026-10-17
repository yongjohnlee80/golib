package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// KEY COMMANDS — every key a golib widget acts on, declared as data (ADR 1792255248 §4.6). A widget
// asks its Context which of its own commands a key is (tui.Context.KeyCommand); a key-mapping scheme
// changes their keys by id. Each widget's defaults reproduce the keys its handler took before
// commands existed, written out key by key: where the old handler let Shift through to a functional
// key, the Shift form is a default too.
//
// A label's mnemonic (&File, a button's underlined letter) is not a command and cannot be rebound.

// cmd declares one command.
func cmd(id string, scope keymap.Scope, keys ...string) keymap.Command {
	return keymap.Command{ID: id, Scope: scope, Keys: tuikey.MustParse(keys...)}
}

// with is each text and its Shift form, for a functional key the old handlers took either way.
func with(texts ...string) []string {
	out := make([]string, 0, 2*len(texts))
	for _, t := range texts {
		out = append(out, t, "Shift+"+t)
	}
	return out
}

// ListScope is a List's keys.
const ListScope keymap.Scope = "tui.list"

var (
	ListUp       = cmd("tui.list.up", ListScope, append(with("Up"), "K")...)
	ListDown     = cmd("tui.list.down", ListScope, append(with("Down"), "J")...)
	ListTop      = cmd("tui.list.top", ListScope, "G")
	ListBottom   = cmd("tui.list.bottom", ListScope, "Shift+G")
	ListPageUp   = cmd("tui.list.pageUp", ListScope, with("PgUp")...)
	ListPageDown = cmd("tui.list.pageDown", ListScope, with("PgDown")...)
	ListHome     = cmd("tui.list.home", ListScope, with("Home")...)
	ListEnd      = cmd("tui.list.end", ListScope, with("End")...)
	ListActivate = cmd("tui.list.activate", ListScope, with("Enter")...)
	ListToggle   = cmd("tui.list.toggle", ListScope, "Space") // multi-select only
	ListMenu     = cmd("tui.list.contextMenu", ListScope, "Shift+F10", "Menu")

	listCommands = []keymap.Command{ListMenu, ListUp, ListDown, ListTop, ListBottom, ListPageUp,
		ListPageDown, ListHome, ListEnd, ListActivate, ListToggle}
)

// KeyCommands is every key command golib declares: the runtime's and every widget's, for keymap.Build.
// (Commands is the Editor command line's registry; these are keys.)
func KeyCommands() []keymap.Command {
	out := append([]keymap.Command(nil), tui.Commands()...)
	out = append(out, listCommands...)
	return out
}
