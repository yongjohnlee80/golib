package widget_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// titleEditor is switchEditor's Editor in a Box titled title, its switch on the box's title row
// (SetModeSwitchInTitle, held in the box's title slot), at w×10, under th (nil: the default),
// with the text in page (zero: the default) and the title aligned as align.
func titleEditor(t *testing.T, w int, title string, align style.Align, th *style.Theme, page ...style.Style) (*harness, *shell, *widget.Editor, *widget.Box) {
	t.Helper()
	opts := []widget.EditorOption{widget.WithCore(widget.CoreInitialText("name,n\nann,1\nbob,2\n")), widget.EditorModeSwitch(true)}
	if len(page) > 0 {
		opts = append(opts, widget.WithEditorStyles(widget.TextInputStyles{Text: page[0]}))
	}
	ed := widget.NewEditor(opts...)
	ed.SetModeSwitchInTitle(true)
	ed.SetDocumentView(widget.NewTableDocument(ed.Core(), ','))
	ed.SetRenderedDocument(true)
	box := widget.NewBox(ed, widget.WithTitle(title), widget.WithTitleAlign(align), widget.WithTitleSlot(ed.TitleSwitch()))
	sh := newShell(box)
	var appOpts []tui.AppOption
	if th != nil {
		appOpts = append(appOpts, tui.WithTheme(th))
	}
	h := startAppOpts(t, sh, w, 10, appOpts...)
	h.onLoop(func() { h.app.FocusInto(ed) })
	h.barrier(sh)
	return h, sh, ed, box
}

// bold reports whether c is drawn bold: the switch's mark on the chosen segment.
func bold(c tui.Cell) bool { return c.Attrs.Mask&tui.AttrBold != 0 }

// switchCol is the column the switch's first cell is drawn at on row y, -1 for none.
func switchCol(h *harness, y int) int {
	r := h.row(y)
	if i := strings.Index(r, widget.ModeSwitchRaw); i >= 0 {
		return len([]rune(r[:i]))
	}
	return -1
}

// The switch sits on its frame's title row, right-aligned inside the corners with a border cell
// after it, the title before it; the editor gives up no row, so its text starts on its own first
// row (the box's second). A press on a segment there chooses its view, and Ctrl+T and SetView move
// the mark the switch draws, so the frame's switch follows the editor wherever the choice is made.
func TestTheSwitchSitsOnItsFramesTitleRow(t *testing.T) {
	h, sh, ed, _ := titleEditor(t, 50, "notes.csv", style.AlignLeft, nil)
	defer h.stop()
	top := h.row(0)
	if !strings.HasPrefix(top, "┌ notes.csv ") || !strings.HasSuffix(top, widget.ModeSwitchRaw+widget.ModeSwitchSep+widget.ModeSwitchRendered+"─┐") {
		t.Fatalf("the title row %q; want the title at its left and the switch at its right, a border cell after it", top)
	}
	if !strings.HasPrefix(h.row(1), "│name,n") {
		t.Fatalf("row 1 %q; want the text's first line on the editor's first row: no switch row", h.row(1))
	}
	x := switchCol(h, 0)
	if want := 50 - 2 - widget.ModeSwitchWidth; x != want {
		t.Errorf("the switch at column %d; want %d", x, want)
	}
	h.inject(pressAt(x+len(widget.ModeSwitchRaw)+3, 0))
	h.barrier(sh)
	if chosen(h, ed) != widget.ViewRendered {
		t.Fatalf("a press on Rendered in the title row: %v; want Rendered", chosen(h, ed))
	}
	if !bold(h.tb.Snapshot()[0][x+len(widget.ModeSwitchRaw)+3]) {
		t.Error("Rendered chosen, its segment isn't marked (bold)")
	}
	h.inject(pressAt(x+2, 0))
	h.barrier(sh)
	if chosen(h, ed) != widget.ViewRaw {
		t.Fatalf("a press on Raw in the title row: %v; want Raw", chosen(h, ed))
	}
	h.inject(ctrlEv('t'))
	h.barrier(sh)
	if chosen(h, ed) != widget.ViewRendered || !bold(h.tb.Snapshot()[0][x+len(widget.ModeSwitchRaw)+3]) {
		t.Error("Ctrl+T chose Rendered, but the title row's switch doesn't show it")
	}
	h.onLoop(func() { ed.SetView(widget.ViewRaw) })
	h.barrier(sh)
	if !bold(h.tb.Snapshot()[0][x+2]) {
		t.Error("SetView chose Raw, but the title row's switch doesn't show it")
	}
}

