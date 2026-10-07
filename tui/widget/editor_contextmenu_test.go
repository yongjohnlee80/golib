package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func rightClick(x, y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: x, Y: y}
}

// ctxFixture mounts an editor with text under an OverlayHost (where the menu
// opens) inside a shell (which records what bubbles past the editor).
func ctxFixture(t *testing.T, w, hgt int, opts ...widget.EditorOption) (*harness, *widget.Editor, *shell) {
	t.Helper()
	ed := widget.NewEditor(append([]widget.EditorOption{widget.WithInitialText("hello world\nsecond line")}, opts...)...)
	sh := newShell(widget.NewOverlayHost(ed))
	h := startApp(t, sh, w, hgt)
	h.inject(click(0, 0)) // focus the editor; the cursor goes to 1:1
	h.settle()
	return h, ed, sh
}

// panelCorner is the cell of the menu panel's top-left border corner, read
// from the snapshot cell by cell. (cellOfLabel counts BYTES, and the panel's
// border runes are three bytes each, so it cannot place anything inside it.)
func panelCorner(t *testing.T, h *harness) (x, y int) {
	t.Helper()
	for row, cells := range h.tb.Snapshot() {
		for col, c := range cells {
			if c.Content == "┌" {
				return col, row
			}
		}
	}
	t.Fatalf("no menu panel on screen:\n%s", h.grid())
	return 0, 0
}

func menuOpen(h *harness, ed *widget.Editor) bool {
	var open bool
	h.onLoop(func() { open = ed.ContextMenuOpen() })
	return open
}

// OFF UNLESS ASKED: a right press on an editor without the option bubbles to
// its parents, as before this menu existed.
func TestEditorContextMenu_OffByDefaultTheRightPressBubbles(t *testing.T) {
	ed := widget.NewEditor(widget.WithInitialText("hello"))
	sh := newShell(widget.NewOverlayHost(ed))
	h := startApp(t, sh, 30, 6)
	h.inject(rightClick(2, 0))
	h.settle()
	bubbled := false
	for _, m := range sh.bubbledMice() {
		bubbled = bubbled || (m.Kind == tui.MousePress && m.Button == tui.MouseRight)
	}
	if !bubbled {
		t.Error("the right press did not reach the editor's parent")
	}
	h.wantNotContains("Copy")
}

// THE MENU OPENS AT THE POINTER, below and to the right of it, with the three
// stock rows.
func TestEditorContextMenu_OpensAtThePointer(t *testing.T) {
	h, ed, _ := ctxFixture(t, 40, 12, widget.WithContextMenu(nil))
	h.inject(rightClick(5, 1))
	h.waitFor("the menu opened", func() bool { return menuOpen(h, ed) })
	h.settle()
	if x, y := panelCorner(t, h); x != 5 || y != 2 {
		t.Errorf("the panel's corner is at %d,%d; want 5,2, just below the pointer at 5,1:\n%s", x, y, h.grid())
	}
	_, cy := cellOfLabel(t, h, "Copy")
	_, uy := cellOfLabel(t, h, "Cut")
	_, py := cellOfLabel(t, h, "Paste")
	if cy != 3 || uy != 4 || py != 5 {
		t.Errorf("rows at %d, %d, %d; want Copy, Cut, Paste on 3, 4, 5", cy, uy, py)
	}
}

// NEAR THE BOTTOM EDGE IT OPENS ABOVE the pointer instead of off screen.
func TestEditorContextMenu_FlipsAboveAtTheBottomEdge(t *testing.T) {
	h, ed, _ := ctxFixture(t, 40, 8, widget.WithContextMenu(nil))
	h.inject(rightClick(3, 7))
	h.waitFor("the menu opened", func() bool { return menuOpen(h, ed) })
	h.settle()
	if _, y := cellOfLabel(t, h, "Paste"); y >= 7 {
		t.Errorf("Paste is at row %d; the menu did not open above row 7", y)
	}
}

// COPY ON A SELECTION: the register and the system clipboard get the selected
// text, the menu closes, and focus is back on the editor.
func TestEditorContextMenu_CopyTakesTheSelection(t *testing.T) {
	h, ed, _ := ctxFixture(t, 40, 12, widget.WithContextMenu(nil))
	h.inject(typeString("vllll")...) // Visual over "hello"
	h.settle()
	h.inject(rightClick(2, 0))
	h.waitFor("the menu opened", func() bool { return menuOpen(h, ed) })
	h.settle()
	h.inject(key(tui.KeyEnter)) // Copy is the first row, and enabled
	h.waitFor("the menu closed", func() bool { return !menuOpen(h, ed) })
	var reg string
	var focused bool
	h.onLoop(func() {
		reg, _ = ed.Register()
		focused = ed.Context().Focused()
	})
	if reg != "hello" {
		t.Errorf("register = %q, want %q", reg, "hello")
	}
	if got := string(h.tb.Clipboard()); got != "hello" {
		t.Errorf("clipboard = %q, want %q", got, "hello")
	}
	if !focused {
		t.Error("focus did not return to the editor")
	}
	h.wantNotContains("Paste")
}

