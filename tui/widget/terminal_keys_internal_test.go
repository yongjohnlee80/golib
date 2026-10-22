package widget

import (
	"cmp"
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldInputAct is Terminal.inputKey's decision before commands, verbatim but for its effects, named as
// inputAct's, and the Terminal's state, its arguments. HandleEvent passes it no release.
func oldInputAct(k tui.KeyEvent, running, exited, prefix, vimKeys, altScreen bool) termInput {
	if !running {
		if exited && (k.Code == tui.KeyEnter || (k.Text == "i" && k.Mods.Chord() == 0)) {
			return termRestart
		}
		return termNone
	}
	if prefix {
		switch {
		case isCtrl(k, 'n'):
			return termToNormal
		case isCtrl(k, '\\'):
			return termSendPrefix
		}
		return termSendBoth
	}
	if isCtrl(k, '\\') {
		return termPrefix
	}
	if k.Code == tui.KeyEscape && k.Mods.Chord() == 0 && vimKeys && !altScreen {
		return termToNormal
	}
	return termSend
}

// terminalInputDeviation names what the key map changes on purpose in input mode: Hyper or Meta held
// on the prefix or Esc; the old prefix test's case folding, which read Ctrl+< as Ctrl+\ (it mapped
// \ to < as a letter to its capital); and an i with no text, which the old restart refused and the key
// map reads by its code.
func terminalInputDeviation(e tui.KeyEvent) string {
	switch {
	case e.Code == '<' && e.Mods&tui.ModCtrl != 0:
		return "Ctrl+< is not Ctrl+\\"
	case e.Code == 'i' && e.Text == "" && e.Mods.Chord() == 0:
		return "an i with no text reads its code"
	}
	return hyperOrMeta(e)
}

func TestTerminalInputKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	for st := range 32 {
		running, exited, prefix, vimKeys, alt := st&1 != 0, st&2 != 0, st&4 != 0, st&8 != 0, st&16 != 0
		name := fmt.Sprintf("Terminal input (running %v, exited %v, prefix %v, vim %v, alt %v)", running, exited, prefix, vimKeys, alt)
		keyGridDiff(t, name,
			func(e tui.KeyEvent) string {
				if e.Kind == tui.KeyRelease {
					return ""
				}
				return fmt.Sprint(oldInputAct(e, running, exited, prefix, vimKeys, alt))
			},
			func(e tui.KeyEvent) string {
				if e.Kind == tui.KeyRelease {
					return ""
				}
				return fmt.Sprint(inputAct(noKeymap, e, running, exited, prefix, vimKeys, alt))
			},
			terminalInputDeviation)
	}
}

// oldNormalKey is normalState.key's key decision before commands, verbatim but for its effects, named
// by the command each now is, with nothing pending and something to cancel.
func oldNormalKey(k tui.KeyEvent) string {
	mods := k.Mods.Chord()
	if mods&(tui.ModAlt|tui.ModSuper|tui.ModHyper|tui.ModMeta) != 0 {
		return ""
	}
	if mods&tui.ModCtrl != 0 {
		switch {
		case isCtrl(k, 'u'):
			return "halfUp"
		case isCtrl(k, 'd'):
			return "halfDown"
		case isCtrl(k, 'b'):
			return "pageUp"
		case isCtrl(k, 'f'):
			return "pageDown"
		}
		return ""
	}
	if k.Code == tui.KeyEscape {
		return "cancel"
	}
	key := k.Text
	switch k.Code {
	case tui.KeyLeft:
		key = "h"
	case tui.KeyRight:
		key = "l"
	case tui.KeyUp:
		key = "k"
	case tui.KeyDown:
		key = "j"
	case tui.KeyHome:
		key = "0"
	case tui.KeyEnd:
		key = "$"
	case tui.KeyPageUp:
		return "pageUp"
	case tui.KeyPageDown:
		return "pageDown"
	case tui.KeyEnter:
		key = "\r"
	}
	switch key {
	case "i", "a", "\r":
		return "input"
	case "h":
		return "left"
	case "l":
		return "right"
	case "j":
		return "down"
	case "k":
		return "up"
	case "0":
		return "lineStart"
	case "$":
		return "lineEnd"
	case "w":
		return "wordForward"
	case "b":
		return "wordBackward"
	case "e":
		return "wordEnd"
	case "g":
		return "goPrefix"
	case "G":
		return "bottom"
	case "v":
		return "visual"
	case "V":
		return "visualLine"
	case "y":
		return "yank"
	case "/":
		return "search"
	case "?":
		return "searchBackward"
	case "n":
		return "next"
	case "N":
		return "prev"
	}
	return ""
}

// normalDeviation names what the key map changes on purpose in normal mode: Hyper or Meta held; a
// letter with no text, which the old handler, reading only the text, ignored and the key map reads by
// its code; W, B and E, which the old handler did not have; and the old prefix test's case folding (Ctrl and a capital letter is the letter's chord
// both ways, but the old test also took a Ctrl capital that key.Of reads with Shift).
func normalDeviation(e tui.KeyEvent) string {
	if e.Text == "" && e.Code < 128 && e.Code > ' ' && e.Mods.Chord()&^tui.ModShift == 0 {
		return "a character with no text reads its code"
	}
	if isKey(e, TermWORD.Keys...) || isKey(e, TermWORDBack.Keys...) || isKey(e, TermWORDEnd.Keys...) {
		return "W, B and E are new: the WORD motions, the editor's"
	}
	return cmp.Or(hyperOrMeta(e), ctrlShiftLetter(e))
}

func TestTerminalNormalKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	keyGridDiff(t, "Terminal normal", presses(oldNormalKey),
		presses(func(e tui.KeyEvent) string {
			cmd, ok := (*tui.Context)(nil).KeyCommand(e, terminalNormalCommands...)
			if !ok {
				return ""
			}
			return strings.TrimPrefix(cmd.ID, "tui.terminal.normal.")
		}),
		normalDeviation)
}
