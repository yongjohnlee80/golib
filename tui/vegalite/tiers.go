package vegalite

// The bars and Braille tiers register here, under Lay's registry (lay.go): the bars first (a lone
// bar layer over discrete categories), then Braille plots (lines, points, areas, rules), then the
// table (table.go).
func init() {
	registerTier(1, "bars", layBars)
	registerTier(2, "braille", layBraille)
}
