package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldTreeKey is Tree.handleKey's key decision at golib 6a0b271 for a tree with rows, verbatim but for
// its effects, which are named: the oracle of the no-map differential.
func oldTreeKey(e tui.KeyEvent) string {
	if e.Kind == tui.KeyRelease {
		return ""
	}
	if e.Mods&nonTextMods != 0 {
		return ""
	}
	code := e.Code
	if e.Text != "" {
		code = []rune(e.Text)[0]
	}
	switch code {
	case 'j', tui.KeyDown:
		return "down"
	case 'k', tui.KeyUp:
		return "up"
	case tui.KeyPageDown:
		return "pageDown"
	case tui.KeyPageUp:
		return "pageUp"
	case 'g', tui.KeyHome:
		return "first"
	case 'G', tui.KeyEnd:
		return "last"
	case 'l', tui.KeyRight:
		return "expand"
	case tui.KeyEnter:
		return "activate"
	case 'h', tui.KeyLeft:
		return "collapse"
	}
	return ""
}

var treeOp = map[string]string{
	TreeDown.ID: "down", TreeUp.ID: "up", TreePageDown.ID: "pageDown", TreePageUp.ID: "pageUp",
	TreeFirst.ID: "first", TreeLast.ID: "last", TreeExpand.ID: "expand", TreeActivate.ID: "activate",
	TreeCollapse.ID: "collapse",
}

func TestTreeKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	tr := &Tree{}
	keyGridDiff(t, "Tree", oldTreeKey,
		func(e tui.KeyEvent) string {
			cmd, ok := tr.keyCommand(e)
			if !ok {
				return ""
			}
			return treeOp[cmd.ID]
		},
		noTextShiftedPrintable)
}

func TestEveryTreeCommandIsNamed(t *testing.T) {
	for _, c := range treeCommands {
		if treeOp[c.ID] == "" {
			t.Errorf("%s has no name in treeOp", c.ID)
		}
	}
}
