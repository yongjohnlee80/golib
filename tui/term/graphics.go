package term

import (
	"bytes"
	"encoding/base64"
	"strconv"

	"github.com/yongjohnlee80/golib/tui"
)

// Images by kitty's graphics protocol (https://sw.kovidgoyal.net/kitty/graphics-protocol/): a PNG
// is transmitted and placed in one command (a=T, f=100), over the cells it is given (c, r), the
// cursor left where it was (C=1), replies suppressed (q=2), in base64 chunks of at most 4096 bytes
// (m=1 on every chunk but the last). Placing an id again replaces the image; a delete (a=d, d=I)
// frees the image and its data. Inside tmux each command is passed through to the terminal
// outside (tmux's allow-passthrough option must be on).

const graphicsChunk = 4096

type imageOp struct {
	place  *tui.ImagePlacement
	delete uint32
}

// PlaceImage implements tui.GraphicsBackend: latches a placement for the next Flush.
func (b *Backend) PlaceImage(p tui.ImagePlacement) {
	b.cmu.Lock()
	b.imageOps = append(b.imageOps, imageOp{place: &p})
	b.cmu.Unlock()
}

// DeleteImage implements tui.GraphicsBackend: latches a delete for the next Flush.
func (b *Backend) DeleteImage(id uint32) {
	b.cmu.Lock()
	b.imageOps = append(b.imageOps, imageOp{delete: id})
	b.cmu.Unlock()
}

// writeImages emits the latched image commands into the frame's buffer, under wmu.
func (b *Backend) writeImages(ops []imageOp) {
	if len(ops) == 0 {
		return
	}
	if b.placed == nil {
		b.placed = map[uint32]bool{}
	}
	for _, op := range ops {
		if op.place == nil {
			b.writeImageDelete(&b.buf, op.delete)
			delete(b.placed, op.delete)
			continue
		}
		p := op.place
		b.writeImageDelete(&b.buf, p.ID) // the old placement goes, wherever it was
		b.writeCUP(p.X, p.Y)
		b.writeImagePNG(&b.buf, p)
		b.penKnown = false // the next cell anchors absolutely
		b.placed[p.ID] = true
	}
}

func (b *Backend) writeImageDelete(buf *bytes.Buffer, id uint32) {
	b.writeAPC(buf, "a=d,d=I,q=2,i="+strconv.FormatUint(uint64(id), 10), "")
}

func (b *Backend) writeImagePNG(buf *bytes.Buffer, p *tui.ImagePlacement) {
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
			ctrl = "a=T,f=100,t=d,C=1,q=2,i=" + strconv.FormatUint(uint64(p.ID), 10) +
				",c=" + strconv.Itoa(p.Cols) + ",r=" + strconv.Itoa(p.Rows) + "," + ctrl
			first = false
		}
		b.writeAPC(buf, ctrl, chunk)
	}
}

// writeAPC writes one graphics command, passed through tmux when inside it.
func (b *Backend) writeAPC(buf *bytes.Buffer, ctrl, payload string) {
	seq := "\x1b_G" + ctrl
	if payload != "" {
		seq += ";" + payload
	}
	seq += "\x1b\\"
	buf.WriteString(passthrough(seq, inTmux(b.cfg.env)))
}
