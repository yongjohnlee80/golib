package widget

import (
	"cmp"
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldSplitKey is Split.resolveKey before commands, verbatim but for the split's axis and step unit,
// which are its arguments: the oracle of the no-map differential.
func oldSplitKey(e tui.KeyEvent, horiz bool, unit StepUnit) (tui.Action, bool) {
	if e.Kind == tui.KeyRelease {
		return nil, false
	}
	if e.Code == tui.KeyEscape && e.Mods.Chord() == 0 {
		return ResizeCancelAction{}, true
	}
	if e.Mods&tui.ModAlt == 0 || e.Mods&^tui.ModAlt != 0 {
		return nil, false
	}
	step := func(d int) (tui.Action, bool) {
		if horiz {
			return ResizeStepAction{DX: d, Unit: unit}, true
		}
		return ResizeStepAction{DY: d, Unit: unit}, true
	}
	switch e.Code {
	case tui.KeyLeft:
		if horiz {
			return step(-1)
		}
	case tui.KeyRight:
		if horiz {
			return step(1)
		}
	case tui.KeyUp:
		if !horiz {
			return step(-1)
		}
	case tui.KeyDown:
		if !horiz {
			return step(1)
		}
	}
	return nil, false
}

// oldResizableKey is Resizable.resizeKeys before commands, verbatim but for its step unit.
func oldResizableKey(k tui.KeyEvent, unit StepUnit) (tui.Action, bool) {
	if k.Kind == tui.KeyRelease {
		return nil, false
	}
	if k.Code == tui.KeyEscape && k.Mods.Chord() == 0 {
		return ResizeCancelAction{}, true
	}
	if k.Mods != tui.ModShift {
		return nil, false
	}
	switch k.Code {
	case tui.KeyRight:
		return ResizeStepAction{DX: 1, Unit: unit}, true
	case tui.KeyLeft:
		return ResizeStepAction{DX: -1, Unit: unit}, true
	case tui.KeyDown:
		return ResizeStepAction{DY: 1, Unit: unit}, true
	case tui.KeyUp:
		return ResizeStepAction{DY: -1, Unit: unit}, true
	}
	return nil, false
}

func actionName(a tui.Action, ok bool) string {
	if !ok {
		return ""
	}
	return fmt.Sprintf("%#v", a)
}

// lockedArrow is the deviation the key map makes on purpose for the resize keys: the old handlers
// compared raw Mods, so a lock bit a kitty terminal reports (Num Lock, Caps Lock) stopped Alt or
// Shift and an arrow stepping. Every binding now drops lock bits.
func lockedArrow(e tui.KeyEvent) string {
	if e.Mods&tui.LockMods != 0 {
		switch e.Code {
		case tui.KeyLeft, tui.KeyRight, tui.KeyUp, tui.KeyDown:
			return "a lock bit no longer stops a resize step"
		}
	}
	return ""
}

func TestSplitKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	for _, o := range []Orientation{Horizontal, Vertical} {
		s := &Split{o: o, stepUnit: StepPercent}
		keyGridDiff(t, map[Orientation]string{Horizontal: "Split horizontal", Vertical: "Split vertical"}[o],
			func(e tui.KeyEvent) string { return actionName(oldSplitKey(e, o == Horizontal, s.stepUnit)) },
			func(e tui.KeyEvent) string { return actionName(s.resolveKey(e)) },
			func(e tui.KeyEvent) string { return cmp.Or(lockedArrow(e), hyperOrMeta(e)) })
	}
}

func TestResizableKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	r := &Resizable{stepUnit: StepPercent}
	keyGridDiff(t, "Resizable",
		func(e tui.KeyEvent) string { return actionName(oldResizableKey(e, r.stepUnit)) },
		func(e tui.KeyEvent) string { return actionName(r.resizeKeys(e)) },
		func(e tui.KeyEvent) string { return cmp.Or(lockedArrow(e), hyperOrMeta(e)) })
}
