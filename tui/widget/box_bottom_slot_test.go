package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// tabsEditor is an Editor in a Box titled title, w×10, its switch on the box's title row and its
// tabs (alpha active) on the box's bottom border while on, the box's status status; chose records
// each tab chosen.
func tabsEditor(t *testing.T, w int, title, status string, on bool, tabs ...widget.StripTab) (*harness, *shell, *widget.Editor, *[]string) {
	t.Helper()
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("name,n\nann,1\n")), widget.EditorModeSwitch(true))
	ed.SetModeSwitchInTitle(true)
	ed.SetDocumentView(widget.NewTableDocument(ed.Core(), ','))
	ed.SetRenderedDocument(true)
	ed.SetTabsInFrame(on)
	var chose []string
	strip := ed.Tabs()
	strip.SetOnSelect(func(id string) { chose = append(chose, id); strip.SetActive(id) })
	strip.SetTabs(tabs)
	if len(tabs) > 0 {
		strip.SetActive(tabs[0].ID)
	}
	box := widget.NewBox(ed, widget.WithTitle(title), widget.WithStatus(status),
		widget.WithTitleSlot(ed.TitleSwitch()), widget.WithBottomSlot(ed.FrameTabs()))
	sh := newShell(box)
	h := startApp(t, sh, w, 10)
	h.onLoop(func() { h.app.FocusInto(ed) })
	h.barrier(sh)
	return h, sh, ed, &chose
}

var twoTabs = []widget.StripTab{{ID: "1", Label: "alpha"}, {ID: "2", Label: "beta"}}

// col is the column, in cells, where sub starts on row y, -1 for none.
func col(h *harness, y int, sub string) int {
	r := h.row(y)
	if i := strings.Index(r, sub); i >= 0 {
		return len([]rune(r[:i]))
	}
	return -1
}

// The tabs sit on the frame's bottom border, a border cell after the corner, in opening order; the
// title row keeps the title and the switch whole, as a box without tabs draws them, and the editor
// gives up no row. A press on a tab chooses it.
func TestTheTabsSitOnTheFramesBottomBorder(t *testing.T) {
	h, sh, _, chose := tabsEditor(t, 60, "notes.csv", "", true, twoTabs...)
	defer h.stop()
	top, bottom := h.row(0), h.row(9)
	if !strings.HasPrefix(top, "┌ notes.csv ") || col(h, 0, "alpha") >= 0 || switchCol(h, 0) != 60-2-widget.ModeSwitchWidth {
		t.Fatalf("the title row %q; want the title and the switch, as without tabs", top)
	}
	a, b := col(h, 9, "alpha"), col(h, 9, "beta")
	if !strings.HasPrefix(bottom, "└─") || !strings.HasSuffix(bottom, "─┘") || !(1 < a && a < b) {
		t.Fatalf("the bottom border %q; want └─, alpha, beta, ─┘", bottom)
	}
	if !strings.HasPrefix(h.row(1), "│name,n") || !strings.HasPrefix(h.row(8), "│") {
		t.Fatalf("rows 1 and 8 %q, %q; want the editor's: the tabs take no row", h.row(1), h.row(8))
	}
	h.inject(pressAt(b+1, 9))
	h.barrier(sh)
	if len(*chose) != 1 || (*chose)[0] != "2" {
		t.Fatalf("a press on beta chose %v; want [2]", *chose)
	}
}

// Tabs off, or a strip with no tabs, take no room: the bottom border is drawn as a box without them.
func TestTabsOffOrNoneTakeNoRoom(t *testing.T) {
	for _, c := range []struct {
		name string
		on   bool
		tabs []widget.StripTab
	}{
		{"off", false, twoTabs},
		{"no tabs", true, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, _, _, _ := tabsEditor(t, 50, "notes.csv", "", c.on, c.tabs...)
			defer h.stop()
			if bottom := h.row(9); bottom != "└"+strings.Repeat("─", 48)+"┘" {
				t.Fatalf("the bottom border %q; want a plain border", bottom)
			}
		})
	}
}

