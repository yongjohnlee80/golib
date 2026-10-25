package gui_test

import (
	"testing"

	"github.com/yongjohnlee80/gio/io/pointer"
	guiwidget "github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// A card over the native editor, as a FloatWindow's AddAligned puts one (the editor filled, the card
// at the top-right), takes the I-beam from the cells it covers, through the App and the Backend as a
// window runs them: on the card's rows the editor's I-beam stops where the card begins, and below it
// runs the editor's width.
func TestACardOverTheNativeEditorTakesItsCellsIBeam(t *testing.T) {
	e := guiwidget.NewEditor(guiwidget.WithCore(tuiwidget.CoreInitialText("one two three four five six seven eight nine ten")))
	card := tuiwidget.NewBox(tuiwidget.NewText("3 of 9 [X]"), tuiwidget.WithTitle("CARD")) // 12 columns, 3 rows
	st := tui.NewStack()
	st.AddFilled(e)
	st.AddAligned(card, tui.AlignTopRight)
	p := startPipe(t, st, 60, 8)
	f := p.frame(t, 0)
	for i := 1; from(f, pointer.CursorText) < 0 && i < 50; i++ {
		f = p.frame(t, i)
	}
	cardLeft := 60 - 12
	under, below := false, false
	for _, a := range f {
		if a.Cursor != pointer.CursorText {
			continue
		}
		if a.Y < 3 && a.X+a.W > cardLeft {
			t.Fatalf("an I-beam area %+v reaches under the card (columns %d to 59, rows 0 to 2): %+v", a, cardLeft, f)
		}
		under = under || (a.Y >= 1 && a.Y < 3 && a.X+a.W == cardLeft)
		below = below || (a.Y >= 3 && a.X+a.W == 60)
	}
	if !under || !below {
		t.Fatalf("the I-beam areas %+v: want them to stop at the card's left (%d) on its rows, and run the full width below it", f, cardLeft)
	}
}
