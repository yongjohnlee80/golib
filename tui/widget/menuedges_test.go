package widget_test

// THE EDGES OF THE VOCABULARY ADDED FOR THE MENU BAR.
//
// A vertical menu, the overrides, and the cases a bar reaches only when its
// model is shaped awkwardly. These are the paths the bar tests never touch,
// which is exactly why they are worth their own file: a feature exercised only
// through the one configuration its author had in mind is a feature with one
// configuration.

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// columnFixture is a VERTICAL menu — no bar. Its arrow vocabulary is the other
// one: Up and Down move, Right descends into a submenu, Left comes back.
func columnFixture(t *testing.T, m *widget.Menu, items []widget.MenuItemModel) *harness {
	t.Helper()
	host := widget.NewOverlayHost(m)
	h := startApp(t, host, 50, 14)
	h.onLoop(func() {
		if err := m.SetModel(items); err != nil {
			t.Fatalf("SetModel: %v", err)
		}
		m.Context().RequestFocus()
	})
	h.settle()
	return h
}

func columnModel() []widget.MenuItemModel {
	return []widget.MenuItemModel{
		widget.NewCommand("one", "One", nil),
		widget.NewSubmenu("deep", "Deep", []widget.MenuItemModel{
			widget.NewCommand("leaf", "Leaf", nil),
		}),
		widget.NewCommand("three", "Three", nil),
	}
}

// TestAVerticalMenuKeepsItsOwnArrowVocabulary.
//
// A column has ONE axis of travel and cascades sideways, so Right descends and
// Left returns — the opposite assignment from a bar, where the horizontal pair
// walks the bar and Up leaves the level. Splitting the two resolvers is what
// allows both; this is the half the bar tests never reach.
func TestAVerticalMenuKeepsItsOwnArrowVocabulary(t *testing.T) {
	press := func(h *harness, code rune) {
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: code})
		h.settle()
		h.settle()
	}
	selOf := func(t *testing.T, h *harness, m *widget.Menu) widget.ItemID {
		t.Helper()
		var id widget.ItemID
		h.onLoop(func() { id, _ = m.Selected() })
		return id
	}

	t.Run("Down and Up move the selection", func(t *testing.T) {
		m := widget.NewMenu()
		h := columnFixture(t, m, columnModel())
		defer h.stop()
		h.onLoop(func() { m.Select("one") })
		h.settle()
		press(h, tui.KeyDown)
		if got := selOf(t, h, m); got != "deep" {
			t.Errorf("Down selected %q, want deep", got)
		}
		press(h, tui.KeyUp)
		if got := selOf(t, h, m); got != "one" {
			t.Errorf("Up selected %q, want one", got)
		}
	})

	t.Run("Right descends and Left returns", func(t *testing.T) {
		m := widget.NewMenu()
		h := columnFixture(t, m, columnModel())
		defer h.stop()
		h.onLoop(func() { m.Select("deep") })
		h.settle()

		press(h, tui.KeyRight)
		if n := openLevelsOn(t, h, m); n != 1 {
			t.Fatalf("Right on a submenu row opened %d levels, want 1", n)
		}
		press(h, tui.KeyLeft)
		if n := openLevelsOn(t, h, m); n != 0 {
			t.Errorf("Left left %d levels open, want 0", n)
		}
	})

	t.Run("Right on a plain row does nothing", func(t *testing.T) {
		// The control for the case above: a column has no bar to walk to, so
		// the key that descends must NOT quietly do something else instead.
		m := widget.NewMenu()
		h := columnFixture(t, m, columnModel())
		defer h.stop()
		h.onLoop(func() { m.Select("one") })
		h.settle()
		press(h, tui.KeyRight)
		if n := openLevelsOn(t, h, m); n != 0 {
			t.Errorf("Right on a command row opened %d levels", n)
		}
		if got := selOf(t, h, m); got != "one" {
			t.Errorf("Right on a command row moved the selection to %q", got)
		}
	})

	t.Run("Left with nothing open does nothing", func(t *testing.T) {
		m := widget.NewMenu()
		h := columnFixture(t, m, columnModel())
		defer h.stop()
		h.onLoop(func() { m.Select("one") })
		h.settle()
		press(h, tui.KeyLeft)
		if got := selOf(t, h, m); got != "one" {
			t.Errorf("Left at the root moved the selection to %q", got)
		}
	})
}

