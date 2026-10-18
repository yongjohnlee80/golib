//go:build !gui_opcount

package gui

// Without the gui_opcount tag the counters compile to nothing (see opcount.go), and every macro's
// count is zero.
func countRow()         {}
func countPaint()       {}
func countBatch()       {}
func countCell()        {}
func countFill()        {}
func countGlyphPath()   {}
func countColourGlyph() {}
func countImage()       {}
func countClip()        {}
func countTransform()   {}
func walkNow() int      { return 0 }
func countReplay(int)   {}
