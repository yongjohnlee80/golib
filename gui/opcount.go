//go:build gui_opcount

package gui

import "sync/atomic"

// The op counters, compiled in only under the gui_opcount build tag: what the renderer and
// present record, so a test can hold the drawing to its budget (count ops, not milliseconds).
//
// walk is what Gio walks: every op recorded, plus, at each replay of a recorded macro (a row, a
// frame segment, a canvas picture), the ops that macro holds. A macro's own count is the walk
// while it was recorded (walkNow before and after).
type opCounts struct {
	rows   atomic.Int64 // rows recorded (recordRow)
	paints atomic.Int64 // native views painted (View.Paint)

	fills        atomic.Int64 // solid or gradient paints of a clipped area
	glyphPaths   atomic.Int64 // glyph outlines: a row's batch per colour, or one glyph drawn alone
	colourGlyphs atomic.Int64 // colour glyph bitmaps
	images       atomic.Int64 // image and frame paints
	clips        atomic.Int64 // clip areas pushed
	transforms   atomic.Int64 // transforms and opacities pushed

	batches atomic.Int64 // glyph paths that are a row's batch
	cells   atomic.Int64 // glyph paths of a glyph drawn alone (drawCell)

	walk atomic.Int64
}

var ops opCounts

// resetOps zeroes every counter.
func resetOps() { ops = opCounts{} }

func countRow()   { ops.rows.Add(1) }
func countPaint() { ops.paints.Add(1) }
func countBatch() { ops.batches.Add(1) }
func countCell()  { ops.cells.Add(1) }

func countFill()        { ops.fills.Add(1); ops.walk.Add(1) }
func countGlyphPath()   { ops.glyphPaths.Add(1); ops.walk.Add(1) }
func countColourGlyph() { ops.colourGlyphs.Add(1); ops.walk.Add(1) }
func countImage()       { ops.images.Add(1); ops.walk.Add(1) }
func countClip()        { ops.clips.Add(1); ops.walk.Add(1) }
func countTransform()   { ops.transforms.Add(1); ops.walk.Add(1) }

// walkNow is the walk so far.
func walkNow() int { return int(ops.walk.Load()) }

// countReplay counts a replayed macro's ops into the walk.
func countReplay(n int) { ops.walk.Add(int64(n)) }
