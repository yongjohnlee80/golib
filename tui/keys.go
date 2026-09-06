package tui

// Functional key codes for KeyEvent.Code. The terminal decoder in tui/term
// maps escape sequences onto exactly these constants.
// REFERENCE: tui/term
//
// Printable keys carry their Unicode code point in Code. Functional keys
// use the kitty keyboard protocol's Unicode Private Use Area assignments
// (https://sw.kovidgoyal.net/kitty/keyboard-protocol/#functional-key-definitions)
// so a kitty-mode decode is identity and the legacy CSI/SS3 decoder maps
// onto the same constants. The C0-derived keys keep their traditional
// code points, exactly as kitty specifies them.
const (
	// C0-derived (legacy byte values, kitty-compatible).
	KeyEnter     rune = 13  // CR
	KeyTab       rune = 9   // HT
	KeyBackspace rune = 127 // DEL
	KeyEscape    rune = 27  // ESC

	// Functional keys (kitty PUA assignments).
	KeyUp       rune = 57352
	KeyDown     rune = 57353
	KeyLeft     rune = 57350
	KeyRight    rune = 57351
	KeyHome     rune = 57356
	KeyEnd      rune = 57357
	KeyPageUp   rune = 57354
	KeyPageDown rune = 57355
	KeyInsert   rune = 57348
	KeyDelete   rune = 57349

	KeyF1  rune = 57364
	KeyF2  rune = 57365
	KeyF3  rune = 57366
	KeyF4  rune = 57367
	KeyF5  rune = 57368
	KeyF6  rune = 57369
	KeyF7  rune = 57370
	KeyF8  rune = 57371
	KeyF9  rune = 57372
	KeyF10 rune = 57373
	KeyF11 rune = 57374
	KeyF12 rune = 57375
)