// WITHOUT A SELECTION, Copy and Cut are disabled and Enter lands on Paste —
// which is disabled too while the register is empty, so nothing is chosen.
// Once the register holds text, Paste pastes it.
func TestEditorContextMenu_PasteNeedsARegisterAndCopyNeedsASelection(t *testing.T) {
	h, ed, _ := ctxFixture(t, 40, 12, widget.WithContextMenu(nil))
	var rows []widget.MenuItemModel
	h.onLoop(func() { rows = widget.EditorContextItems(ed) })
	for _, r := range rows {
		if r.Enabled {
			t.Errorf("%s is enabled with no selection and an empty register", r.Label)
		}
	}

	h.onLoop(func() { ed.SetRegister("XY", false) })
	h.inject(rightClick(0, 0))
	h.waitFor("the menu opened", func() bool { return menuOpen(h, ed) })
	h.settle()
	h.inject(key(tui.KeyEnter)) // the only enabled row: Paste
	h.waitFor("the menu closed", func() bool { return !menuOpen(h, ed) })
	var v string
	h.onLoop(func() { v = ed.Value() })
	if !strings.HasPrefix(v, "XYhello") {
		t.Errorf("value = %q, want the register pasted before the cursor", v)
	}
}

// READ-ONLY: Cut and Paste are disabled whatever the selection and register.
func TestEditorContextMenu_ReadOnlyDisablesCutAndPaste(t *testing.T) {
	h, ed, _ := ctxFixture(t, 40, 12, widget.WithContextMenu(nil), widget.WithEditorReadOnly(true))
	h.inject(typeString("vll")...)
	h.settle()
	h.onLoop(func() { ed.SetRegister("XY", false) })
	var rows []widget.MenuItemModel
	h.onLoop(func() { rows = widget.EditorContextItems(ed) })
	for _, r := range rows {
		want := r.ID == widget.EditorMenuCopy
		if r.Enabled != want {
			t.Errorf("%s enabled = %v on a read-only editor with a selection", r.Label, r.Enabled)
		}
	}
}

// LIGHT DISMISSAL: Escape closes the menu, and so does a press outside it —
// which is not passed on to the editor underneath.
func TestEditorContextMenu_EscapeAndAnOutsidePressClose(t *testing.T) {
	h, ed, _ := ctxFixture(t, 40, 12, widget.WithContextMenu(nil))
	h.inject(rightClick(5, 0))
	h.waitFor("the menu opened", func() bool { return menuOpen(h, ed) })
	h.inject(key(tui.KeyEscape))
	h.waitFor("Escape closed it", func() bool { return !menuOpen(h, ed) })

	h.inject(rightClick(5, 0))
	h.waitFor("the menu opened again", func() bool { return menuOpen(h, ed) })
	h.settle()
	h.inject(click(30, 1)) // outside the panel, on the editor's second line
	h.waitFor("the outside press closed it", func() bool { return !menuOpen(h, ed) })
	var line int
	h.onLoop(func() { line, _ = ed.Line() })
	if line != 0 {
		t.Errorf("the dismissing press moved the cursor to line %d: it reached the editor", line)
	}
}

// A CONSUMER'S ROWS: the builder runs at open, its rows' EditorMenuActions run
// against the editor, and the stock rows can be kept beside them.
func TestEditorContextMenu_ConsumerRowsRun(t *testing.T) {
	var ran int
	build := func(e *widget.Editor) []widget.MenuItemModel {
		rows := widget.EditorContextItems(e)
		return append(rows, widget.NewCommand("upper", "Upper",
			widget.EditorMenuAction{ID: "upper", Run: func(e *widget.Editor) {
				ran++
				e.SetValue(strings.ToUpper(e.Value()))
			}}))
	}
	h, ed, _ := ctxFixture(t, 40, 12, widget.WithContextMenu(build))
	h.inject(rightClick(1, 0))
	h.waitFor("the menu opened", func() bool { return menuOpen(h, ed) })
	h.settle()
	h.wantContains("Copy")
	px, _ := panelCorner(t, h)
	_, y := cellOfLabel(t, h, "Upper")
	x := px + 2 // inside the border, on the row's label
	h.inject(click(x, y))
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
	h.waitFor("the menu closed", func() bool { return !menuOpen(h, ed) })
	var v string
	h.onLoop(func() { v = ed.Value() })
	if ran != 1 || !strings.HasPrefix(v, "HELLO WORLD") {
		t.Errorf("ran %d, value %q", ran, v)
	}
}

// THE POINTER, NOT THE EDITOR'S LOCAL FRAME: an editor that does not sit at
// the host's origin still gets its menu at the pointer. The host resolves the
// editor's position (the popup layer, a sibling, cannot), and the click's
// editor-local point is added to it.
func TestEditorContextMenu_OpensAtThePointerWhenTheEditorIsInset(t *testing.T) {
	ed := widget.NewEditor(widget.WithInitialText("hello world\nsecond line"), widget.WithContextMenu(nil))
	h := startApp(t, widget.NewOverlayHost(widget.NewBox(ed)), 40, 12) // the editor starts at 1,1
	h.inject(click(1, 1))
	h.settle()
	h.inject(rightClick(6, 2)) // screen 6,2 is the editor's 5,1
	h.waitFor("the menu opened", func() bool { return menuOpen(h, ed) })
	h.settle()
	// The box's own corner is at 0,0; the panel's is the other one.
	var corners [][2]int
	for row, cells := range h.tb.Snapshot() {
		for col, c := range cells {
			if c.Content == "┌" {
				corners = append(corners, [2]int{col, row})
			}
		}
	}
	if len(corners) != 2 || corners[1] != [2]int{6, 3} {
		t.Errorf("corners %v; want the panel's at 6,3, just below the pointer at 6,2:\n%s", corners, h.grid())
	}
}