// A press on the title row's switch reaches it with the focus elsewhere (the page shown takes the
// editor's focus as it arrives, which skipped a press that focused the editor first), and leaves
// the focus where it was.
func TestATitleRowSwitchTakesAPressWithTheFocusElsewhere(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("name,n\nann,1\n")), widget.EditorModeSwitch(true))
	ed.SetModeSwitchInTitle(true)
	ed.SetDocumentView(widget.NewTableDocument(ed.Core(), ','))
	ed.SetRenderedDocument(true)
	ed.SetView(widget.ViewRendered)
	other := widget.NewTextInput()
	box := widget.NewBox(ed, widget.WithTitle("t"), widget.WithTitleSlot(ed.TitleSwitch()))
	sh := newShell(widget.NewSplit(widget.Horizontal, other, box))
	h := startApp(t, sh, 90, 8)
	defer h.stop()
	h.onLoop(func() { h.app.FocusInto(other) })
	h.barrier(sh)
	x := switchCol(h, 0)
	if x < 0 {
		t.Fatalf("no switch on the title row:\n%s", h.grid())
	}
	h.inject(pressAt(x+2, 0))
	h.barrier(sh)
	if chosen(h, ed) != widget.ViewRaw {
		t.Fatalf("a press on Raw with the focus in another widget: %v; want Raw", chosen(h, ed))
	}
	var stays bool
	h.onLoop(func() { stays = h.app.FocusWithin(other) })
	if !stays {
		t.Error("the press on the title row's switch moved the focus")
	}
}

// A press on the title row outside the switch (on the title, on the border) does nothing: the view
// stays, the focus stays, and the content child isn't reached (its cursor doesn't move). The wheel
// on the switch does nothing either: the text doesn't scroll.
func TestThePressOrWheelOnTheTitleRowOutsideTheSwitchReachesNothing(t *testing.T) {
	h, sh, ed, _ := titleEditor(t, 50, "notes.csv", style.AlignLeft, nil)
	defer h.stop()
	var ln0, col0 int
	h.onLoop(func() { ed.Core().SetLine(2, 1); ln0, col0 = ed.Core().Line() })
	h.barrier(sh)
	for _, x := range []int{3, 15, 0, 49} { // the title, the border between, the corners
		h.inject(pressAt(x, 0))
		h.barrier(sh)
		var ln, col int
		var focused bool
		h.onLoop(func() { ln, col = ed.Core().Line(); focused = h.app.FocusWithin(ed) })
		if chosen(h, ed) != widget.ViewRaw || ln != ln0 || col != col0 || !focused {
			t.Errorf("a press at %d,0: view %v, cursor %d:%d (was %d:%d), focus in the editor %v; want nothing changed",
				x, chosen(h, ed), ln, col, ln0, col0, focused)
		}
	}
	x := switchCol(h, 0)
	before := h.grid()
	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: x + 2, Y: 0})
	h.barrier(sh)
	if chosen(h, ed) != widget.ViewRaw || h.grid() != before {
		t.Errorf("the wheel on the title row's switch changed the screen or the view (%v):\n%s", chosen(h, ed), h.grid())
	}
}

// The title truncates before the switch, never under it; a right-aligned title puts the switch at
// the row's left end instead, one border cell after the corner. A frame too narrow for both (inner
// width under the switch's + 4) keeps its title and hides the switch, and a press where the switch
// would be is no switch's.
func TestTheTitleRowSharesItsRowWithTheSwitch(t *testing.T) {
	long := strings.Repeat("a long title ", 6)
	h, _, _, _ := titleEditor(t, 50, long, style.AlignLeft, nil)
	top := h.row(0)
	x := switchCol(h, 0)
	if x < 0 || !strings.Contains(top[:strings.Index(top, widget.ModeSwitchRaw)], "…") {
		t.Errorf("a long title %q; want it cut with … before the switch", top)
	}
	h.stop()

	h, _, _, _ = titleEditor(t, 50, "notes.csv", style.AlignRight, nil)
	if top := h.row(0); !strings.HasPrefix(top, "┌─"+widget.ModeSwitchRaw) || !strings.HasSuffix(top, " notes.csv ┐") {
		t.Errorf("a right-aligned title %q; want the switch at the left end, the title at the right", top)
	}
	h.stop()

	narrow := 2 + widget.ModeSwitchWidth + 3 // inner = switch + 3: one short of the room it needs
	h, sh, ed, _ := titleEditor(t, narrow, "notes.csv", style.AlignLeft, nil)
	defer h.stop()
	if top := h.row(0); strings.Contains(top, widget.ModeSwitchRaw) || !strings.HasPrefix(top, "┌ notes") {
		t.Errorf("%d columns: the title row %q; want the title kept and the switch hidden", narrow, top)
	}
	h.inject(pressAt(narrow-4, 0))
	h.barrier(sh)
	if chosen(h, ed) != widget.ViewRaw {
		t.Errorf("a press where a hidden switch would be chose %v", chosen(h, ed))
	}
}

