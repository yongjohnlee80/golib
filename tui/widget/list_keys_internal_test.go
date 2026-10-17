package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldListKey is List.HandleEvent's key handling at golib 6a0b271, verbatim but for its effects, which
// are named: the oracle of the no-map differential. IsContextMenuKey is inlined as it was, so a later
// change to it cannot move the oracle.
func oldListKey(e tui.KeyEvent, contextMenu, multi bool) string {
	if e.Kind == tui.KeyRelease {
		return ""
	}
	isContextMenuKey := func(k tui.KeyEvent) bool {
		if k.Kind != tui.KeyPress {
			return false
		}
		switch m := k.Mods.Chord(); {
		case k.Code == tui.KeyF10 && m == tui.ModShift:
			return true
		case k.Code == tui.KeyMenu && m == 0:
			return true
		}
		return false
	}
	if contextMenu && isContextMenuKey(e) {
		return "menu"
	}
	if e.Mods&nonTextMods != 0 {
		return ""
	}
	code := e.Code
	if e.Text != "" {
		code = []rune(e.Text)[0]
	}
	switch code {
	case 'k', tui.KeyUp:
		return "up"
	case 'j', tui.KeyDown:
		return "down"
	case 'g':
		return "first"
	case 'G':
		return "last"
	case tui.KeyPageUp:
		return "pageUp"
	case tui.KeyPageDown:
		return "pageDown"
	case tui.KeyHome:
		return "first"
	case tui.KeyEnd:
		return "last"
	case tui.KeyEnter:
		return "activate"
	case ' ':
		if !multi {
			return ""
		}
		return "toggle"
	}
	return ""
}

// listOp names what the converted List does with a command, as the oracle names it.
var listOp = map[string]string{
	ListMenu.ID: "menu", ListUp.ID: "up", ListDown.ID: "down", ListTop.ID: "first", ListHome.ID: "first",
	ListBottom.ID: "last", ListEnd.ID: "last", ListPageUp.ID: "pageUp", ListPageDown.ID: "pageDown",
	ListActivate.ID: "activate", ListToggle.ID: "toggle",
}

func TestListKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	for _, contextMenu := range []bool{false, true} {
		for _, multi := range []bool{false, true} {
			l := NewList(WithItems([]string{"a", "b"}, func(s string) string { return s }))
			l.contextMenu, l.multi = contextMenu, multi
			keyGridDiff(t, "List",
				func(e tui.KeyEvent) string { return oldListKey(e, contextMenu, multi) },
				func(e tui.KeyEvent) string {
					cmd, ok := l.keyCommand(e)
					if !ok {
						return ""
					}
					return listOp[cmd.ID]
				},
				noTextShiftedPrintable)
		}
	}
}

// Every List command has a name the oracle uses, so the differential cannot pass by omission.
func TestEveryListCommandIsNamed(t *testing.T) {
	for _, c := range listCommands {
		if listOp[c.ID] == "" {
			t.Errorf("%s has no name in listOp", c.ID)
		}
	}
}
