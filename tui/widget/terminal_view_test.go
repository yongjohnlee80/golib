package widget_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/pty"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// doubleView is a TerminalView at half the window's cell: twice the columns and rows, the pointer
// doubled. It records its owner, its frames and whether it was detached.
type doubleView struct {
	owner *widget.Terminal

	mu       sync.Mutex
	frames   int
	text     string
	cursor   tui.Point
	show     bool
	detached bool
}

func (v *doubleView) Cells(w, h int) (int, int) { return 2 * w, 2 * h }
func (v *doubleView) Pointer(e tui.MouseEvent) tui.MouseEvent {
	e.X, e.Y = 2*e.X, 2*e.Y
	return e
}
func (v *doubleView) Frame(cells []tui.Cell, cols, rows int, cursor tui.Point, _ tui.CursorShape, show bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.frames++
	var b strings.Builder
	for i, c := range cells {
		if i > 0 && i%cols == 0 {
			b.WriteByte('\n')
		}
		b.WriteString(c.Content)
	}
	v.text, v.cursor, v.show = b.String(), cursor, show
}
func (v *doubleView) Detached() { v.mu.Lock(); v.detached = true; v.mu.Unlock() }

func (v *doubleView) state() (frames int, text string, cursor tui.Point, show, detached bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.frames, v.text, v.cursor, v.show, v.detached
}

// TestATerminalViewSizesDrawsAndPointsInItsOwnCells: with a view, the screen and the program get
// the view's size on every pass, an unchanged relayout included; the terminal draws nothing in the
// window and hands the view one frame per render; the pointer reaches the program in the view's
// cells; the window has no cell cursor and the view's caret shows while the terminal is focused;
// taken off, the view is told Detached and the terminal is the window's again.
func TestATerminalViewSizesDrawsAndPointsInItsOwnCells(t *testing.T) {
	f := startTerm(t, 20, 6)
	v := &doubleView{}
	f.h.onLoop(func() { v.owner = f.term; f.term.SetTerminalView(v) })
	f.h.waitFor("the program at the view's size", func() bool { return f.proc().lastSize() == [2]int{12, 40} })
	for range 3 { // unchanged relayouts: the size stays the view's
		f.h.onLoop(func() { f.term.Context().RequestLayout() })
		f.h.sync()
	}
	f.proc().mu.Lock()
	sizes := append([][2]int(nil), f.proc().sizes...)
	f.proc().mu.Unlock()
	if last := sizes[len(sizes)-1]; last != [2]int{12, 40} {
		t.Fatalf("sizes %v: the window's size came back", sizes)
	}

	f.output("hello")
	f.h.waitFor("the frame", func() bool { _, text, _, _, _ := v.state(); return strings.Contains(text, "hello") })
	if strings.Contains(termScreen(f), "hello") {
		t.Fatalf("the terminal drew in the window:\n%s", termScreen(f))
	}
	before, _, _, _, _ := v.state()
	f.h.onLoop(func() { f.term.MarkDirty() })
	f.h.sync()
	if after, _, _, _, _ := v.state(); after != before+1 {
		t.Fatalf("one render handed %d frames, want 1", after-before)
	}
	_, _, cur, show, _ := v.state()
	if cur != (tui.Point{X: 5, Y: 0}) || !show {
		t.Fatalf("the frame's caret %+v show %v, want after hello, shown (focused)", cur, show)
	}
	var x, y int
	var ok bool
	f.h.onLoop(func() { x, y, ok = f.term.Cursor() })
	if ok {
		t.Fatalf("a window-cell cursor %d,%d with a view", x, y)
	}

	f.output("\x1b[?1000;1006h")
	f.h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 2, Y: 1})
	f.waitSent("\x1b[<0;5;3M") // (2,1) doubled to (4,2), one-based

	f.h.onLoop(func() { f.term.SetTerminalView(nil) })
	f.h.waitFor("the window's size again", func() bool { return f.proc().lastSize() == [2]int{6, 20} })
	if _, _, _, _, detached := v.state(); !detached {
		t.Fatal("the view taken off was not told Detached")
	}
	f.h.waitFor("drawn in the window again", func() bool { return strings.Contains(termScreen(f), "hello") })
}

// TestTerminalTabsGiveEachTabItsOwnView: SetTerminalViews makes one view per tab with that tab's
// terminal, for tabs added later too; a closed tab's view is Detached; nil takes them all off.
func TestTerminalTabsGiveEachTabItsOwnView(t *testing.T) {
	tabs := widget.NewTerminalTabs()
	var made []*doubleView
	h := startApp(t, newShell(tabs), 30, 8)
	h.onLoop(func() {
		tabs.SetTerminalViews(func(term *widget.Terminal) widget.TerminalView {
			v := &doubleView{owner: term}
			made = append(made, v)
			return v
		})
	})
	var ids []widget.TerminalTabID
	h.onLoop(func() {
		for _, name := range []string{"a", "b"} {
			id, err := tabs.Add(name, widget.WithStarter(func(pty.Cmd) (widget.TerminalProcess, error) { return newFakeProc(), nil }))
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
	})
	h.sync()
	h.onLoop(func() {
		if len(made) != 2 || made[0].owner == made[1].owner {
			t.Fatalf("%d views made for 2 tabs, owners distinct %v", len(made), len(made) == 2 && made[0].owner != made[1].owner)
		}
		for i, v := range made {
			if v.owner.TerminalView() != widget.TerminalView(v) {
				t.Errorf("tab %d: its terminal does not read its own view back", i)
			}
		}
	})
	h.onLoop(func() { _, _ = tabs.Remove(ids[0]) })
	h.sync()
	if _, _, _, _, detached := made[0].state(); !detached {
		t.Fatal("a closed tab's view was not told Detached")
	}
	h.onLoop(func() { tabs.SetTerminalViews(nil) })
	if _, _, _, _, detached := made[1].state(); !detached {
		t.Fatal("SetTerminalViews(nil) left a view on")
	}
}

// termScreen is the fixture's rows, top to bottom.
func termScreen(f *termFixture) string {
	var rows []string
	for y := range 6 {
		rows = append(rows, f.h.row(y))
	}
	return strings.Join(rows, "\n")
}
