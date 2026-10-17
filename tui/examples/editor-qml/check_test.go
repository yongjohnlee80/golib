package main

import (
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// TestEveryQMLFileIsSound lints what the program ships: editor.qml, the layout
// under the theme it does not import, and every dialog. A theme that lacks a
// role the layout reads fails here, not the day someone switches to it.
func TestEveryQMLFileIsSound(t *testing.T) {
	h := &Host{}
	decltest.Check(t, h.options(Options{Now: fixedNow, Tick: time.Hour})...)
}

// TestTheKeysAreSound checks the program's keys as a user's scheme would meet them: golib's commands,
// the layout's Shortcuts, and the menus' Alt+letters (decltest.CheckKeymap).
func TestTheKeysAreSound(t *testing.T) {
	h := &Host{}
	decltest.CheckKeymap(t, h.options(Options{Now: fixedNow, Tick: time.Hour})...)
}
