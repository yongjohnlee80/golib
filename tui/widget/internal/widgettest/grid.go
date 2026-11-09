package widgettest

import "github.com/yongjohnlee80/golib/tui"

// Presses is the grid as the Modal hands it to its own keys: presses only.
func Presses(f func(tui.KeyEvent) string) func(tui.KeyEvent) string {
	return func(e tui.KeyEvent) string {
		if e.Kind != tui.KeyPress {
			return ""
		}
		return f(e)
	}
}

// CtrlShiftLetter is the deviation key.Candidates makes on purpose: a Ctrl or Alt letter with Shift
// held and no command of its own reaches the Shift-less chord, as a legacy terminal cannot tell them
// apart; the old test of the exact chord refused it.
func CtrlShiftLetter(e tui.KeyEvent) string {
	if e.Mods&tui.ModShift != 0 && e.Mods&(tui.ModCtrl|tui.ModAlt) != 0 && e.Code < 128 &&
		(e.Code >= 'a' && e.Code <= 'z' || e.Code >= 'A' && e.Code <= 'Z') {
		return "a Ctrl or Alt letter with Shift reaches its Shift-less chord"
	}
	return ""
}
