package style

// XtermRGB is the RGB xterm's default palette gives 256-colour index i: the sixteen ANSI colours
// (0–15), the 6×6×6 cube on levels 0, 95, 135, 175, 215, 255 (16–231), and the 24-step grey ramp
// from 8 by 10 (232–255). It is the one statement of that palette: the terminal downsampler, the
// contrast check (Legible) and a renderer drawing indexed colours all read it, so they agree about
// every index. A theme that overrides the sixteen ANSI colours does so on top of it.
func XtermRGB(i uint8) [3]uint8 {
	switch {
	case i < 16:
		return xterm16[i]
	case i < 232:
		c := int(i) - 16
		return [3]uint8{xtermCube[c/36], xtermCube[c/6%6], xtermCube[c%6]}
	}
	v := uint8(8 + 10*(int(i)-232))
	return [3]uint8{v, v, v}
}

// xterm16 is xterm's sixteen ANSI colours.
var xterm16 = [16][3]uint8{
	{0x00, 0x00, 0x00}, {0xcd, 0x00, 0x00}, {0x00, 0xcd, 0x00}, {0xcd, 0xcd, 0x00},
	{0x00, 0x00, 0xee}, {0xcd, 0x00, 0xcd}, {0x00, 0xcd, 0xcd}, {0xe5, 0xe5, 0xe5},
	{0x7f, 0x7f, 0x7f}, {0xff, 0x00, 0x00}, {0x00, 0xff, 0x00}, {0xff, 0xff, 0x00},
	{0x5c, 0x5c, 0xff}, {0xff, 0x00, 0xff}, {0x00, 0xff, 0xff}, {0xff, 0xff, 0xff},
}

// xtermCube are the 6×6×6 cube's component levels.
var xtermCube = [6]uint8{0, 95, 135, 175, 215, 255}
