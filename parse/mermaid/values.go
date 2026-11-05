package mermaid

import (
	"strconv"
	"strings"
)

// VALUES BOTH RENDERERS SHOW, computed here once so the window and the cells agree.

// Percent is share (0..1) as a percentage, one decimal with a whole ".0" trimmed: "42%", "12.5%",
// "0%". Every renderer calls it; none rounds a share itself.
func Percent(share float64) string {
	s := strconv.FormatFloat(100*share, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0") + "%"
}
