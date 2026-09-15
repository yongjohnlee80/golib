package widget_test

// The Menu family's PUBLISHED SURFACE: the options, the accessors, the style
// value and the render paths. Individually small, and untested collectively they
// are the part of a widget a consumer meets first — an option that silently does
// nothing, or a style method that mutates the value two menus are sharing, is a
// defect no behavioural test of the widget's logic will ever reach.

import (
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TestMenuStyleIsNilSafeAndImmutable — the same contract as ButtonStyle and
// ModalStyle, because a consumer who has learned one has learned them all.
func TestMenuStyleIsNilSafeAndImmutable(t *testing.T) {
	var s *widget.MenuStyle
	def := widget.DefaultMenuStyle()
	if s.Surface() != def.Surface() || s.Selected() != def.Selected() ||
		s.Armed() != def.Armed() || s.Disabled() != def.Disabled() ||
		s.Accel() != def.Accel() || s.Border() != def.Border() {
		t.Error("a nil MenuStyle does not fall back to the defaults")
	}

	base := widget.NewMenuStyle(styleOf(1), styleOf(2))
	for name, tc := range map[string]struct {
		derive func(*widget.MenuStyle) *widget.MenuStyle
		read   func(*widget.MenuStyle) style.Style
	}{
		"WithSurface":  {func(x *widget.MenuStyle) *widget.MenuStyle { return x.WithSurface(styleOf(6)) }, (*widget.MenuStyle).Surface},
		"WithSelected": {func(x *widget.MenuStyle) *widget.MenuStyle { return x.WithSelected(styleOf(6)) }, (*widget.MenuStyle).Selected},
		"WithArmed":    {func(x *widget.MenuStyle) *widget.MenuStyle { return x.WithArmed(styleOf(6)) }, (*widget.MenuStyle).Armed},
		"WithDisabled": {func(x *widget.MenuStyle) *widget.MenuStyle { return x.WithDisabled(styleOf(6)) }, (*widget.MenuStyle).Disabled},
		"WithAccel":    {func(x *widget.MenuStyle) *widget.MenuStyle { return x.WithAccel(styleOf(6)) }, (*widget.MenuStyle).Accel},
		"WithBorder":   {func(x *widget.MenuStyle) *widget.MenuStyle { return x.WithBorder(styleOf(6)) }, (*widget.MenuStyle).Border},
	} {
		was := tc.read(base)
		got := tc.derive(base)
		if tc.read(got) != styleOf(6) {
			t.Errorf("%s did not change the copy", name)
		}
		if tc.read(base) != was {
			t.Errorf("%s mutated the receiver; styles are shared and must be copied", name)
		}
		if got.Surface() != base.Surface() && name != "WithSurface" {
			t.Errorf("%s also changed an unrelated look", name)
		}
	}

	// NewMenuStyle states the two looks a caller has an opinion about and
	// derives the rest, so a partial statement still yields a complete style.
	d := widget.NewMenuStyle(styleOf(1), styleOf(2))
	if d.Border() != d.Surface() {
		t.Error("NewMenuStyle did not derive the border from the surface")
	}
	if d.Armed() == d.Selected() {
		t.Error("NewMenuStyle did not derive a distinct armed look")
	}
	if d.Disabled() == d.Surface() {
		t.Error("NewMenuStyle did not derive a distinct disabled look")
	}
}

// TestADisabledRowPaintsAsDisabled.
//
// Asserted where a user sees it — in the painted cells — rather than through a
// selector function, because the precedence is the package's own presentation
// choice and not a contract a consumer's RowRenderer has to obey.
//
// It deliberately does NOT assert disabled-over-selected: repairSelection moves
// the selection off a row that stops being selectable, so that combination is
// one the Menu cannot produce, and a test asserting it would be measuring an
// arrangement no user can reach.
func TestADisabledRowPaintsAsDisabled(t *testing.T) {
	st := widget.NewMenuStyle(styleOf(1), styleOf(2))
	m := widget.NewMenu(widget.WithMenuStyle(st))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("first", "First", nil),
		widget.NewCommand("second", "Second", nil),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 20, 6)
	defer h.stop()
	h.settle()

	// Row 1 is not the selection (row 0 is), so it carries the ordinary look.
	normal := cellAttrs(h, 1, 1)

	h.onLoop(func() { m.SetEnabled("second", false) })
	h.settle()
	disabled := cellAttrs(h, 1, 1)

	if normal == disabled {
		t.Error("a disabled row paints exactly like an enabled one; the disabled " +
			"look is the only thing telling a user the row will not respond")
	}
}

