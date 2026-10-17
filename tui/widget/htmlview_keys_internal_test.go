package widget

import (
	"cmp"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldHTMLViewKey is HTMLView.key's decision before commands, verbatim but for its effects, which are
// named, and for whether a selection exists, its argument.
func oldHTMLViewKey(e tui.KeyEvent, hasSelection bool) string {
	if e.Kind == tui.KeyRelease {
		return ""
	}
	plain, ctrl := e.Mods.Chord() == 0, e.Mods.Chord() == tui.ModCtrl
	switch {
	case e.Code == tui.KeyUp, e.Code == 'k' && plain:
		return "up"
	case e.Code == tui.KeyDown, e.Code == 'j' && plain:
		return "down"
	case e.Code == tui.KeyPageUp:
		return "pageUp"
	case e.Code == tui.KeyPageDown || e.Code == ' ' && plain:
		return "pageDown"
	case e.Code == 'u' && ctrl:
		return "halfUp"
	case e.Code == 'd' && ctrl:
		return "halfDown"
	case e.Code == tui.KeyHome, e.Code == 'g' && plain:
		return "top"
	case e.Code == tui.KeyEnd, e.Code == 'G' && (plain || e.Mods.Chord() == tui.ModShift),
		e.Code == 'g' && e.Mods.Chord() == tui.ModShift:
		return "bottom"
	case e.Code == 'c' && e.Mods.Chord() == tui.ModCtrl, e.Code == 'y' && e.Mods.Chord() == 0:
		return "copy"
	case e.Code == tui.KeyEscape && hasSelection:
		return "clearSelection"
	}
	return ""
}

// ctrlShiftLetter is the deviation key.Candidates makes on purpose: a Ctrl or Alt letter with Shift
// held and no command of its own reaches the Shift-less chord, as a legacy terminal cannot tell them
// apart; the old test of the exact chord refused it.
func ctrlShiftLetter(e tui.KeyEvent) string {
	if e.Mods&tui.ModShift != 0 && e.Mods&(tui.ModCtrl|tui.ModAlt) != 0 && e.Code < 128 &&
		(e.Code >= 'a' && e.Code <= 'z' || e.Code >= 'A' && e.Code <= 'Z') {
		return "a Ctrl or Alt letter with Shift reaches its Shift-less chord"
	}
	return ""
}

func TestHTMLViewKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	v := &HTMLView{}
	for _, sel := range []bool{false, true} {
		keyGridDiff(t, map[bool]string{false: "HTMLView", true: "HTMLView, selecting"}[sel],
			func(e tui.KeyEvent) string { return oldHTMLViewKey(e, sel) },
			func(e tui.KeyEvent) string {
				cmd, ok := v.keyOf(e, sel)
				if !ok {
					return ""
				}
				return strings.TrimPrefix(cmd.ID, "tui.htmlview.")
			},
			func(e tui.KeyEvent) string {
				return cmp.Or(hyperOrMeta(e), shiftSpentOnPrintable(e), ctrlShiftLetter(e))
			})
	}
}
