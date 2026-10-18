//go:build !gui_opcount

package gui

// Without the gui_opcount tag the counters compile to nothing (see opcount.go).
func countRow()   {}
func countPaint() {}
func countFill()  {}
func countBatch() {}
func countCell()  {}