// TestTheEnumsNameEveryValue — these strings appear in traces and failures, so
// a kind that renders as a number is a message nobody can read.
func TestTheEnumsNameEveryValue(t *testing.T) {
	for k, want := range map[widget.ItemKind]string{
		widget.ItemKindCommand:   "command",
		widget.ItemKindSubmenu:   "submenu",
		widget.ItemKindSeparator: "separator",
		widget.ItemKindCheck:     "check",
		widget.ItemKindRadio:     "radio",
	} {
		if got := k.String(); got != want {
			t.Errorf("ItemKind(%d).String() = %q, want %q", k, got, want)
		}
	}
	// The FIRST invalid value, not a far one: an off-by-one bound rejects 200 as
	// readily as a correct bound does.
	if !widget.ItemKindRadio.Valid() || (widget.ItemKindRadio + 1).Valid() {
		t.Error("ItemKind.Valid does not bound the declared set at its edge")
	}
	if got := (widget.ItemKindRadio + 1).String(); got != "unknown" {
		t.Errorf("an undeclared kind rendered as %q", got)
	}

	// RowState is four INDEPENDENT flags, so its rendering has to show
	// combinations rather than pick the winner an enum would have to.
	for _, tc := range []struct {
		st   widget.RowState
		want string
	}{
		{widget.RowState{}, "normal"},
		{widget.RowState{Selected: true}, "selected"},
		{widget.RowState{Armed: true}, "armed"},
		{widget.RowState{Selected: true, Open: true}, "selected+open"},
		{widget.RowState{Selected: true, Focused: true, Open: true}, "selected+focused+open"},
	} {
		if got := tc.st.String(); got != tc.want {
			t.Errorf("RowState%+v.String() = %q, want %q", tc.st, got, tc.want)
		}
	}

	for p, want := range map[widget.BarPlacement]string{
		widget.BarPlacementTop:    "top",
		widget.BarPlacementBottom: "bottom",
		widget.BarPlacementLeft:   "left",
		widget.BarPlacementRight:  "right",
	} {
		if got := p.String(); got != want {
			t.Errorf("BarPlacement(%d).String() = %q, want %q", p, got, want)
		}
	}
	if got := (widget.BarPlacementRight + 1).String(); got != "unknown" {
		t.Errorf("an undeclared placement rendered as %q", got)
	}
}

// swatchRenderer is a consumer row painter: the declared extension seam for a
// row kind the closed ItemKind does not provide.
type swatchRenderer struct{ seen []widget.RowView }

func (r *swatchRenderer) RenderRow(s tui.Surface, row widget.RowView, st widget.RowState) {
	r.seen = append(r.seen, row)
	for i, cluster := range []string{"[", "#", "]"} {
		s.SetCell(i, 0, cluster, style.New())
	}
}

