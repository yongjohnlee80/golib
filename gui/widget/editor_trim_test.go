package widget

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// TestBlocksFarFromTheViewLetGoOfTheirLines: a long note painted at its top and then at its end
// lets the top blocks' lines go, keeping their heights; painted at the top again, they are laid out
// again.
func TestBlocksFarFromTheViewLetGoOfTheirLines(t *testing.T) {
	e := NewEditor()
	var sb strings.Builder
	for i := range 600 {
		fmt.Fprintf(&sb, "line %d, with a few words on it\n", i)
	}
	e.core.SetValue(sb.String())
	v := bodyView{b: e.body}
	paint := func() { v.Paint(gui.NewRecordingCanvas(gui.Size{W: 800, H: 400}, gui.Size{W: 8, H: 16})) }
	l := e.layout

	paint()
	first := l.laid[0]
	if first == nil || first.trimmed || len(first.bl.Lines) == 0 {
		t.Fatal("the first block, in view, was not laid out")
	}
	h0 := first.bl.Height

	l.scroll = l.total() - 400 // the note's end, far below
	paint()
	if !first.trimmed || first.bl.Lines != nil {
		t.Fatal("a block far above the view kept its lines")
	}
	if got := l.heightOf(0); got != h0 {
		t.Fatalf("a trimmed block's height is %v, it was laid out %v tall: the blocks below it would move", got, h0)
	}
	// the blocks this paint drew (their tops in the view) keep their lines; the scroll was set from
	// estimated heights, so which blocks those are is read back, not assumed
	inView := 0
	for i := range l.laid {
		if top := l.topOf(i); top >= l.scroll && top < l.scroll+400 {
			inView++
			if lb := l.laid[i]; lb == nil || lb.trimmed || len(lb.bl.Lines) == 0 {
				t.Fatalf("block %d, in view, has no lines", i)
			}
		}
	}
	if inView == 0 {
		t.Fatal("no block was in view at the note's end")
	}

	l.scroll = 0
	paint()
	if lb := l.laid[0]; lb.trimmed || len(lb.bl.Lines) == 0 {
		t.Fatal("a trimmed block drawn again was not laid out again")
	}
}
