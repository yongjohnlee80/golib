package term

import (
	"bytes"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

func TestEmitterFramesWithoutABackend(t *testing.T) {
	// The emitter is bytes in, bytes out: a frame hides the cursor while it paints, writes the
	// cell, and gives the latched cursor back; the same state again is zero bytes.
	e := newEmitter(false)
	diff := []tui.CellUpdate{{X: 2, Y: 1, Cell: tui.Cell{Content: "a", Width: 1}}}
	want := tui.CursorState{X: 3, Y: 1, PosSet: true, Visible: true}
	got := string(e.frame(tui.Capabilities{}, diff, want, true, nil))
	if exp := "\x1b[?25l\x1b[2;3H\x1b[0ma\x1b[2;4H\x1b[?25h"; got != exp {
		t.Fatalf("frame = %q, want %q", got, exp)
	}
	if again := e.frame(tui.Capabilities{}, nil, want, false, nil); again != nil {
		t.Fatalf("a frame with nothing to say = %q, want nil", again)
	}
	if got := string(e.frame(tui.Capabilities{SyncOutput: true}, nil, want, true, nil)); got != "\x1b[?2026h\x1b[2;4H\x1b[?2026l" {
		t.Fatalf("a cursor-only frame under mode 2026 = %q", got)
	}
}

func TestEmitterRestoresWhatItGave(t *testing.T) {
	// restore gives back the cursor colour the emitter set and deletes the images it placed, and
	// only those; a second restore has nothing left to delete.
	e := newEmitter(false)
	red := tui.CellColor{Kind: tui.CellColorRGB, R: 255}
	place := &tui.ImagePlacement{Image: tui.Image{ID: 7, Version: 1, PNG: []byte{1}}, Cols: 1, Rows: 1}
	e.frame(tui.Capabilities{}, nil, tui.CursorState{Visible: true, Color: red, Colored: true}, true, []tui.ImageOp{{Place: place}})

	var buf bytes.Buffer
	e.restore(&buf)
	if exp := "\x1b]112\x07\x1b_Ga=d,d=I,q=2,i=7\x1b\\"; buf.String() != exp {
		t.Fatalf("restore = %q, want %q", buf.String(), exp)
	}
	buf.Reset()
	e.restore(&buf)
	if buf.Len() != 0 {
		t.Fatalf("a second restore = %q, want nothing", buf.String())
	}
}