// TestTheTopOfALevelSkipsRowsThatCannotBeSelected.
//
// Up closes a level at its FIRST SELECTABLE row, which is not always its first
// row: a level that opens with a separator would otherwise need one extra Up
// that appears to do nothing.
func TestTheTopOfALevelSkipsRowsThatCannotBeSelected(t *testing.T) {
	model := []widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewSeparator("sep"),
			widget.NewCommand("new", "New", nil),
			widget.NewCommand("open", "Open", nil),
		}),
	}
	m := widget.NewMenu()
	h, _ := barFixture(t, m, model, 50, 12)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()
	h.onLoop(func() { _ = m.Open("file") })
	h.settle()
	h.settle()

	var sel widget.ItemID
	h.onLoop(func() { sel, _ = m.Selected() })
	if sel != "new" {
		t.Fatalf("the level opened on %q; a separator is not selectable", sel)
	}
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyUp})
	h.settle()
	h.settle()
	if n := openLevelsOn(t, h, m); n != 0 {
		t.Errorf("Up on the first SELECTABLE row left %d levels open; the separator "+
			"above it is not a row the user can stand on", n)
	}
}

// TestABarWithOneCategoryHasNowhereToStep.
//
// barNeighbour has to report "nowhere" rather than handing back the category
// already open, which would reopen it and close its own cascade on every press.
func TestABarWithOneCategoryHasNowhereToStep(t *testing.T) {
	model := []widget.MenuItemModel{
		widget.NewSubmenu("only", "Only", []widget.MenuItemModel{
			widget.NewCommand("a", "A", nil),
			widget.NewCommand("b", "B", nil),
		}),
	}
	m := widget.NewMenu()
	h, _ := barFixture(t, m, model, 40, 12)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()
	h.onLoop(func() { _ = m.Open("only") })
	h.settle()
	h.settle()
	h.onLoop(func() { m.Select("b") })
	h.settle()

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight})
	h.settle()
	h.settle()
	if n := openLevelsOn(t, h, m); n != 1 {
		t.Errorf("Right in a one-category bar left %d levels open, want the cascade "+
			"untouched", n)
	}
	var sel widget.ItemID
	h.onLoop(func() { sel, _ = m.Selected() })
	if sel != "b" {
		t.Errorf("Right moved the selection to %q; there is nowhere to step", sel)
	}
}

// TestALevelTooNarrowForItsNameDrawsNoTitle.
//
// A title clipped to half a word on a frame is harder to read than no title, so
// the frame goes bare rather than carrying "Prefere…".
func TestALevelTooNarrowForItsNameDrawsNoTitle(t *testing.T) {
	model := []widget.MenuItemModel{
		widget.NewSubmenu("long", "AVeryLongCategoryName", []widget.MenuItemModel{
			widget.NewCommand("a", "A", nil),
		}),
	}
	m := widget.NewMenu()
	// Narrow enough that the level cannot hold its own name.
	h, _ := barFixture(t, m, model, 14, 10)
	defer h.stop()
	h.onLoop(func() { _ = m.Open("long") })
	h.settle()
	h.settle()

	grid := h.grid()
	if row := rowContaining(grid, "┌"); row >= 0 {
		line := strings.Split(grid, "\n")[row]
		if strings.Contains(line, "AVeryLong") {
			t.Errorf("a title too wide for its frame was drawn anyway: %q", line)
		}
	}
}

// TestSelectedBlurredIsOverridableAndNilSafe.
//
// The override exists for a design that wants a faint marker where the
// selection will return to, rather than nothing at all — and every accessor in
// this package answers on a nil style, because a menu with no style asks one
// for values on every paint.
func TestSelectedBlurredIsOverridableAndNilSafe(t *testing.T) {
	var nilStyle *widget.MenuStyle
	if got := nilStyle.SelectedBlurred(); got != widget.DefaultMenuStyle().SelectedBlurred() {
		t.Error("a nil MenuStyle did not answer with the default blurred look")
	}

	marker := style.New().Underline(true)
	s := widget.DefaultMenuStyle().WithSelectedBlurred(marker)
	if s.SelectedBlurred() != marker {
		t.Errorf("WithSelectedBlurred stored %+v, want the supplied look", s.SelectedBlurred())
	}
	// A copy, never a mutation: these values are shared between menus.
	if widget.DefaultMenuStyle().SelectedBlurred() == marker {
		t.Error("WithSelectedBlurred mutated the value it was called on")
	}
	// And it leaves the other looks alone.
	if s.Selected() != widget.DefaultMenuStyle().Selected() {
		t.Error("WithSelectedBlurred disturbed the focused selection look")
	}
}

// TestButtonAlignNamesItself, because a failure reading "align 2" is one the
// reader has to go and decode.
func TestButtonAlignNamesItself(t *testing.T) {
	for _, tc := range []struct {
		a    widget.ButtonAlign
		want string
	}{
		{widget.ButtonsCenter, "center"},
		{widget.ButtonsLeft, "left"},
		{widget.ButtonsRight, "right"},
		{widget.ButtonsRight + 1, "unknown"},
	} {
		if got := tc.a.String(); got != tc.want {
			t.Errorf("ButtonAlign(%d).String() = %q, want %q", tc.a, got, tc.want)
		}
	}
	if widget.ButtonAlign(widget.ButtonsRight + 1).Valid() {
		t.Error("an out-of-range ButtonAlign reported itself valid")
	}
}
