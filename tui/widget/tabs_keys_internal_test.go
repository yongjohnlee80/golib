package widget

import (
	"cmp"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldTabsKey is Tabs.HandleEvent's key decision before commands, verbatim but for its effects, which
// are named: the oracle of the no-map differential.
func oldTabsKey(e tui.KeyEvent, focused bool) string {
	if e.Kind == tui.KeyRelease {
		return ""
	}
	if e.Mods&tui.ModCtrl != 0 {
		switch e.Code {
		case tui.KeyPageUp:
			return "prev"
		case tui.KeyPageDown:
			return "next"
		}
		return ""
	}
	if focused && e.Mods.Chord() == 0 {
		switch e.Code {
		case '[', tui.KeyLeft:
			return "prev"
		case ']', tui.KeyRight:
			return "next"
		}
	}
	return ""
}

var deltaName = map[int]string{-1: "prev", 0: "", 1: "next"}

func TestTabsKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	tb := &Tabs{}
	for _, focused := range []bool{false, true} {
		keyGridDiff(t, map[bool]string{false: "Tabs, content focused", true: "Tabs, bar focused"}[focused],
			func(e tui.KeyEvent) string { return oldTabsKey(e, focused) },
			func(e tui.KeyEvent) string { return deltaName[tb.keyDelta(e, focused)] },
			tabsDeviation)
	}
}

// tabsDeviation names what the key map changes on purpose for Tabs: Hyper or Meta held, and Shift
// spent on the bracket it typed.
func tabsDeviation(e tui.KeyEvent) string {
	return cmp.Or(shiftSpentOnPrintable(e), hyperOrMeta(e))
}

// oldStripKey is tabStrip.key's decision before commands, verbatim but for its effects and for the
// owner's state, which is the caller's: whether the strip has tabs, and which is active.
func oldStripKey(e tui.KeyEvent, baseFocused, hasAdd, menuShown bool) string {
	if e.Kind == tui.KeyRelease {
		return ""
	}
	focused := baseFocused && e.Mods.Chord() == 0
	if focused {
		if e.Text == "+" && hasAdd {
			return "add"
		}
		if e.Code == tui.KeyEnter && menuShown {
			return "menu"
		}
	}
	delta := 0
	if e.Mods.Chord() == tui.ModCtrl {
		if e.Code == tui.KeyPageUp {
			delta = -1
		}
		if e.Code == tui.KeyPageDown {
			delta = 1
		}
	} else if focused {
		if e.Code == tui.KeyLeft || e.Code == '[' {
			delta = -1
		}
		if e.Code == tui.KeyRight || e.Code == ']' {
			delta = 1
		}
	}
	return deltaName[delta]
}

var stripKeyName = map[stripKey]string{stripNone: "", stripAdd: "add", stripMenu: "menu", stripPrev: "prev", stripNext: "next"}

// stripDeviation names what the key map changes on purpose for the strip: Hyper or Meta held; Shift
// spent on the character it typed (a kitty terminal's Shift+= sends "+" with Shift), which the old
// test of the unshifted chord refused; and a + with no text, which the old strip, reading only the
// text, refused, and the key map reads by its code.
func stripDeviation(e tui.KeyEvent) string {
	if e.Code == '+' && e.Text == "" && e.Mods.Chord() == 0 {
		return "a + with no text reads its code"
	}
	return cmp.Or(shiftSpentOnPrintable(e), hyperOrMeta(e))
}

func TestTabStripKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	s := &tabStrip{base: &Base{}}
	for _, focused := range []bool{false, true} {
		for _, hasAdd := range []bool{false, true} {
			for _, menuShown := range []bool{false, true} {
				keyGridDiff(t, "TabStrip",
					func(e tui.KeyEvent) string { return oldStripKey(e, focused, hasAdd, menuShown) },
					func(e tui.KeyEvent) string { return stripKeyName[s.keyOf(e, focused, hasAdd, menuShown)] },
					stripDeviation)
			}
		}
	}
	// a kitty terminal's Shift+= typing +, which the grid's own texts do not model
	kitty := tui.KeyEvent{Kind: tui.KeyPress, Code: '=', Mods: tui.ModShift, Text: "+"}
	if got := s.keyOf(kitty, true, true, false); got != stripAdd {
		t.Errorf("Shift+= typing + on a focused strip with something to add: %v", stripKeyName[got])
	}
}
