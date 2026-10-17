package widget

import (
	"cmp"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldFileViewKey is FileOpenView's key tests before commands — pathKey, filterKey, then movePart, in
// that order — verbatim but for the focus and filter conditions, all granted, and their effects,
// which are named by the command each now is.
func oldFileViewKey(k tui.KeyEvent) string {
	if k.Kind != tui.KeyRelease {
		switch {
		case k.Text == "~" && k.Mods&nonTextMods == 0:
			return "home"
		case k.Code == tui.KeyEscape:
			return "cancel"
		}
		switch {
		case k.Code == '/' && k.Mods == 0:
			return "filter"
		case k.Code == tui.KeyEnter:
			return "filterDone"
		case k.Code == tui.KeyEscape:
			return "cancel"
		}
	}
	if k.Mods != tui.ModCtrl {
		return ""
	}
	switch k.Code {
	case 'j':
		return "partDown"
	case 'k':
		return "partUp"
	case 'l':
		return "preview"
	case 'h':
		return "back"
	}
	return ""
}

// fileViewDeviation names what the key map changes on purpose for the chooser:
//   - Hyper or Meta held;
//   - Shift spent on the character it typed;
//   - a Ctrl letter with Shift, or a Ctrl capital with no text, reaching its Shift-less chord;
//   - a ~ with no text, read by its code;
//   - a lock bit no longer stopping / or a Ctrl move (the old tests compared raw Mods);
//   - a release no longer moving among the parts, which the old movePart, never testing the kind,
//     did a second time under a terminal that reports releases.
func fileViewDeviation(e tui.KeyEvent) string {
	switch {
	case e.Kind == tui.KeyRelease:
		return "a release no longer moves among the parts"
	case e.Mods&tui.LockMods != 0 && (e.Code == '/' || e.Mods&tui.ModCtrl != 0):
		return "a lock bit no longer stops / or a Ctrl move"
	}
	return cmp.Or(hyperOrMeta(e), shiftSpentOnPrintable(e), ctrlShiftLetter(e), capitalWithModifier(e),
		textlessCharacter(e))
}

func TestFileViewKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	v := &FileOpenView{}
	keyGridDiff(t, "FileOpenView", oldFileViewKey,
		func(e tui.KeyEvent) string {
			cmd, ok := v.Context().KeyCommand(e, fileViewCommands...)
			if !ok {
				return ""
			}
			return strings.TrimPrefix(cmd.ID, "tui.fileview.")
		},
		fileViewDeviation)
}