// TestARowRendererPaintsTheWholeRowAndSeesNoChildren.
//
// Delegation is TOTAL: a hook that painted only part of a row would have to
// agree with the built-in painter about where the parts are, and the two would
// drift. And RowView omits Children deliberately — passing the model value would
// hand consumer code a slice header aliasing the Menu's own storage.
func TestARowRendererPaintsTheWholeRowAndSeesNoChildren(t *testing.T) {
	rr := &swatchRenderer{}
	m := widget.NewMenu(widget.WithRowRenderer(rr))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("colours", "Colours", []widget.MenuItemModel{
			widget.NewCommand("red", "Red", nil),
		}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()

	if got := h.grid(); !strings.Contains(got, "[#]") {
		t.Errorf("the consumer renderer did not paint the row:\n%s", got)
	}
	var seen []widget.RowView
	h.onLoop(func() { seen = append(seen, rr.seen...) })
	if len(seen) == 0 {
		t.Fatal("the renderer was never called")
	}
	if seen[0].ID != "colours" {
		t.Errorf("the view names %q, want colours", seen[0].ID)
	}
	if !seen[0].HasChildren {
		t.Error("HasChildren is false for a submenu row")
	}
	if seen[0].Kind != widget.ItemKindSubmenu {
		t.Errorf("the view's kind is %v, want submenu", seen[0].Kind)
	}
}

// TestSelectionChangesReachBothTheCallbackAndTheBus.
//
// Two audiences, and they need different things: a controller constructed
// alongside the menu takes the callback, while an observer that would rather not
// be coupled to construction takes the event.
func TestSelectionChangesReachBothTheCallbackAndTheBus(t *testing.T) {
	var called []widget.ItemID
	m := widget.NewMenu(widget.WithOnSelectionChanged(func(id widget.ItemID) {
		called = append(called, id)
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("one", "One", nil),
		widget.NewCommand("two", "Two", nil),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()

	var events []widget.MenuSelectionChangedEvent
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.MenuSelectionChangedEvent) {
		events = append(events, ev)
	})
	defer unsub()

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown})
	h.waitFor("the change was published", func() bool {
		var n int
		h.onLoop(func() { n = len(events) })
		return n == 1
	})
	h.settle()

	h.onLoop(func() {
		if len(events) != 1 || events[0].ItemID != "two" {
			t.Errorf("events = %v, want one naming two", events)
		}
		// The callback also saw the initial selection at mount, so the LAST
		// entry is the move under test.
		if len(called) == 0 || called[len(called)-1] != "two" {
			t.Errorf("callback saw %v, want it to end at two", called)
		}
	})
}

// TestAMenuRestylesItsOpenLevelsToo.
//
// A theme swap that reached only the root would leave a cascade half-dressed,
// which is the state a user notices immediately and a screenshot test of the
// root would not.
func TestAMenuRestylesItsOpenLevelsToo(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", nil),
		}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 40, 14)
	defer h.stop()
	h.onLoop(func() {
		if err := m.Open("file"); err != nil {
			t.Errorf("Open: %v", err)
		}
	})
	h.settle()
	h.settle()

	nx, ny := cellOfLabel(t, h, "New")
	before := rowStyleAt(t, h, nx, ny)

	underlined := widget.NewMenuStyle(
		style.New().Underline(true), style.New().Underline(true))
	h.onLoop(func() { m.WithStyle(underlined) })
	h.settle()

	if rowStyleAt(t, h, nx, ny) == before {
		t.Error("the open level kept its old style; a restyle must reach the whole cascade")
	}
}

// TestTheMenuPointerPolicyBoundIsProbedAtItsEdge.
func TestTheMenuPointerPolicyBoundIsProbedAtItsEdge(t *testing.T) {
	m := widget.NewMenu()
	if f := fatalFromWidgetExt(func() { m.WithPointerPolicy(tui.PointerDisabled) }); f != nil {
		t.Errorf("the last valid policy was rejected: %v", f.Rule)
	}
	if f := fatalFromWidgetExt(func() { m.WithPointerPolicy(tui.PointerDisabled + 1) }); f == nil {
		t.Error("Menu.WithPointerPolicy accepted the first value past the declared set")
	}
	item := widget.NewMenuItem("X")
	if f := fatalFromWidgetExt(func() { item.WithPointerPolicy(tui.PointerDisabled + 1) }); f == nil {
		t.Error("MenuItem.WithPointerPolicy accepted the first value past the declared set")
	}
	bar := widget.NewMenuBar(widget.NewMenu())
	if f := fatalFromWidgetExt(func() { bar.WithPointerPolicy(tui.PointerDisabled + 1) }); f == nil {
		t.Error("MenuBar.WithPointerPolicy accepted the first value past the declared set")
	}
}

