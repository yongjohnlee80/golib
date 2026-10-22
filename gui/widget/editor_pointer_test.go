package widget

import (
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// A drag held past the text's bottom keeps scrolling, a line per tick, and the selection with it,
// though the pointer does not move, as the terminal Editor's does (tuiwidget.EditorPointer); the
// release stops it.
func TestADragHeldPastTheBottomKeepsScrolling(t *testing.T) {
	h := startEditor(t, 40, 10, WithCore(tuiwidget.CoreInitialText(strings.Repeat("line\n", 100))))
	h.paint()
	ox, oy := h.bodyOrigin()
	var rows int
	h.onLoop(func() { rows = h.e.body.h })
	_ = h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: ox + 1, Y: oy})
	_ = h.tb.Inject(tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: ox + 1, Y: oy + rows, SubY: 0.5})
	h.barrier()
	var line float32
	h.onLoop(func() { line = h.e.layout.lineHeight(h.e.layout.monoFont()) })
	scroll := func() (s float32) { h.onLoop(func() { s = h.e.layout.scroll }); return s }
	// the motion itself scrolls (its step, and the caret's reveal); what follows is the hold's
	held := scroll()
	heldLine, _ := h.line()
	h.until("the held drag to scroll on by four lines, the pointer still", func() bool { return scroll() >= held+4*line })
	if ln, _ := h.line(); ln < heldLine+4 {
		t.Errorf("the selection's end is on line %d, want it carried four lines on from %d", ln, heldLine)
	}
	_ = h.tb.Inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: ox + 1, Y: oy + rows})
	h.barrier()
	stopped := scroll()
	time.Sleep(200 * time.Millisecond) // four of the auto-scroll's ticks
	if got := scroll(); got != stopped {
		t.Errorf("after the release the view scrolled on, %v to %v", stopped, got)
	}
}