// The switch takes no room on the title row while there is nothing to switch to (no document
// view), and comes when one is set; SetModeSwitchInTitle(false) moves it back to the editor's first
// row, and the title row has none, so it is never shown twice.
func TestTheTitleRowsSwitchFollowsTheEditor(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("name,n\nann,1\n")), widget.EditorModeSwitch(true))
	ed.SetModeSwitchInTitle(true)
	box := widget.NewBox(ed, widget.WithTitle("notes.csv"), widget.WithTitleSlot(ed.TitleSwitch()))
	sh := newShell(box)
	h := startApp(t, sh, 50, 8)
	defer h.stop()
	if strings.Contains(h.grid(), widget.ModeSwitchRaw) || !strings.HasPrefix(h.row(1), "│name,n") {
		t.Fatalf("no document view: want no switch anywhere and the text on the first row:\n%s", h.grid())
	}
	h.onLoop(func() {
		ed.SetDocumentView(widget.NewTableDocument(ed.Core(), ','))
		ed.SetRenderedDocument(true)
	})
	h.barrier(sh)
	if switchCol(h, 0) < 0 || !strings.HasPrefix(h.row(1), "│name,n") {
		t.Fatalf("a document view set: want the switch on the title row, the text still on the first:\n%s", h.grid())
	}
	h.onLoop(func() { ed.SetModeSwitchInTitle(false) })
	h.barrier(sh)
	if switchCol(h, 0) >= 0 || switchCol(h, 1) < 0 || strings.Count(h.grid(), widget.ModeSwitchRaw) != 1 {
		t.Errorf("back on the editor's row: want one switch, on the box's second row:\n%s", h.grid())
	}
}

// An editor whose switch is on the title row but which no frame holds keeps it on its own first
// row: the switch is never lost.
func TestATitleSwitchNoFrameHoldsStaysOnTheEditorsRow(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("name,n\nann,1\n")), widget.EditorModeSwitch(true))
	ed.SetModeSwitchInTitle(true)
	ed.SetDocumentView(widget.NewTableDocument(ed.Core(), ','))
	ed.SetRenderedDocument(true)
	sh := newShell(ed)
	h := startApp(t, sh, 40, 6)
	defer h.stop()
	if switchCol(h, 0) != 40-widget.ModeSwitchWidth || !strings.HasPrefix(h.row(1), "name,n") {
		t.Errorf("no frame holds the switch: want it on the editor's first row:\n%s", h.grid())
	}
}

// The title row's switch reads in a sepia page and a dark one: its segments wear the title row's
// colours (the title's on the row's), the chosen segment bold and underlined, an unavailable
// Rendered faint, never a colour of its own. A golden per theme.
func TestTheTitleRowsSwitchReadsInEveryTheme(t *testing.T) {
	for name, th := range switchThemes {
		t.Run(name, func(t *testing.T) {
			w := 50
			h, sh, ed, _ := titleEditor(t, w, "notes.csv", style.AlignLeft, &th, switchPages[name])
			defer h.stop()
			x := switchCol(h, 0)
			var b strings.Builder
			look := func(state string) {
				cells := h.tb.Snapshot()[0]
				fmt.Fprintf(&b, "%s\n  title    %s\n  Raw      %s\n  |        %s\n  Rendered %s\n  border   %s\n", state,
					cellLook(cells[3]), cellLook(cells[x+2]), cellLook(cells[x+len(widget.ModeSwitchRaw)]),
					cellLook(cells[x+len(widget.ModeSwitchRaw)+3]), cellLook(cells[w-2]))
			}
			look("Raw chosen")
			h.inject(ctrlEv('t'))
			h.barrier(sh)
			look("Rendered chosen")
			h.onLoop(func() { ed.SetView(widget.ViewRaw); ed.SetRenderedDocument(false) })
			h.barrier(sh)
			look("Rendered unavailable")
			got := b.String()
			path := filepath.Join("testdata", "modeswitch", "title-"+name+".golden")
			if *updateSwitch {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s: %v (-update-modeswitch)", path, err)
			}
			if got != string(want) {
				t.Errorf("%s differs:\n--- got\n%s--- want\n%s", path, got, want)
			}
			// the segments wear the title row's colours: the title's on the row's, no patch of their own
			cells := h.tb.Snapshot()
			if title, sw := cells[0][3].Attrs, cells[0][x+2].Attrs; title.FG != sw.FG || title.BG != sw.BG {
				t.Errorf("the title row's switch colours %+v differ from the title's %+v", sw, title)
			}
		})
	}
}