// TestAStyledMenuReachesItsRowsAndItsAccessors.
//
// WithMenuStyle is the construction option; the accessors are what a caller
// reads back. An option that stored the value somewhere nothing painted from
// would look configured and render default.
func TestAStyledMenuReachesItsRowsAndItsAccessors(t *testing.T) {
	underlined := widget.NewMenuStyle(
		style.New().Underline(true), style.New().Underline(true))
	m := widget.NewMenu(widget.WithMenuStyle(underlined))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("one", "One", nil),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()

	x, y := cellOfLabel(t, h, "One")
	if rowStyleAt(t, h, x, y).Mask&tui.AttrUnderline == 0 {
		t.Error("the construction style never reached the painted row")
	}
}

// ─── MenuItem's surface ──────────────────────────────────────────────────────

// TestAMenuItemReportsWhatItWasBuiltWith.
func TestAMenuItemReportsWhatItWasBuiltWith(t *testing.T) {
	item := widget.NewMenuItem("Rename",
		widget.WithItemKind(widget.ItemKindCheck),
		widget.WithItemAccel("F2"),
		widget.WithItemChecked(true),
		widget.WithItemStyle(widget.NewMenuStyle(styleOf(1), styleOf(2))))

	if item.Label() != "Rename" {
		t.Errorf("Label() = %q", item.Label())
	}
	if item.Kind() != widget.ItemKindCheck {
		t.Errorf("Kind() = %v, want check", item.Kind())
	}
	if !item.Checked() {
		t.Error("Checked() is false for an item built checked")
	}
	if !item.Enabled() {
		t.Error("Enabled() is false for an item built with the default")
	}
	if item.Armed() {
		t.Error("Armed() is true before any press")
	}

	item.SetChecked(false)
	if item.Checked() {
		t.Error("SetChecked(false) did not take")
	}
	item.SetChecked(false) // idempotent
	if item.Checked() {
		t.Error("a repeated SetChecked changed the state")
	}
}

// TestAMenuItemPaintsItsMarkLabelAndAccelerator.
//
// The three pieces have to coexist in one line: a check mark at the left, the
// label after it, and the accelerator right-aligned. Getting the widths wrong
// shows up as an accelerator that overwrites the label.
func TestAMenuItemPaintsItsMarkLabelAndAccelerator(t *testing.T) {
	item := widget.NewMenuItem("Wrap lines",
		widget.WithItemKind(widget.ItemKindCheck),
		widget.WithItemChecked(true),
		widget.WithItemAccel("Ctrl+W"))
	host := widget.NewOverlayHost(item)
	h := startApp(t, host, 40, 4)
	defer h.stop()
	h.settle()

	got := h.grid()
	for _, want := range []string{"✓", "Wrap lines", "Ctrl+W"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is not painted:\n%s", want, got)
		}
	}
	// The accelerator sits to the RIGHT of the label rather than over it.
	lx, _ := cellOfLabel(t, h, "Wrap lines")
	ax, _ := cellOfLabel(t, h, "Ctrl+W")
	if ax <= lx+len("Wrap lines") {
		t.Errorf("the accelerator at column %d overlaps the label starting at %d", ax, lx)
	}

	// Unchecking clears the mark rather than leaving it painted.
	h.onLoop(func() { item.SetChecked(false) })
	h.settle()
	if strings.Contains(h.grid(), "✓") {
		t.Error("the check mark survived being unchecked")
	}
}

// TestAMenuItemRestylesAtRuntime.
func TestAMenuItemRestylesAtRuntime(t *testing.T) {
	item := widget.NewMenuItem("Plain")
	host := widget.NewOverlayHost(item)
	h := startApp(t, host, 30, 4)
	defer h.stop()
	h.settle()

	x, y := cellOfLabel(t, h, "Plain")
	before := rowStyleAt(t, h, x, y)
	h.onLoop(func() {
		item.WithStyle(widget.NewMenuStyle(
			style.New().Underline(true), style.New().Underline(true)))
	})
	h.settle()
	if rowStyleAt(t, h, x, y) == before {
		t.Error("WithStyle did not repaint the item")
	}
}

// ─── MenuBar's surface ───────────────────────────────────────────────────────