// Turned on after the first layout, the tabs take their place; turned off, they give it back.
func TestTabsInFrameTurnsLive(t *testing.T) {
	h, sh, ed, _ := tabsEditor(t, 60, "notes.csv", "", false, twoTabs...)
	defer h.stop()
	h.onLoop(func() { ed.SetTabsInFrame(true) })
	h.barrier(sh)
	if col(h, 9, "alpha") < 0 {
		t.Fatalf("turned on, the bottom border %q; want alpha", h.row(9))
	}
	h.onLoop(func() { ed.SetTabsInFrame(false) })
	h.barrier(sh)
	if col(h, 9, "alpha") >= 0 {
		t.Fatalf("turned off, the bottom border %q; want no tabs", h.row(9))
	}
}

// In a narrow frame the tabs scroll in the border's room, the active one in view, a border cell
// before the corner.
func TestNarrowTabsKeepToTheirRoom(t *testing.T) {
	var many []widget.StripTab
	for _, n := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		many = append(many, widget.StripTab{ID: n, Label: n + ".md"})
	}
	h, _, _, _ := tabsEditor(t, 40, "notes.csv", "", true, many...)
	defer h.stop()
	bottom := h.row(9)
	if !strings.HasPrefix(bottom, "└─") || !strings.HasSuffix(bottom, "─┘") || col(h, 9, "one.md") < 2 {
		t.Fatalf("the bottom border %q; want the strip between └─ and ─┘, the active tab (one.md) shown", bottom)
	}
	if strings.Contains(bottom, "seven") {
		t.Errorf("the bottom border %q: all seven tabs drawn in 40 columns; want them scrolled", bottom)
	}
}

// A status keeps its words at the border's right; the tabs keep to the room before it.
func TestTheStatusKeepsItsPlaceBesideTheTabs(t *testing.T) {
	h, _, _, _ := tabsEditor(t, 60, "notes.csv", "ok", true, twoTabs...)
	defer h.stop()
	bottom := h.row(9)
	if b := col(h, 9, "beta"); !strings.HasSuffix(bottom, " ok ┘") || b < 0 || b > col(h, 9, " ok ") {
		t.Fatalf("the bottom border %q; want the tabs, then the status at the right", bottom)
	}
}

// A press on a tab reaches the strip with the focus elsewhere, and leaves the focus where it was.
func TestABorderTabTakesAPressWithTheFocusElsewhere(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("x\n")))
	ed.SetTabsInFrame(true)
	var chose []string
	ed.Tabs().SetOnSelect(func(id string) { chose = append(chose, id) })
	ed.Tabs().SetTabs(twoTabs)
	other := widget.NewTextInput()
	box := widget.NewBox(ed, widget.WithTitle("t"), widget.WithBottomSlot(ed.FrameTabs()))
	sh := newShell(widget.NewSplit(widget.Horizontal, other, box))
	h := startApp(t, sh, 90, 8)
	defer h.stop()
	h.onLoop(func() { h.app.FocusInto(other) })
	h.barrier(sh)
	b := col(h, 7, "beta")
	if b < 0 {
		t.Fatalf("no tabs on the bottom border:\n%s", h.grid())
	}
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: b + 1, Y: 7})
	h.barrier(sh)
	if len(chose) != 1 || chose[0] != "2" {
		t.Fatalf("a press on beta with the focus in another widget chose %v; want [2]", chose)
	}
	var stays bool
	h.onLoop(func() { stays = h.app.FocusWithin(other) })
	if !stays {
		t.Error("the press on a border tab moved the focus")
	}
}

// The bottom slot is the box's child: listed after the title slot, and removed by Remove.
func TestTheBottomSlotIsABoxChild(t *testing.T) {
	ed := widget.NewEditor()
	box := widget.NewBox(ed, widget.WithTitleSlot(ed.TitleSwitch()), widget.WithBottomSlot(ed.FrameTabs()))
	var kids []tui.Component
	for c := range box.Children() {
		kids = append(kids, c)
	}
	if len(kids) != 3 || kids[2] != ed.FrameTabs() || box.BottomSlot() != ed.FrameTabs() {
		t.Fatalf("children %v; want the editor, the switch, then the tabs", kids)
	}
	box.Remove(ed.FrameTabs())
	if box.BottomSlot() != nil {
		t.Error("Remove left the bottom slot")
	}
	t1, t2 := ed.FrameTabs(), ed.FrameTabs()
	s1, s2 := ed.Tabs(), ed.Tabs()
	if t1 != t2 || s1 != s2 {
		t.Error("FrameTabs or Tabs is a new component each call")
	}
}
