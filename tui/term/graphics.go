package term

import (
	"bytes"
	"encoding/base64"
	"strconv"

	"github.com/yongjohnlee80/golib/tui"
)

// Images by kitty's graphics protocol (https://sw.kovidgoyal.net/kitty/graphics-protocol/): a PNG
// is transmitted and placed in one command (a=T, f=100), over the cells it is given (c, r), the
// part of it shown its clip (x, y, w, h, in its pixels), the cursor left where it was (C=1),
// replies suppressed (q=2), in base64 chunks of at most 4096 bytes (m=1 on every chunk but the
// last). The same version placed again — a new clip, new cells — deletes only the old placement
// (a=d, d=i) and places the bytes the terminal holds (a=p): a scroll sends no image. A new version,
// and a delete, free the image and its data (a=d, d=I). Inside tmux each command is passed through
// to the terminal outside (tmux's allow-passthrough option must be on).

const graphicsChunk = 4096

// PlaceImage implements tui.GraphicsBackend: latches a placement for the next Flush.
func (b *Backend) PlaceImage(p tui.ImagePlacement) {
	b.latch(func() { b.images.Place(p) })
}

// DeleteImage implements tui.GraphicsBackend: latches a delete for the next Flush.
func (b *Backend) DeleteImage(id uint32) {
	b.latch(func() { b.images.Delete(id) })
}

// writeImages emits the latched image commands into the frame's buffer.
func (e *emitter) writeImages(ops []tui.ImageOp) {
	if len(ops) == 0 {
		return
	}
	if e.placed == nil {
		e.placed, e.sent = map[uint32]bool{}, map[uint32]uint64{}
	}
	for _, op := range ops {
		if op.Place == nil {
			e.writeImageDelete(&e.buf, op.Delete)
			delete(e.placed, op.Delete)
			delete(e.sent, op.Delete)
			continue
		}
		p := op.Place
		held, ok := e.sent[p.ID]
		if ok && held == p.Version {
			e.writePlacementDelete(&e.buf, p.ID) // the old placement goes; the bytes stay
			e.writeCUP(p.X, p.Y)
			e.writeAPC(&e.buf, "a=p,C=1,q=2,i="+strconv.FormatUint(uint64(p.ID), 10)+placementKeys(p), "")
		} else {
			e.writeImageDelete(&e.buf, p.ID) // the old image goes, wherever it was
			e.writeCUP(p.X, p.Y)
			e.writeImagePNG(&e.buf, p)
			e.sent[p.ID] = p.Version
		}
		e.penKnown = false // the next cell anchors absolutely
		e.placed[p.ID] = true
	}
}

// placementKeys are a placement's cells, and its clip when it has one.
func placementKeys(p *tui.ImagePlacement) string {
	keys := ",c=" + strconv.Itoa(p.Cols) + ",r=" + strconv.Itoa(p.Rows)
	if c := p.Clip; !c.Empty() {
		keys += ",x=" + strconv.Itoa(c.X) + ",y=" + strconv.Itoa(c.Y) + ",w=" + strconv.Itoa(c.W) + ",h=" + strconv.Itoa(c.H)
	}
	return keys
}

func (e *emitter) writeImageDelete(buf *bytes.Buffer, id uint32) {
	e.writeAPC(buf, "a=d,d=I,q=2,i="+strconv.FormatUint(uint64(id), 10), "")
}

// writePlacementDelete deletes an image's placements and keeps its data.
func (e *emitter) writePlacementDelete(buf *bytes.Buffer, id uint32) {
	e.writeAPC(buf, "a=d,d=i,q=2,i="+strconv.FormatUint(uint64(id), 10), "")
}

func (e *emitter) writeImagePNG(buf *bytes.Buffer, p *tui.ImagePlacement) {
	data := base64.StdEncoding.EncodeToString(p.PNG)
	first := true
	for len(data) > 0 || first {
		n := min(len(data), graphicsChunk)
		chunk := data[:n]
		data = data[n:]
		more := "0"
		if len(data) > 0 {
			more = "1"
		}
		ctrl := "m=" + more
		if first {
			ctrl = "a=T,f=100,t=d,C=1,q=2,i=" + strconv.FormatUint(uint64(p.ID), 10) + placementKeys(p) + "," + ctrl
			first = false
		}
		e.writeAPC(buf, ctrl, chunk)
	}
}

// writeAPC writes one graphics command, passed through tmux when inside it.
func (e *emitter) writeAPC(buf *bytes.Buffer, ctrl, payload string) {
	seq := "\x1b_G" + ctrl
	if payload != "" {
		seq += ";" + payload
	}
	seq += "\x1b\\"
	buf.WriteString(passthrough(seq, e.tmux))
}