// TestABarReportsItsPlacementAndIsNotATabStop.
//
// The bar is not focusable: the Menu inside it is, and a focusable shell would
// insert a Tab stop that does nothing between the application and its menu.
func TestABarReportsItsPlacementAndIsNotATabStop(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("file", "File", nil),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	bar := widget.NewMenuBar(m, widget.WithBarPlacement(widget.BarPlacementRight))
	if bar.Placement() != widget.BarPlacementRight {
		t.Errorf("Placement() = %v, want right", bar.Placement())
	}

	other := widget.NewButton("Other")
	root := tui.NewFlex(tui.Vertical)
	root.Add(bar, other)
	host := widget.NewOverlayHost(root)
	h := startApp(t, host, 40, 10)
	defer h.stop()
	h.settle()

	// Tab twice: the stops are the Menu and the Button, never the bar.
	seen := map[bool]int{}
	for range 2 {
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
		h.settle()
		var onMenu bool
		h.onLoop(func() { onMenu = m.Context().Focused() })
		seen[onMenu]++
	}
	if seen[true] == 0 {
		t.Error("Tab never reached the menu inside the bar")
	}
}

// TestAVerticalBarLaysItsRowsDownAColumn.
func TestAVerticalBarLaysItsRowsDownAColumn(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("file", "File", nil),
		widget.NewCommand("edit", "Edit", nil),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	bar := widget.NewMenuBar(m, widget.WithBarPlacement(widget.BarPlacementLeft))
	host := widget.NewOverlayHost(bar)
	h := startApp(t, host, 40, 10)
	defer h.stop()
	h.settle()

	_, fy := cellOfLabel(t, h, "File")
	_, ey := cellOfLabel(t, h, "Edit")
	if fy == ey {
		t.Errorf("both rows are on line %d; a vertical bar stacks them", fy)
	}
	// And the arrows stay vertical for a vertical bar.
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown})
	h.settle()
	if id := selectedOn(t, h, m); id != "edit" {
		t.Errorf("Down selected %q, want edit: a vertical bar steps vertically", id)
	}
}

// TestAMnemonicActivatesItsRowFromAnywhereInTheLevel.
//
// A mnemonic is the keyboard's shortcut past traversal: it acts on its row
// wherever the selection happens to be. Case-insensitively, because a user
// pressing Shift for no reason should still get their command, and scoped to the
// level ON SCREEN, because a mnemonic for a row the user cannot see is a
// keystroke with an invisible effect.
func TestAMnemonicActivatesItsRowFromAnywhereInTheLevel(t *testing.T) {
	var ran []widget.ItemID
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool { return true }))
	quit := widget.NewCommand("quit", "Quit", saveAction{})
	quit.Hotkey = 'Q'
	quit.HotkeyIdx = 0
	hidden := widget.NewCommand("hidden", "Zap", saveAction{})
	hidden.Hotkey = 'z'
	hidden.Visible = false
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("open", "Open", saveAction{}),
		quit,
		hidden,
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()

	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.MenuActivatedEvent) {
		ran = append(ran, ev.ItemID)
	})
	defer unsub()

	// Lower case reaches an upper-case mnemonic, from a selection on another row.
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 'q'})
	h.waitFor("the mnemonic activated its row", func() bool {
		var n int
		h.onLoop(func() { n = len(ran) })
		return n == 1
	})
	h.settle()
	h.onLoop(func() {
		if len(ran) != 1 || ran[0] != "quit" {
			t.Errorf("activations = %v, want [quit]", ran)
		}
	})

	// A hidden row's mnemonic does nothing: the keystroke would have no visible
	// effect, which is worse than no shortcut at all.
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 'z'})
	h.settle()
	h.settle()
	h.onLoop(func() {
		if len(ran) != 1 {
			t.Errorf("a hidden row's mnemonic fired: %v", ran)
		}
	})
}

