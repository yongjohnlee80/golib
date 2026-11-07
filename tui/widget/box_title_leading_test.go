package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// tabsEditor is an Editor in a Box titled title, w×10, its switch on the box's title row and its
// tabs (alpha, beta, alpha active) at the row's left, while tabsOn; chose records each tab chosen.
func tabsEditor(t *testing.T, w int, title string, align style.Align, tabsOn bool, tabs ...widget.StripTab) (*harness, *shell, *widget.Editor, *[]string) {
	t.Helper()
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("name,n\nann,1\n")), widget.EditorModeSwitch(true))
	ed.SetModeSwitchInTitle(true)
	ed.SetDocumentView(widget.NewTableDocument(ed.Core(), ','))
	ed.SetRenderedDocument(true)
	ed.SetTabsInTitle(tabsOn)
	var chose []string
	strip := ed.Tabs()
	strip.SetOnSelect(func(id string) { chose = append(chose, id); strip.SetActive(id) })
	strip.SetTabs(tabs)
	if len(tabs) > 0 {
		strip.SetActive(tabs[0].ID)
	}
	box := widget.NewBox(ed, widget.WithTitle(title), widget.WithTitleAlign(align),
		widget.WithTitleSlot(ed.TitleSwitch()), widget.WithTitleLeading(ed.TitleTabs()))
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

// The tabs lead the frame's title row: a border cell after the corner, the tabs, the title after
// them, and the switch at the row's right as before. The editor gives up no row: its text starts on
// its own first row. A press on a tab chooses it.
func TestTheTabsLeadTheFramesTitleRow(t *testing.T) {
	h, sh, _, chose := tabsEditor(t, 60, "notes.csv", style.AlignLeft, true, twoTabs...)
	defer h.stop()
	top := h.row(0)
	a, b, title, sw := col(h, 0, "alpha"), col(h, 0, "beta"), col(h, 0, "notes.csv"), switchCol(h, 0)
	if !strings.HasPrefix(top, "┌─") || a < 2 || !(a < b && b < title && title < sw) {
		t.Fatalf("the title row %q; want ┌─, alpha, beta, the title, then the switch, in that order", top)
	}
	if want := 60 - 2 - widget.ModeSwitchWidth; sw != want {
		t.Errorf("the switch at column %d; want %d, where it is without tabs", sw, want)
	}
	if !strings.HasPrefix(h.row(1), "│name,n") {
		t.Fatalf("row 1 %q; want the text's first line: the tabs take no row", h.row(1))
	}
	h.inject(pressAt(b+1, 0))
	h.barrier(sh)
	if len(*chose) != 1 || (*chose)[0] != "2" {
		t.Fatalf("a press on beta chose %v; want [2]", *chose)
	}
}

// Tabs off, or a strip with no tabs, take no room: the title row is drawn as a box without them.
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
			h, _, _, _ := tabsEditor(t, 50, "notes.csv", style.AlignLeft, c.on, c.tabs...)
			defer h.stop()
			if top := h.row(0); !strings.HasPrefix(top, "┌ notes.csv ") || strings.Contains(top, "alpha") {
				t.Fatalf("the title row %q; want the title right after the corner, no tabs", top)
			}
		})
	}
}

// Turned on after the first layout, the tabs take their place; turned off, they give it back.
func TestTabsInTitleTurnsLive(t *testing.T) {
	h, sh, ed, _ := tabsEditor(t, 60, "notes.csv", style.AlignLeft, false, twoTabs...)
	defer h.stop()
	h.onLoop(func() { ed.SetTabsInTitle(true) })
	h.barrier(sh)
	if a, title := col(h, 0, "alpha"), col(h, 0, "notes.csv"); a < 0 || a > title {
		t.Fatalf("turned on, the title row %q; want alpha before the title", h.row(0))
	}
	h.onLoop(func() { ed.SetTabsInTitle(false) })
	h.barrier(sh)
	if top := h.row(0); !strings.HasPrefix(top, "┌ notes.csv ") {
		t.Fatalf("turned off, the title row %q; want the title right after the corner", top)
	}
}

