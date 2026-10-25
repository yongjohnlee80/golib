package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// click_focus_test.go: a click takes the keyboard, as any floating window's does: a press on the
// editor window, floating or tiled, moves the keyboard out of an open non-modal Drawer and raises
// the window; a press on the Drawer takes it back; a modal Dialog traps it until it closes. Every
// reading waits on the input's own effect, or on the effect of input queued after it (input is
// dispatched in order): Screen.Keys only queues it.

// clickDoc is an 80×24 Window: the FloatWindow fw (columns 4 to 35 floating, the whole manager when
// App.fwTiled), a non-modal Drawer dr over the right from column 24, and a modal Dialog dlg.
const clickDoc = `import tui 1.0
import demo 1.0
Window {
 TileManager {
  FloatWindow { id: fw; floatX: 5; floatY: 10; floatWidth: 40; floatHeight: 60
    TileManager.edge: Tui.Center; TileManager.tiled: App.fwTiled
    Frame { title: "FW"; Editor { id: ed; text: "one two three" } } }
 }
 Drawer { id: dr; modal: false; edge: Tui.Right; size: 70; Frame { title: "DR"; Editor { id: dred; text: "drawer" } } }
 Dialog { id: dlg; title: "ASK"; TextField { id: tf } }
}`

// clickRig is clickDoc running, with the focus readable.
type clickRig struct {
	s *decltest.Screen
}

func runClick(t *testing.T, tiled bool) *clickRig {
	t.Helper()
	r := &clickRig{}
	r.s = decltest.Run(t, 80, 24, tuidecl.LayoutSource("main.qml", []byte(clickDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Sources(map[string]any{"App.fwTiled": tiled}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{}),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(widget.NewMinimizedWindows())))
	r.s.WaitForText(t, "FW")
	return r
}

// in reports whether the keyboard is inside the node id.
func (r *clickRig) in(t *testing.T, id string) bool {
	t.Helper()
	var within bool
	onScreenLoop(t, r.s, func() {
		c, _ := r.s.Program.Find(id)
		within = r.s.Program.App().FocusWithin(c)
	})
	return within
}

// waitIn waits for the keyboard to be inside id.
func (r *clickRig) waitIn(t *testing.T, id string) {
	t.Helper()
	r.s.WaitFor(t, "the keyboard in "+id, func(string) bool { return r.in(t, id) })
}

// on is the cell two into text where it's drawn.
func (r *clickRig) on(t *testing.T, text string) (x, y int) {
	t.Helper()
	x, y, ok := at(r.s.String(), text)
	if !ok {
		t.Fatalf("%q isn't drawn:\n%s", text, r.s.String())
	}
	return x + 2, y
}

// click presses and releases the left button at (x, y).
func (r *clickRig) click(t *testing.T, x, y int) {
	t.Helper()
	r.s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
}

// cellAt is the rune drawn at (x, y).
func (r *clickRig) cellAt(x, y int) rune {
	rows := strings.Split(r.s.String(), "\n")
	if y >= len(rows) {
		return 0
	}
	rs := []rune(rows[y])
	if x >= len(rs) {
		return ' '
	}
	return rs[x]
}

// A click on the editor window, floating and tiled, takes the keyboard from the open non-modal
// Drawer and raises the window over it: the Drawer's left border (column 24), drawn over the window
// before, is under it after.
func TestAClickOnTheEditorWindowTakesTheKeyboardFromANonModalDrawer(t *testing.T) {
	for _, tiled := range []bool{false, true} {
		r := runClick(t, tiled)
		call(t, r.s, "dr", "open")
		call(t, r.s, "dred", "forceActiveFocus")
		r.waitIn(t, "dred")
		r.s.WaitFor(t, "the Drawer over the window", func(string) bool { return r.cellAt(24, 6) == '│' })
		x, y := r.on(t, "one two")
		r.click(t, x, y)
		r.waitIn(t, "ed")
		r.s.WaitFor(t, "the window raised over the Drawer", func(string) bool { return r.cellAt(24, 6) != '│' })
		if r.in(t, "dred") {
			t.Fatalf("tiled %v: the Drawer's editor still holds the keyboard", tiled)
		}
	}
}

// A click on the Drawer takes the keyboard back from the editor window.
func TestAClickOnTheDrawerTakesTheKeyboardBack(t *testing.T) {
	r := runClick(t, false)
	call(t, r.s, "dr", "open")
	call(t, r.s, "ed", "forceActiveFocus")
	r.waitIn(t, "ed")
	x, y := r.on(t, "drawer")
	r.click(t, x, y)
	r.waitIn(t, "dred")
}

// A modal Dialog traps the keyboard: a click on the editor changes nothing (the Dialog's backdrop
// takes it), as text typed after it, landing in the Dialog's field, shows once both are dispatched;
// closing the Dialog gives the keyboard back to where it was before the Dialog opened, the Drawer's
// editor, not the editor that was clicked.
func TestAModalDialogKeepsTheKeyboardFromAClickAndGivesItBack(t *testing.T) {
	r := runClick(t, false)
	call(t, r.s, "dr", "open")
	call(t, r.s, "dred", "forceActiveFocus")
	r.waitIn(t, "dred")
	x, y := r.on(t, "one two") // the Dialog's backdrop hides it
	call(t, r.s, "dlg", "open")
	r.waitIn(t, "tf")
	r.click(t, x, y)
	r.s.Keys(t, decltest.Type("typed")...)
	r.s.WaitFor(t, "the text typed after the click in the Dialog's field", func(sc string) bool {
		return strings.Contains(sc, "typed")
	})
	if !r.in(t, "tf") || r.in(t, "ed") || strings.Contains(r.s.String(), "typedone") {
		t.Fatalf("a click outside the modal Dialog moved the keyboard:\n%s", r.s.String())
	}
	call(t, r.s, "dlg", "close")
	r.waitIn(t, "dred")
}
