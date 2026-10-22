package gui

import "github.com/yongjohnlee80/gio/op"

// frameCutter cuts the frame being recorded at its stream slots (frameParts): the drawing before
// the first slot, and after each, as a segment of its own with its ops' cost (gui_opcount). The
// tui loop owns it, as it owns the renderer.
type frameCutter struct {
	ops      *op.Ops    // the frame being recorded
	rec      op.MacroOp // the segment being recorded
	cuts     []framePart
	cutting  bool
	segStart int // the walk when the segment being recorded began (gui_opcount)
}

// begin starts recording ops, the frame, as its first segment.
func (c *frameCutter) begin(ops *op.Ops) {
	c.ops, c.rec, c.cuts, c.cutting = ops, op.Record(ops), c.cuts[:0], true
	c.segStart = walkNow()
}

// recording reports whether ops is the frame being cut: a canvas drawing into it may cut it.
func (c *frameCutter) recording(ops *op.Ops) bool { return c.cutting && c.ops == ops }

// cut ends the drawing so far at a stream slot and starts the next segment.
func (c *frameCutter) cut(slot streamSlot) {
	c.cuts = append(c.cuts, framePart{slot: slot, seg: c.rec.Stop(), cost: walkNow() - c.segStart})
	c.segStart = walkNow()
	c.rec = op.Record(c.ops)
}

// end stops recording: the first segment and its cost, and each slot with the segment after it;
// nil parts when the frame had no slot, the first segment being the whole frame.
func (c *frameCutter) end() (first op.CallOp, firstCost int, parts []framePart) {
	c.cutting = false
	last := c.rec.Stop()
	lastCost := walkNow() - c.segStart
	if len(c.cuts) == 0 {
		return last, lastCost, nil
	}
	// cuts[0].seg is the drawing before the first slot; each later seg follows the slot before.
	parts = make([]framePart, len(c.cuts))
	for i := range c.cuts {
		parts[i].slot = c.cuts[i].slot
		if i+1 < len(c.cuts) {
			parts[i].seg, parts[i].cost = c.cuts[i+1].seg, c.cuts[i+1].cost
		} else {
			parts[i].seg, parts[i].cost = last, lastCost
		}
	}
	return c.cuts[0].seg, c.cuts[0].cost, parts
}
