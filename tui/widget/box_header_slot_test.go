package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
	"github.com/yongjohnlee80/golib/tui/widget/table"
)

// headTabsEditor is an Editor in a Box titled title, w×10, its switch on the box's title row and
// its tabs (the first active) on the box's first row inside while on; chose records each tab chosen.
func headTabsEditor(t *testing.T, w int, title string, on bool, tabs ...core.StripTab) (*harness, *shell, *editor.Editor, *[]string) {
	t.Helper()
	ed := editor.NewEditor(editor.WithCore(editor.CoreInitialText("name,n\nann,1\n")), editor.EditorModeSwitch(true))
	ed.SetModeSwitchInTitle(true)
	ed.SetDocumentView(table.NewTableDocument(ed.Core(), ','))
	ed.SetRenderedDocument(true)
	ed.SetTabsInFrame(on)
	var chose []string
	strip := ed.Tabs()
	strip.SetOnSelect(func(id string) { chose = append(chose, id); strip.SetActive(id) })
	strip.SetTabs(tabs)
	if len(tabs) > 0 {
		strip.SetActive(tabs[0].ID)
	}
	box := core.NewBox(ed, core.WithTitle(title), core.WithTitleSlot(ed.TitleSwitch()))
	box.SetHeaderSlot(ed.FrameTabs())
	sh := newShell(box)
	h := startApp(t, sh, w, 10)
	h.onLoop(func() { h.app.FocusInto(ed) })
	h.barrier(sh)
	return h, sh, ed, &chose
}

// plainBottom is a w-wide box's plain bottom border.
func plainBottom(w int) string { return "└" + strings.Repeat("─", w-2) + "┘" }

// The tabs sit on the first row inside the frame, under the title row, in opening order, as a panel
// of terminals carries its tabs; the title row keeps the title and the switch whole, as a box
// without tabs draws them, the bottom border is plain, and the editor starts a row lower. A press on
// a tab chooses it.
func TestTheTabsSitOnTheRowUnderTheTitle(t *testing.T) {
	h, sh, _, chose := headTabsEditor(t, 70, "notes.csv", true, twoTabs...)
	defer h.stop()
	top, strip := h.row(0), h.row(1)
	if !strings.HasPrefix(top, "┌ notes.csv ") || col(h, 0, "alpha") >= 0 || col(h, 0, "beta") >= 0 {
		t.Fatalf("the title row %q; want the title and no tabs", top)
	}
	if switchCol(h, 0) != 70-2-editor.ModeSwitchWidth {
		t.Fatalf("the title row %q; want the switch at its right, where it is without tabs", top)
	}
	a, b := col(h, 1, "alpha"), col(h, 1, "beta")
	if !strings.HasPrefix(strip, "│") || !strings.HasSuffix(strip, "│") || !(0 < a && a < b) {
		t.Fatalf("row 1 %q; want the tabs inside the frame, alpha then beta", strip)
	}
	if bottom := h.row(9); bottom != plainBottom(70) {
		t.Fatalf("the bottom border %q; want a plain border", bottom)
	}
	if !strings.HasPrefix(h.row(2), "│name,n") || !strings.HasPrefix(h.row(8), "│") {
		t.Fatalf("rows 2 and 8 %q, %q; want the editor's, under the tabs", h.row(2), h.row(8))
	}
	h.inject(pressAt(b+1, 1))
	h.barrier(sh)
	if len(*chose) != 1 || (*chose)[0] != "2" {
		t.Fatalf("a press on beta chose %v; want [2]", *chose)
	}
}

// In a narrow frame the strip scrolls in its row (← … →), inside the frame; the title row keeps the
// switch whole at its right.
func TestNarrowHeaderTabsScrollInTheirRow(t *testing.T) {
	var many []core.StripTab
	for _, n := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		many = append(many, core.StripTab{ID: n, Label: n + ".md"})
	}
	h, _, _, _ := headTabsEditor(t, 40, "notes.csv", true, many...)
	defer h.stop()
	strip := h.row(1)
	if switchCol(h, 0) != 40-2-editor.ModeSwitchWidth {
		t.Fatalf("the title row %q; want the switch whole at its right", h.row(0))
	}
	if !strings.HasPrefix(strip, "│") || !strings.HasSuffix(strip, "│") || col(h, 1, "one.md") < 1 {
		t.Fatalf("row 1 %q; want the strip inside the frame, the active tab (one.md) shown", strip)
	}
	if l, r := col(h, 1, "←"), col(h, 1, "→"); l < 1 || r <= l {
		t.Fatalf("row 1 %q; want the strip scrolling (← … →)", strip)
	}
	if strings.Contains(strip, "seven") {
		t.Errorf("row 1 %q: all seven tabs drawn in 40 columns; want them scrolled", strip)
	}
}

