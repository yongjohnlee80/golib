//go:build gui_opcount

package gui

// The op counters, compiled in only under the gui_opcount build tag: what the renderer records,
// so a test can hold the drawing to its budget (count ops, not milliseconds).
type opCounts struct {
	rows    int // rows recorded (recordRow)
	paints  int // native views painted (View.Paint)
	fills   int // filled rectangles (fillRect)
	batches int // glyph batches: one outline per colour per row segment
	cells   int // glyphs drawn alone (drawCell)
}

var ops opCounts

func countRow()   { ops.rows++ }
func countPaint() { ops.paints++ }
func countFill()  { ops.fills++ }
func countBatch() { ops.batches++ }
func countCell()  { ops.cells++ }
