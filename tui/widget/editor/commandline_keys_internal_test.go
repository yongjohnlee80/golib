package editor

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"

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

func TestCommandLineKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	for _, dropdown := range []bool{false, true} {
		widgettest.KeyGridDiff(t, map[bool]string{false: "CommandLine", true: "CommandLine, dropdown open"}[dropdown],
			widgettest.Presses(func(e tui.KeyEvent) string { return oldCmdlineKey(e, dropdown) }),
			widgettest.Presses(func(e tui.KeyEvent) string {
				back, ok := cmdlineStep(widgettest.NoKeymap, e, dropdown)
				if !ok {
					return ""
				}
				return map[bool]string{false: "next", true: "back"}[back]
			}),
			widgettest.HyperOrMeta)
	}
	widgettest.KeyGridDiff(t, "CommandLine replace question",
		widgettest.Presses(oldReplaceKey),
		widgettest.Presses(func(e tui.KeyEvent) string { return replaceAnswer(widgettest.NoKeymap, e).String() }),
		widgettest.HyperOrMeta)
}

// A replaceChoice past the answers says "unknown", never panicking.
func TestAReplaceChoicePastTheAnswersIsUnknown(t *testing.T) {
	if got := (replaceQuit + 1).String(); got != "unknown" {
		t.Fatalf("String() = %q, want unknown", got)
	}
}
