package gui

import (
	"image"
	"testing"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"github.com/yongjohnlee80/golib/tui"
)

// Two frames with the caret at the same place: the input method's stored caret
// geometry must survive the second frame's selection notification — Gio keeps
// whatever the command carries, so a zero caret would move the candidate
// window to the origin until the caret moved again.
func TestAStationaryCaretKeepsItsGeometry(t *testing.T) {
	r := new(input.Router)
	b := &Backend{q: newEventQueue()}
	tag := &b.gio
	m := metrics{}
	b.readInput(r.Source(), tag, m) // registers the filters
	r.Source().Execute(key.FocusCmd{Tag: tag})
	b.readInput(r.Source(), tag, m)

	caret := image.Rect(100, 200, 101, 218)
	frame1 := &frame{caret: caret, base: 14}
	b.placeCaret(r.Source(), tag, frame1)
	state1 := r.EditorState()
	if state1.Selection.Caret.Pos.X != 100 || state1.Selection.Caret.Pos.Y != 214 ||
		state1.Selection.Caret.Ascent != 14 {
		t.Fatalf("frame 1 stored caret %+v, want (100,214) ascent 14", state1.Selection.Caret)
	}
	// the next frame: the caret has not moved; the backend tells the selection again
	b.placeCaret(r.Source(), tag, frame1)
	state2 := r.EditorState()
	if state2.Selection.Caret.Pos.X != 100 || state2.Selection.Caret.Pos.Y != 214 ||
		state2.Selection.Caret.Ascent != 14 {
		t.Fatalf("a stationary frame stored caret %+v, want the geometry kept — a zero caret moves the candidate window to the origin",
			state2.Selection.Caret)
	}
	// and the caret's move is still told when it does move
	frame2 := &frame{caret: image.Rect(50, 60, 51, 78), base: 14}
	b.placeCaret(r.Source(), tag, frame2)
	state3 := r.EditorState()
	if state3.Selection.Caret.Pos.X != 50 || state3.Selection.Caret.Pos.Y != 74 {
		t.Fatalf("a moved caret stored %+v, want (50,74)", state3.Selection.Caret)
	}
	_ = tui.KeyEnter
}
