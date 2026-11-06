package vegalite

// The Braille tier registers here, under Lay's registry (lay.go), as the bars tier does in its own
// file: lines, points, areas and rules plotted in Braille dots, after the bars and before the
// table.
func init() { registerTier(2, "braille", layBraille) }
