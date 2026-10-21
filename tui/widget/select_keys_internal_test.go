package widget

import (
	"cmp"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldSelectOpens is the closed Select's key decision before commands, verbatim but for its effect.
func oldSelectOpens(e tui.KeyEvent) string {
	if e.Kind == tui.KeyRelease || e.Mods&nonTextMods != 0 {
		return ""
	}
	switch e.Code {
	case tui.KeyEnter, ' ', tui.KeyDown:
		return "open"
	}
	return ""
}

func TestSelectKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	s := &Select[string]{}
	keyGridDiff(t, "Select",
		oldSelectOpens,
		func(e tui.KeyEvent) string { return map[bool]string{true: "open"}[s.opens(e)] },
		func(e tui.KeyEvent) string { return cmp.Or(hyperOrMeta(e), shiftSpentOnPrintable(e)) })
}

// oldPopupKey is the open options' key decision before commands, verbatim but for its effects, which
// are named, and for the select's filtering, its argument.
func oldPopupKey(e tui.KeyEvent, filterOn bool) string {
	if e.Kind == tui.KeyRelease {
		return ""
	}
	switch e.Code {
	case tui.KeyEscape:
		return "close"
	case tui.KeyEnter:
		return "commit"
	case tui.KeyUp:
		return "up"
	case tui.KeyDown:
		return "down"
	case 'k':
		if filterOn {
			break
		}
		return "up"
	case 'j':
		if filterOn {
			break
		}
		return "down"
	case tui.KeyPageUp:
		return "pageUp"
	case tui.KeyPageDown:
		return "pageDown"
	case tui.KeyHome:
		return "first"
	case tui.KeyEnd:
		return "last"
	case tui.KeyBackspace:
		return "backspace"
	}
	if filterOn && e.Text != "" && e.Mods&nonTextMods == 0 && e.Code != tui.KeyTab {
		return "type"
	}
	return ""
}

var popupKeyName = map[popupKey]string{popupNone: "", popupClose: "close", popupCommit: "commit",
	popupUp: "up", popupDown: "down", popupPageUp: "pageUp", popupPageDown: "pageDown",
	popupFirst: "first", popupLast: "last", popupBackspace: "backspace", popupType: "type"}

func TestSelectPopupKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	p := &selectPanel[string]{}
	for _, filterOn := range []bool{false, true} {
		keyGridDiff(t, map[bool]string{false: "Select options", true: "Select options, filtering"}[filterOn],
			func(e tui.KeyEvent) string { return oldPopupKey(e, filterOn) },
			func(e tui.KeyEvent) string { return popupKeyName[p.keyOf(e, filterOn)] },
			popupDeviation)
	}
}

// popupDeviation names what the key map changes on purpose for the options: Hyper or Meta held; a
// capital J or K with Ctrl, Alt or Super and no text, which key.Of reads as the letter's chord (Ctrl+K)
// where the old switch compared the code to 'k'; and a J or K typed with Shift, the capital, which the
// old switch took for j or k because it read the code and ignored the Shift that typed it.
func popupDeviation(e tui.KeyEvent) string {
	switch {
	case (e.Code == 'J' || e.Code == 'K') && e.Text == "" && e.Mods&(tui.ModCtrl|tui.ModAlt|tui.ModSuper) != 0:
		return "a capital with a modifier and no text is its letter's chord"
	case (e.Code == 'j' || e.Code == 'k') && (e.Text == "J" || e.Text == "K"):
		return "a typed capital J or K is not j or k"
	}
	return hyperOrMeta(e)
}
