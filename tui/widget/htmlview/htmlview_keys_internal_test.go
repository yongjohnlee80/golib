package htmlview

import (
	"cmp"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/editor"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"

	"github.com/yongjohnlee80/golib/tui"
)

// oldHTMLViewKey is HTMLView.key's decision before commands, verbatim but for its effects, which are
// named, and for whether a selection exists, its argument; then the keys added with commands, each
// as it was written: the page navigation (pan, block and heading travel) and the keyset rule, under
// which only Vim (vim) reads the printing keys and a modeless keyset types them.
func oldHTMLViewKey(e tui.KeyEvent, hasSelection, vim bool) string {
	if e.Kind == tui.KeyRelease {
		return ""
	}
	chord := e.Mods.Chord()
	plain, ctrl := chord == 0, chord == tui.ModCtrl
	alt, altShift := chord == tui.ModAlt, chord == tui.ModAlt|tui.ModShift
	switch {
	case e.Code == tui.KeyUp && alt:
		return "previousBlock"
	case e.Code == tui.KeyDown && alt:
		return "nextBlock"
	case e.Code == tui.KeyUp && altShift:
		return "previousHeading"
	case e.Code == tui.KeyDown && altShift:
		return "nextHeading"
	case e.Code == tui.KeyUp && chord&tui.ModAlt == 0, vim && e.Code == 'k' && plain:
		return "up"
	case e.Code == tui.KeyDown && chord&tui.ModAlt == 0, vim && e.Code == 'j' && plain:
		return "down"
	case e.Code == tui.KeyPageUp:
		return "pageUp"
	case e.Code == tui.KeyPageDown || vim && e.Code == ' ' && plain:
		return "pageDown"
	case e.Code == 'u' && ctrl:
		return "halfUp"
	case e.Code == 'd' && ctrl:
		return "halfDown"
	case e.Code == tui.KeyHome, vim && e.Code == 'g' && plain:
		return "top"
	case e.Code == tui.KeyEnd, vim && e.Code == 'G' && (plain || chord == tui.ModShift),
		vim && e.Code == 'g' && chord == tui.ModShift:
		return "bottom"
	case e.Code == 'c' && chord == tui.ModCtrl, vim && e.Code == 'y' && chord == 0:
		return "copy"
	case e.Code == tui.KeyEscape && hasSelection:
		return "clearSelection"
	case e.Code == tui.KeyLeft && plain, vim && e.Code == 'h' && plain:
		return "left"
	case e.Code == tui.KeyRight && plain, vim && e.Code == 'l' && plain:
		return "right"
	case vim && e.Code == ']' && plain:
		return "nextBlock"
	case vim && e.Code == '[' && plain:
		return "previousBlock"
	case vim && e.Code == '}' && plain:
		return "nextHeading"
	case vim && e.Code == '{' && plain:
		return "previousHeading"
	}
	return ""
}

func TestHTMLViewKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	for _, ks := range []editor.Keyset{editor.KeysetVim, editor.KeysetStandard, editor.KeysetNano} {
		v := &HTMLView{}
		v.FollowKeyset(func() editor.Keyset { return ks })
		for _, sel := range []bool{false, true} {
			name := map[bool]string{false: "HTMLView, ", true: "HTMLView selecting, "}[sel] + ks.String()
			widgettest.KeyGridDiff(t, name,
				func(e tui.KeyEvent) string { return oldHTMLViewKey(e, sel, ks == editor.KeysetVim) },
				func(e tui.KeyEvent) string {
					cmd, ok := v.keyOf(e, sel)
					if !ok {
						return ""
					}
					return strings.TrimPrefix(cmd.ID, "tui.htmlview.")
				},
				func(e tui.KeyEvent) string {
					return cmp.Or(widgettest.HyperOrMeta(e), widgettest.ShiftSpentOnPrintable(e), widgettest.CtrlShiftLetter(e))
				})
		}
	}
}
