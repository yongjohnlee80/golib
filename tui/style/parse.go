package style

import (
	"fmt"
	"strconv"
	"strings"
)

// colourNames are the eight ANSI colours, by slot. They name SLOTS, not RGB:
// "blue" is whatever the user's terminal palette says blue is, which is what a
// 1990s text-mode look is made of. "bright" before a name is the slot eight
// above it.
var colourNames = map[string]int{
	"black": 0, "red": 1, "green": 2, "yellow": 3,
	"blue": 4, "magenta": 5, "cyan": 6, "white": 7,
}

// ParseColor reads a colour as a theme writes it:
//
//	"blue"          an ANSI slot           "brightwhite"  the bright slot
//	"gray"/"grey"   bright black           "#1e90ff"      truecolor
//	"default"       the terminal's own foreground or background
//
// Case-insensitive, as Qt's colour names are, and surrounding space is
// ignored. It is the one reader of that vocabulary: a QML palette role, a
// theme's value and a program's own colour strings (a plugin's frame, say)
// all go through it, so they cannot disagree.
func ParseColor(s string) (Color, error) {
	name := strings.ToLower(strings.TrimSpace(s))
	switch name {
	case "default":
		return Default(), nil
	case "gray", "grey":
		return ANSI(8), nil
	}
	if strings.HasPrefix(name, "#") {
		n, err := strconv.ParseUint(strings.TrimPrefix(name, "#"), 16, 32)
		if len(name) != 7 || err != nil {
			return Color{}, fmt.Errorf("%q is not a #rrggbb colour", s)
		}
		return RGB(uint8(n>>16), uint8(n>>8), uint8(n)), nil
	}
	bright := strings.HasPrefix(name, "bright")
	if n, ok := colourNames[strings.TrimPrefix(name, "bright")]; ok {
		if bright {
			n += 8
		}
		return ANSI(n), nil
	}
	return Color{}, fmt.Errorf("%q is not a colour: want one of %s, bright<name>, gray, "+
		"#rrggbb or default", s, colourList())
}

func colourList() string {
	names := make([]string, 8)
	for n, i := range colourNames {
		names[i] = n
	}
	return strings.Join(names, ", ")
}
