package widget

import (
	"context"
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
)

// THE EDITOR'S PICTURES — a Rendered block's Mermaid diagram or Vega-Lite chart, laid off the loop.
// A renderer that draws pictures asks the Editor for one (CellPictureHost); the Editor lays it as a
// task (LayCellPicture), one slot per block and width (the view is laid at two, its scroll
// indicator's column in or out), and lays its blocks again when it arrives. Until then, and when the
// picture can't be laid, the renderer lays the fence as source. A block whose fence changed keeps
// showing its last picture while the new one lays; a result for a request since replaced is
// dropped. The slots are the shown buffer's: another buffer, a view switch or a renderer drops every
// slot and stops its request (Context.GoStoppable's stop), so no result of theirs is taken. A picture's arrival
// settles the layout before the next input, keeping the view's top line, and revealing the cursor
// if it was on the view.

// CellPictureHost lays a Rendered block's picture for a renderer that draws pictures: the Editor
// is one, handed to a renderer with a SetPictureHost method when it is set (SetRenderer).
type CellPictureHost interface {
	// Picture is block b's picture of lang ("mermaid", "vega-lite") from src, laid within cols. ok is
	// false while none is laid yet, or when none can be: the renderer then lays the source.
	Picture(b Block, lang, src string, cols int) (p CellPicture, ok bool)
}

// pictureKey is what a block's picture is laid from.
type pictureKey struct {
	lang, src string
	cols      int
	syntax    SyntaxStyles
}

// pictureAt is a picture slot's place: its block's first line, and the columns it is laid within.
type pictureAt struct{ from, cols int }

// pictureSlot is a block's picture: the request in flight, and the last picture laid.
type pictureSlot struct {
	key     pictureKey
	task    tui.TaskID
	pending bool
	stop    context.CancelFunc
	to      int         // past the block's last line when it was asked for: an edit before it touches it
	pic     CellPicture // the last laid: shown while the next lays
	failed  bool        // the key's lay failed: no picture
}

var _ CellPictureHost = (*Editor)(nil)

// Picture implements CellPictureHost.
func (e *Editor) Picture(b Block, lang, src string, cols int) (CellPicture, bool) {
	ctx := e.Context()
	if ctx == nil {
		return nil, false
	}
	e.picturesOfShownBuffer()
	k := pictureKey{lang: lang, src: src, cols: cols, syntax: e.cellTheme().Syntax}
	at := pictureAt{from: b.From, cols: cols}
	s := e.pics[at]
	if s == nil {
		if e.pics == nil {
			e.pics = map[pictureAt]*pictureSlot{}
		}
		s = &pictureSlot{}
		e.pics[at] = s
	}
	if s.key != k {
		if s.stop != nil {
			s.stop()
		}
		s.key, s.failed, s.pending, s.to = k, false, true, b.To
		policy := ctx.WidthPolicy() // measured off the loop as the loop measures
		measure := func(t string) int { return tui.StringWidthPolicy(t, policy) }
		lay := e.layPicture
		if lay == nil {
			lay = LayCellPicture
		}
		// one request per block and width: a newer one cancels the one it replaces, and so does stop
		s.task, s.stop = ctx.GoStoppable(func(c context.Context) (any, error) {
			return lay(c, k.lang, k.src, measure, k.cols, k.syntax)
		}, tui.Exclusive(fmt.Sprintf("cell picture %d %d", at.from, at.cols)))
	}
	if s.failed || s.pic == nil {
		return nil, false
	}
	return s.pic, true
}

// pictureDone takes a picture's lay, reporting whether it was the current request of a block's:
// the picture is kept (or the block marked as having none), and the layout laid again.
func (e *Editor) pictureDone(r tui.TaskResult) bool {
	e.picturesOfShownBuffer() // another buffer's result is no request of the shown one's
	for _, s := range e.pics {
		if !s.pending || s.task != r.ID {
			continue
		}
		e.arrived()
		s.pending, s.stop = false, nil
		if p, ok := r.Value.(CellPicture); ok && r.Err == nil {
			s.pic = p
		} else {
			s.failed = true
		}
		return true
	}
	return false
}

// arrived marks a picture or an image arriving: the first since the view was laid takes whether the
// cursor is on the view, before the rows change; picGen, a key of the layout, lays the blocks again.
func (e *Editor) arrived() {
	if !e.picArrived { // the first picture since the view was laid: is the cursor on it, before the rows change?
		_, _, e.picReveal = e.Cursor()
		e.picArrived = true
	}
	e.picGen++
}

// dropPictures forgets every block's picture and stops its request: another view, renderer or
// buffer lays its own.
func (e *Editor) dropPictures() {
	for _, s := range e.pics {
		if s.stop != nil {
			s.stop()
		}
	}
	e.pics = nil
	e.dropImages()
	e.picGen++
	e.picArrived, e.picReveal = false, false // an arrival's snapshot is its slots' owner's: the next owner takes its own
}

// picturesTouched stops the requests an edit from line from touches, those of a block ending after
// it (its source changed, or its place): their results are not taken, and the block asks again when
// it is laid rendered.
func (e *Editor) picturesTouched(from int) {
	for _, s := range e.pics {
		if s.pending && s.to > from {
			if s.stop != nil {
				s.stop()
			}
			s.pending, s.stop, s.key = false, nil, pictureKey{}
		}
	}
}

// picturesOfShownBuffer drops the slots when another buffer is shown: a picture, and a request in
// flight, are their buffer's.
func (e *Editor) picturesOfShownBuffer() {
	if b := e.core.Buffer(); e.picBuf != b {
		e.dropPictures()
		e.picBuf = b
	}
}
