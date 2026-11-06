package mermaid

import "math"

// VALUES BOTH RENDERERS SHOW, computed here once so the window and the cells agree. Their display
// (a unit, a format) is the renderers': tui/mermaid writes each once for both.

// Percent is share (0..1) as a percentage, rounded once to one decimal: 0.125 is 12.5, 1/3 is 33.3.
// Every renderer shows this number; none rounds a share itself.
func Percent(share float64) float64 { return math.Round(share*1000) / 10 }
