package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldTextAreaKey is TextArea.handleKey's decision before commands, verbatim but for its effects,
// which are named: the oracle of the no-map differential.
func oldTextAreaKey(e tui.KeyEvent) string {
	if e.Kind == tui.KeyRelease {
		return ""
	}
	shift := e.Mods&tui.ModShift != 0
	ctrl := e.Mods&tui.ModCtrl != 0
	word := e.Mods&(tui.ModCtrl|tui.ModAlt) != 0
	sel := func(name string) string {
		if shift {
			return "select" + strings.ToUpper(name[:1]) + name[1:]
		}
		return name
	}

	switch e.Code {
	case tui.KeyEnter:
		return "newline"
	case tui.KeyBackspace:
		return "backspace"
	case tui.KeyDelete:
		return "delete"
	case tui.KeyLeft:
		if word {
			return sel("wordLeft")
		}
		return sel("left")
	case tui.KeyRight:
		if word {
			return sel("wordRight")
		}
		return sel("right")
	case tui.KeyUp:
		return sel("up")
	case tui.KeyDown:
		return sel("down")
	case tui.KeyPageUp:
		return sel("pageUp")
	case tui.KeyPageDown:
		return sel("pageDown")
	case tui.KeyHome:
		if ctrl {
			return sel("start")
		}
		return sel("home")
	case tui.KeyEnd:
		if ctrl {
			return sel("finish")
		}
		return sel("end")
	}

	if ctrl {
		switch e.Code {
		case 'a':
			return "lineStart"
		case 'e':
			return "lineEnd"
		case 'u':
			return "killLine"
		case 'w':
			return "killWord"
		}
		return ""
	}

	if e.Text != "" && e.Mods&nonTextMods == 0 && e.Code != tui.KeyTab {
		return "type"
	}
	return ""
}

func TestTextAreaKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	ta := &TextArea{}
	keyGridDiff(t, "TextArea", oldTextAreaKey,
		func(e tui.KeyEvent) string {
			if e.Kind == tui.KeyRelease {
				return ""
			}
			if cmd, ok := ta.keyCommand(e); ok {
				return strings.TrimPrefix(cmd.ID, "tui.textarea.")
			}
			if e.Text != "" && e.Mods&nonTextMods == 0 && e.Code != tui.KeyTab {
				return "type"
			}
			return ""
		},
		hyperOrMeta)
}