// In a narrow frame the tabs keep to the room before the switch: they scroll in it, the switch is
// whole at the row's right, and a border cell stands between them; the title has what is left, if
// anything, and never runs over either.
func TestNarrowTabsKeepToTheirRoom(t *testing.T) {
	var many []widget.StripTab
	for _, n := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		many = append(many, widget.StripTab{ID: n, Label: n + ".md"})
	}
	h, _, _, _ := tabsEditor(t, 40, "notes.csv", style.AlignLeft, true, many...)
	defer h.stop()
	top := h.row(0)
	sw := switchCol(h, 0)
	if want := 40 - 2 - widget.ModeSwitchWidth; sw != want {
		t.Fatalf("the title row %q: the switch at %d; want %d, whole", top, sw, want)
	}
	if !strings.HasPrefix(top, "┌─") || col(h, 0, "one.md") < 2 || col(h, 0, "one.md") > sw {
		t.Fatalf("the title row %q; want the strip after the corner's border cell, the active tab (one.md) shown", top)
	}
	cells := []rune(top)
	if cells[sw-1] != '─' {
		t.Errorf("the title row %q: %q before the switch; want a border cell", top, cells[sw-1])
	}
	if strings.Contains(top, "seven") {
		t.Errorf("the title row %q: all seven tabs drawn in 40 columns; want them scrolled", top)
	}
}

// A right-aligned title puts the switch at the row's left; the tabs follow it, and the title keeps
// to the right.
func TestTabsFollowTheSwitchUnderARightAlignedTitle(t *testing.T) {
	h, _, _, _ := tabsEditor(t, 60, "notes.csv", style.AlignRight, true, twoTabs...)
	defer h.stop()
	top := h.row(0)
	sw, a, title := switchCol(h, 0), col(h, 0, "alpha"), col(h, 0, "notes.csv")
	if sw != 2 || !(sw+widget.ModeSwitchWidth < a && a < title) || !strings.HasSuffix(top, " notes.csv ┐") {
		t.Fatalf("the title row %q; want the switch at 2, then the tabs, the title at the right", top)
	}
}

// A press on a tab reaches the strip with the focus elsewhere, and leaves the focus where it was.
func TestATitleRowTabTakesAPressWithTheFocusElsewhere(t *testing.T) {
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("x\n")))
	ed.SetTabsInTitle(true)
	var chose []string
	ed.Tabs().SetOnSelect(func(id string) { chose = append(chose, id) })
	ed.Tabs().SetTabs(twoTabs)
	other := widget.NewTextInput()
	box := widget.NewBox(ed, widget.WithTitle("t"), widget.WithTitleLeading(ed.TitleTabs()))
	sh := newShell(widget.NewSplit(widget.Horizontal, other, box))
	h := startApp(t, sh, 90, 8)
	defer h.stop()
	h.onLoop(func() { h.app.FocusInto(other) })
	h.barrier(sh)
	b := col(h, 0, "beta")
	if b < 0 {
		t.Fatalf("no tabs on the title row:\n%s", h.grid())
	}
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: b + 1, Y: 0})
	h.barrier(sh)
	if len(chose) != 1 || chose[0] != "2" {
		t.Fatalf("a press on beta with the focus in another widget chose %v; want [2]", chose)
	}
	var stays bool
	h.onLoop(func() { stays = h.app.FocusWithin(other) })
	if !stays {
		t.Error("the press on a title-row tab moved the focus")
	}
}

// The leading control is the box's child: listed after the title slot, and removed by Remove.
func TestTheLeadingControlIsABoxChild(t *testing.T) {
	ed := widget.NewEditor()
	box := widget.NewBox(ed, widget.WithTitleSlot(ed.TitleSwitch()), widget.WithTitleLeading(ed.TitleTabs()))
	var kids []tui.Component
	for c := range box.Children() {
		kids = append(kids, c)
	}
	if len(kids) != 3 || kids[2] != ed.TitleTabs() || box.TitleLeading() != ed.TitleTabs() {
		t.Fatalf("children %v; want the editor, the switch, then the tabs", kids)
	}
	box.Remove(ed.TitleTabs())
	if box.TitleLeading() != nil {
		t.Error("Remove left the leading control")
	}
	t1, t2 := ed.TitleTabs(), ed.TitleTabs()
	s1, s2 := ed.Tabs(), ed.Tabs()
	if t1 != t2 || s1 != s2 {
		t.Error("TitleTabs or Tabs is a new component each call")
	}
}
