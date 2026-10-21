package xterm

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/key"
)

// A mouse report's button byte (SGR's b in CSI < b ; x ; y M/m, and the legacy report's b-32):
// the button in its low two bits, plus these.
const (
	MouseShiftBit  = 4  // Shift held
	MouseAltBit    = 8  // Alt (or Meta) held: xterm's "meta"
	MouseCtrlBit   = 16 // Ctrl held
	MouseMotionBit = 32 // motion, the low bits the button held (3: none)
	MouseWheelBit  = 64 // a wheel step, the low bits its direction
	MouseNoButton  = 3  // the low bits of a motion with no button held, and of a legacy release
)

// mouseButtons are the buttons by their number: xterm's low two bits, which are also a browser
// event's button index.
var mouseButtons = []tui.MouseButton{tui.MouseLeft, tui.MouseMiddle, tui.MouseRight}

// MouseButtonCode is the number a button is reported as: 0 left, 1 middle, 2 right.
func MouseButtonCode(b tui.MouseButton) (int, bool) {
	for i, mb := range mouseButtons {
		if mb == b {
			return i, true
		}
	}
	return 0, false
}

// MouseButtonOf is the button a number reports; false for any other number.
func MouseButtonOf(code int) (tui.MouseButton, bool) {
	if code >= 0 && code < len(mouseButtons) {
		return mouseButtons[code], true
	}
	return tui.MouseNone, false
}

// wheelButtons are the wheel directions by the low bits of a wheel report.
var wheelButtons = []tui.MouseButton{tui.WheelUp, tui.WheelDown, tui.WheelLeft, tui.WheelRight}

// WheelCode is a wheel step's button byte: MouseWheelBit plus its direction.
func WheelCode(b tui.MouseButton) (int, bool) {
	for i, w := range wheelButtons {
		if w == b {
			return MouseWheelBit + i, true
		}
	}
	return 0, false
}

// WheelOf is the wheel direction a wheel report's low two bits name.
func WheelOf(low int) tui.MouseButton { return wheelButtons[low&3] }

// MouseModBits are the modifier bits a report carries for mods: Shift, Alt or Meta, and Ctrl.
func MouseModBits(m key.Mods) int {
	b := 0
	if m&key.Shift != 0 {
		b += MouseShiftBit
	}
	if m&(key.Alt|key.Meta) != 0 {
		b += MouseAltBit
	}
	if m&key.Ctrl != 0 {
		b += MouseCtrlBit
	}
	return b
}

// MouseMods is the modifiers a report's button byte carries: Shift, Alt and Ctrl.
func MouseMods(b int) key.Mods {
	var m key.Mods
	if b&MouseShiftBit != 0 {
		m |= key.Shift
	}
	if b&MouseAltBit != 0 {
		m |= key.Alt
	}
	if b&MouseCtrlBit != 0 {
		m |= key.Ctrl
	}
	return m
}
