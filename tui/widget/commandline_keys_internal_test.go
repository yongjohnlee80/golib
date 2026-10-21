package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldCmdlineKey is CommandLine.keys' decision before commands, verbatim but for its effect, named, and
// for whether the dropdown is open, its argument. The Modal hands it presses only.
func oldCmdlineKey(k tui.KeyEvent, dropdown bool) string {
	switch {
	case k.Code == tui.KeyTab:
		return map[bool]string{false: "next", true: "back"}[k.Mods&tui.ModShift != 0]
	case (k.Code == tui.KeyDown || k.Code == tui.KeyUp) && dropdown:
		return map[bool]string{false: "next", true: "back"}[k.Code == tui.KeyUp]
	}
	return ""
}

// oldReplaceKey is the replace question's key decision before commands, verbatim but for its effects.
func oldReplaceKey(k tui.KeyEvent) string {
	switch k.Code {
	case 'y', 'Y':
		return "yes"
	case 'n', 'N':
		return "no"
	case 'q', 'Q':
		return "quit"
	}
	return ""
}

// presses is the grid as the Modal hands it to its own keys: presses only.
func presses(f func(tui.KeyEvent) string) func(tui.KeyEvent) string {
	return func(e tui.KeyEvent) string {
		if e.Kind != tui.KeyPress {
			return ""
		}
		return f(e)
	}
}

func TestCommandLineKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	for _, dropdown := range []bool{false, true} {
		keyGridDiff(t, map[bool]string{false: "CommandLine", true: "CommandLine, dropdown open"}[dropdown],
			presses(func(e tui.KeyEvent) string { return oldCmdlineKey(e, dropdown) }),
			presses(func(e tui.KeyEvent) string {
				back, ok := cmdlineStep(noKeymap, e, dropdown)
				if !ok {
					return ""
				}
				return map[bool]string{false: "next", true: "back"}[back]
			}),
			hyperOrMeta)
	}
	keyGridDiff(t, "CommandLine replace question",
		presses(oldReplaceKey),
		presses(func(e tui.KeyEvent) string { return replaceAnswer(noKeymap, e) }),
		hyperOrMeta)
}