// Tabs off take no row: the editor has the frame's whole inside, as in a box without them.
func TestHeaderTabsOffTakeNoRow(t *testing.T) {
	h, _, _, _ := headTabsEditor(t, 60, "notes.csv", false, twoTabs...)
	defer h.stop()
	if !strings.HasPrefix(h.row(1), "│name,n") || strings.Contains(h.grid(), "alpha") {
		t.Fatalf("tabs off: row 1 %q; want the editor's, no tabs:\n%s", h.row(1), h.grid())
	}
}

// Turned on after the first layout, the tabs take their row and the editor moves under it; turned
// off, they give it back.
func TestHeaderTabsTurnLive(t *testing.T) {
	h, sh, ed, _ := headTabsEditor(t, 60, "notes.csv", false, twoTabs...)
	defer h.stop()
	h.onLoop(func() { ed.SetTabsInFrame(true) })
	h.barrier(sh)
	if col(h, 1, "alpha") < 0 || !strings.HasPrefix(h.row(2), "│name,n") {
		t.Fatalf("turned on, rows 1 and 2 %q, %q; want the tabs, then the editor", h.row(1), h.row(2))
	}
	h.onLoop(func() { ed.SetTabsInFrame(false) })
	h.barrier(sh)
	if col(h, 1, "alpha") >= 0 || !strings.HasPrefix(h.row(1), "│name,n") {
		t.Fatalf("turned off, row 1 %q; want the editor's", h.row(1))
	}
}

// A capped box (SetMaximumWidth) carries the row inside its own frame, centred with it.
func TestACappedBoxCarriesItsHeaderRow(t *testing.T) {
	ed := editor.NewEditor(editor.WithCore(editor.CoreInitialText("x\n")))
	ed.SetTabsInFrame(true)
	ed.Tabs().SetTabs(twoTabs)
	box := core.NewBox(ed, core.WithTitle("t"))
	box.SetHeaderSlot(ed.FrameTabs())
	box.SetMaximumWidth(40)
	sh := newShell(box)
	h := startApp(t, sh, 80, 8)
	defer h.stop()
	corner, a := col(h, 0, "┌"), col(h, 1, "alpha")
	if corner != 20 || col(h, 1, "│") != 20 || a <= 20 || a >= 60 {
		t.Fatalf("rows 0 and 1 %q, %q; want the box at column 20, the tabs inside it", h.row(0), h.row(1))
	}
	if !strings.HasPrefix(strings.TrimLeft(h.row(2), " "), "│x") {
		t.Fatalf("row 2 %q; want the editor under the tabs", h.row(2))
	}
}

// A press on a header tab reaches the strip with the focus elsewhere, and leaves the focus there.
func TestAHeaderTabTakesAPressWithTheFocusElsewhere(t *testing.T) {
	ed := editor.NewEditor(editor.WithCore(editor.CoreInitialText("x\n")))
	ed.SetTabsInFrame(true)
	var chose []string
	ed.Tabs().SetOnSelect(func(id string) { chose = append(chose, id) })
	ed.Tabs().SetTabs(twoTabs)
	other := core.NewTextInput()
	box := core.NewBox(ed, core.WithTitle("t"))
	box.SetHeaderSlot(ed.FrameTabs())
	sh := newShell(core.NewSplit(core.Horizontal, other, box))
	h := startApp(t, sh, 90, 8)
	defer h.stop()
	h.onLoop(func() { h.app.FocusInto(other) })
	h.barrier(sh)
	b := col(h, 1, "beta")
	if b < 0 {
		t.Fatalf("no tabs on the row under the title:\n%s", h.grid())
	}
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: b + 1, Y: 1})
	h.barrier(sh)
	if len(chose) != 1 || chose[0] != "2" {
		t.Fatalf("a press on beta with the focus in another widget chose %v; want [2]", chose)
	}
	var stays bool
	h.onLoop(func() { stays = h.app.FocusWithin(other) })
	if !stays {
		t.Error("the press on a header tab moved the focus")
	}
}

// The header slot is the box's child as the others are: listed last, removed by Remove, nil after.
func TestTheHeaderSlotIsABoxChild(t *testing.T) {
	ed := editor.NewEditor()
	box := core.NewBox(ed, core.WithTitleSlot(ed.TitleSwitch()))
	box.SetHeaderSlot(ed.FrameTabs())
	var kids []tui.Component
	for c := range box.Children() {
		kids = append(kids, c)
	}
	if len(kids) != 3 || kids[2] != ed.FrameTabs() || box.HeaderSlot() != ed.FrameTabs() {
		t.Fatalf("children %v; want the editor, the switch, then the tabs", kids)
	}
	box.Remove(ed.FrameTabs())
	if box.HeaderSlot() != nil {
		t.Error("Remove left the header slot")
	}
}
