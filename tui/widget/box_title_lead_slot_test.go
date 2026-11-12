package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
	"github.com/yongjohnlee80/golib/tui/widget/table"
)

// leadTabsEditor is an Editor in a Box titled title, w×10, its switch on the box's title row and
// its tabs (the first active) at that row's left while on; chose records each tab chosen.
func leadTabsEditor(t *testing.T, w int, title string, on bool, tabs ...core.StripTab) (*harness, *shell, *[]string) {
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
	box.SetTitleLeadSlot(ed.FrameTabs())
	sh := newShell(box)
	h := startApp(t, sh, w, 10)
	h.onLoop(func() { h.app.FocusInto(ed) })
	h.barrier(sh)
	return h, sh, &chose
}

// The tabs sit at the title row's left, a border cell after the corner, in opening order, as a
// panel carries its tabs; the title follows them, the switch keeps the row's right, the bottom
// border is a plain border, and the editor gives up no row. A press on a tab chooses it.
func TestTheTabsSitAtTheTitleRowsLeft(t *testing.T) {
	h, sh, chose := leadTabsEditor(t, 70, "notes.csv", true, twoTabs...)
	defer h.stop()
	top, bottom := h.row(0), h.row(9)
	a, b, title := col(h, 0, "alpha"), col(h, 0, "beta"), col(h, 0, "notes.csv")
	if !strings.HasPrefix(top, "┌─") || !(1 < a && a < b && b < title) {
		t.Fatalf("the title row %q; want ┌─, alpha, beta, then the title", top)
	}
	if switchCol(h, 0) != 70-2-editor.ModeSwitchWidth {
		t.Fatalf("the title row %q; want the switch at its right, where it is without tabs", top)
	}
	if bottom != "└"+strings.Repeat("─", 68)+"┘" {
		t.Fatalf("the bottom border %q; want a plain border", bottom)
	}
	if !strings.HasPrefix(h.row(1), "│name,n") || !strings.HasPrefix(h.row(8), "│") {
		t.Fatalf("rows 1 and 8 %q, %q; want the editor's: the tabs take no row", h.row(1), h.row(8))
	}
	h.inject(pressAt(b+1, 0))
	h.barrier(sh)
	if len(*chose) != 1 || (*chose)[0] != "2" {
		t.Fatalf("a press on beta chose %v; want [2]", *chose)
	}
}

// On a narrow row the switch keeps its place and its width, and the tabs get the room left of it
// (the strip scrolling in it), never over the switch; the title gives way first.
func TestANarrowTitleRowKeepsTheSwitchWhole(t *testing.T) {
	tabs := []core.StripTab{{ID: "1", Label: "alpha"}, {ID: "2", Label: "beta"}, {ID: "3", Label: "gamma"}, {ID: "4", Label: "delta"}}
	h, _, _ := leadTabsEditor(t, 34, "a-long-title.csv", true, tabs...)
	defer h.stop()
	top := h.row(0)
	sw := switchCol(h, 0)
	if sw != 34-2-editor.ModeSwitchWidth {
		t.Fatalf("the title row %q; want the switch whole at its right (column %d), at %d", top, 34-2-editor.ModeSwitchWidth, sw)
	}
	// the strip scrolls in the room left: its arrows at the row's left, before the switch
	if l, r := col(h, 0, "←"), col(h, 0, "→"); l < 2 || r <= l || r >= sw {
		t.Fatalf("the title row %q; want the strip scrolling (← … →) at the row's left, before the switch", top)
	}
}

// Tabs off, or a strip with no tabs, take no room: the title row is drawn as a box without them.
func TestTitleRowTabsOffOrNoneTakeNoRoom(t *testing.T) {
	for _, c := range []struct {
		name string
		on   bool
		tabs []core.StripTab
	}{{"off", false, twoTabs}, {"none", true, nil}} {
		t.Run(c.name, func(t *testing.T) {
			h, _, _ := leadTabsEditor(t, 60, "notes.csv", c.on, c.tabs...)
			defer h.stop()
			if top := h.row(0); !strings.HasPrefix(top, "┌ notes.csv ") || col(h, 0, "alpha") >= 0 {
				t.Fatalf("the title row %q; want the title where it is without tabs", top)
			}
		})
	}
}

// The lead slot is the box's child as the others are: listed, removed by Remove, and nil after.
func TestTheTitleLeadSlotIsAChild(t *testing.T) {
	ed := editor.NewEditor()
	box := core.NewBox(ed, core.WithTitleSlot(ed.TitleSwitch()))
	box.SetTitleLeadSlot(ed.FrameTabs())
	var kids []any
	for c := range box.Children() {
		kids = append(kids, c)
	}
	if len(kids) != 3 || kids[2] != ed.FrameTabs() || box.TitleLeadSlot() != ed.FrameTabs() {
		t.Fatalf("children %v; want the editor, the switch, then the tabs", kids)
	}
	box.Remove(ed.FrameTabs())
	if box.TitleLeadSlot() != nil {
		t.Fatal("Remove left the lead slot")
	}
}
