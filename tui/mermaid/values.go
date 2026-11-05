package mermaid

import "strconv"

// PercentText is a percentage (parse/mermaid's Percent) as a label shows it: its digits, a whole
// one with no ".0", then %: "42%", "12.5%", "0%". The window's painters call it too, so a share
// reads the same in both.
func PercentText(pct float64) string { return strconv.FormatFloat(pct, 'f', -1, 64) + "%" }