// TestAMenuRowPaintsItsAcceleratorToTheRight.
//
// The accelerator is display-only — this widget never binds it — but it has to
// share the row with the label without overwriting it, which is the part that
// goes wrong when the width arithmetic is off.
func TestAMenuRowPaintsItsAcceleratorToTheRight(t *testing.T) {
	save := widget.NewCommand("save", "Save", nil)
	save.Accel = "Ctrl+S"
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{save}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 40, 8)
	defer h.stop()

	got := h.grid()
	if !strings.Contains(got, "Save") || !strings.Contains(got, "Ctrl+S") {
		t.Fatalf("the row did not paint both its label and its accelerator:\n%s", got)
	}
	lx, ly := cellOfLabel(t, h, "Save")
	ax, ay := cellOfLabel(t, h, "Ctrl+S")
	if ay != ly {
		t.Errorf("the accelerator is on line %d and the label on %d; they share a row", ay, ly)
	}
	if ax <= lx+len("Save") {
		t.Errorf("the accelerator at column %d overlaps the label starting at %d", ax, lx)
	}
}

// TestAModelRejectionSaysWhichRowAndWhy.
//
// errors.Is answers "what kind"; the message is what a developer reads at three
// in the morning. An error saying only "invalid model" leaves them diffing two
// slices by hand.
func TestAModelRejectionSaysWhichRowAndWhy(t *testing.T) {
	m := widget.NewMenu()
	err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("dup", "One", nil),
		widget.NewSubmenu("s", "More", []widget.MenuItemModel{
			widget.NewCommand("dup", "Two", nil),
		}),
	})
	if err == nil {
		t.Fatal("the duplicate was accepted")
	}
	msg := err.Error()
	for _, want := range []string{"dup", "duplicate"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message %q does not mention %q", msg, want)
		}
	}
}

// stateRecorder is a consumer row painter that records the state it was handed
// for each row, which is the only way to assert what the renderer contract
// actually delivers.
type stateRecorder struct {
	mu   sync.Mutex
	seen map[widget.ItemID]widget.RowState
}

func (r *stateRecorder) RenderRow(s tui.Surface, row widget.RowView, st widget.RowState) {
	r.mu.Lock()
	if r.seen == nil {
		r.seen = map[widget.ItemID]widget.RowState{}
	}
	r.seen[row.ID] = st
	r.mu.Unlock()
	s.SetCell(0, 0, "x", style.New())
}

func (r *stateRecorder) stateOf(id widget.ItemID) widget.RowState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen[id]
}

// TestARendererSeesFourIndependentFlags.
//
// This is the whole reason RowState is a struct. An enum can report only the
// highest-priority thing true at the moment, so a renderer could not tell a
// selected row that owns the open submenu from a selected row that does not,
// nor either of them from the same row in a menu that has lost focus. Those are
// precisely the distinctions a custom row painter exists to draw.
func TestARendererSeesFourIndependentFlags(t *testing.T) {
	rr := &stateRecorder{}
	m := widget.NewMenu(widget.WithRowRenderer(rr))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", nil),
		}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	elsewhere := widget.NewButton("elsewhere")
	root := tui.NewFlex(tui.Vertical)
	root.Add(m, elsewhere)
	host := widget.NewOverlayHost(root)
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()

	// Selected and focused, nothing open.
	if got := rr.stateOf("file"); got != (widget.RowState{Selected: true, Focused: true}) {
		t.Errorf("state = %+v, want selected+focused", got)
	}

	// Opening the level moves the SELECTION into it — the new level owns the
	// keyboard — while the parent row gains Open. That combination is the one an
	// enum cannot express: the row is not selected and is not armed, yet it is
	// the row a cascade draws as active, and only Open says so.
	h.onLoop(func() {
		if err := m.Open("file"); err != nil {
			t.Errorf("Open: %v", err)
		}
	})
	h.settle()
	if got := rr.stateOf("file"); !got.Open {
		t.Errorf("state = %+v after opening its level, want Open set", got)
	}
	if got := rr.stateOf("new"); !got.Selected {
		t.Errorf("the child row's state = %+v, want the selection to have moved into "+
			"the level that just opened", got)
	}

	// Focus leaves the Menu. Focused goes; Open does not — the level is still
	// open, and a renderer drawing the cascade must still see it.
	h.onLoop(func() { elsewhere.Context().RequestFocus() })
	h.settle()
	h.settle()
	got := rr.stateOf("file")
	if got.Focused {
		t.Errorf("state = %+v after focus left the menu, want Focused clear", got)
	}
	if !got.Open {
		t.Errorf("state = %+v: losing focus did not close the level, so Open stands", got)
	}
}
