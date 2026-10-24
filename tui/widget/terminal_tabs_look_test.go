package widget

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/pty"
	"github.com/yongjohnlee80/golib/tui/style"
)

// terminal_tabs_look_test.go: a tab renamed (SetLabel), the body with no tab (SetEmptyText), and
// the look every tab takes (SetMinimumContrast, SetCursorColor) or its own (WithMinimumContrast,
// WithCursorColor).

func TestTerminalTabsRenameATabAndSayWhatTheEmptyBodyIs(t *testing.T) {
	tabs := NewTerminalTabs()
	changes := 0
	tabs.SetOnChanged(func() { changes++ })
	p := newResultProc()
	defer close(p.release)
	id := addTestTerminal(t, tabs, "one", p)
	before := changes
	if err := tabs.SetLabel(id, "codename-one"); err != nil {
		t.Fatal(err)
	}
	if got := tabs.Tabs()[0].Label; got != "codename-one" || changes != before+1 {
		t.Errorf("label %q, %d changes; want the new label, one change", got, changes-before)
	}
	if err := tabs.SetLabel(id, "codename-one"); err != nil || changes != before+1 {
		t.Errorf("the same label again: %v, %d changes", err, changes-before)
	}
	if err := tabs.SetLabel("terminal-none", "x"); !errors.Is(err, ErrTerminalTabUnknown) {
		t.Errorf("an unknown tab: %v", err)
	}

	empty := NewTerminalTabs()
	h := startAppInternal(t, empty, 40, 4)
	defer h.stopInternal()
	h.syncInternal()
	if got := h.tb.String(); !strings.Contains(got, "No terminals. Use + to add one.") {
		t.Fatalf("the default empty body:\n%s", got)
	}
	h.onLoopInternal(func() { empty.SetEmptyText("No agent running") })
	h.syncInternal()
	if got := h.tb.String(); !strings.Contains(got, "No agent running") || strings.Contains(got, "No terminals") {
		t.Errorf("the empty body after SetEmptyText:\n%s", got)
	}
}

func TestTerminalTabsGiveEveryTabTheirLookAndATabItsOwn(t *testing.T) {
	red, blue := style.RGB(255, 0, 0), style.RGB(0, 0, 255)
	tabs := NewTerminalTabs()
	p1, p2, p3 := newResultProc(), newResultProc(), newResultProc()
	defer close(p1.release)
	defer close(p2.release)
	defer close(p3.release)
	own := addTestTerminalWith(t, tabs, "own", p1, WithMinimumContrast(7), WithCursorColor(blue), WithVimKeys(true))
	if term := pickTerminal(tabs, own); term.minContrast != 7 {
		t.Errorf("a tab's own contrast: %v", term.minContrast)
	} else if c, ok := term.CursorColor(); !ok || c != blue {
		t.Errorf("a tab's own cursor colour: %v %v", c, ok)
	} else if !term.cfg.vimKeys {
		t.Error("a tab's own vim keys are off")
	}
	plain := addTestTerminal(t, tabs, "plain", p2)
	if term := pickTerminal(tabs, plain); term.minContrast != 0 || term.cfg.vimKeys {
		t.Errorf("a tab with no look took one: %v, vim keys %v", term.minContrast, term.cfg.vimKeys)
	}
	// the container's look reaches every tab, those already added and those added later
	tabs.SetMinimumContrast(4.5)
	tabs.SetCursorColor(red)
	tabs.SetVimKeys(false)
	later := addTestTerminalWith(t, tabs, "later", p3, WithVimKeys(true))
	for _, id := range []TerminalTabID{own, plain, later} {
		term := pickTerminal(tabs, id)
		c, ok := term.CursorColor()
		if term.minContrast != 4.5 || !ok || c != red || term.cfg.vimKeys {
			t.Errorf("%s: contrast %v, cursor %v %v, vim keys %v; want the container's", id, term.minContrast, c, ok, term.cfg.vimKeys)
		}
	}
	// a keymap changing reaches the shells already running
	tabs.SetVimKeys(true)
	for _, id := range []TerminalTabID{own, plain, later} {
		if !pickTerminal(tabs, id).cfg.vimKeys {
			t.Errorf("%s: SetVimKeys(true) did not reach it", id)
		}
	}
}

func addTestTerminalWith(t *testing.T, tabs *TerminalTabs, label string, p *resultProc, opts ...TerminalOption) TerminalTabID {
	t.Helper()
	id, err := tabs.Add(label, append(opts, WithStarter(func(pty.Cmd) (TerminalProcess, error) { return p, nil }))...)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestTerminalTabsOpenTheirOwnMenuForATab: SetMenu's rows open over the strip for the tab a right
// press is on (and for the active one by Enter); a chosen row runs with that tab, and the menu
// closes.
func TestTerminalTabsOpenTheirOwnMenuForATab(t *testing.T) {
	tabs := NewTerminalTabs()
	p1, p2 := newResultProc(), newResultProc()
	defer close(p1.release)
	defer close(p2.release)
	a := addTestTerminal(t, tabs, "alpha", p1)
	b := addTestTerminal(t, tabs, "beta", p2)
	var ran []TerminalTabID
	tabs.SetMenu(func(id TerminalTabID) []MenuItemModel {
		return []MenuItemModel{
			NewCommand("close", "Close", TerminalTabsMenuAction{ID: "tabs.close", Run: func(id TerminalTabID) { ran = append(ran, id) }}),
			NewSubmenu("goto", "Go to", []MenuItemModel{
				NewCommand("goto.a", "alpha", TerminalTabsMenuAction{ID: "tabs.goto", Run: func(TerminalTabID) {}}),
			}),
		}
	})
	h := startAppInternal(t, NewOverlayHost(tabs), 40, 8)
	defer h.stopInternal()
	h.syncInternal()
	var bx int
	h.onLoopInternal(func() { bx = tabs.tabX(b) })
	if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: bx + 1, Y: 0}); err != nil {
		t.Fatal(err)
	}
	var open bool
	menuOpen := func(want bool) bool { // injected events and the loop's Updates are unordered: poll
		for range 300 {
			h.onLoopInternal(func() { open = tabs.MenuOpen() })
			if open == want {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	menuOpen(true)
	h.syncInternal()
	if !open || !strings.Contains(h.tb.String(), "Close") || !strings.Contains(h.tb.String(), "Go to") {
		t.Fatalf("the right press on beta opened no menu:\n%s", h.tb.String())
	}
	if err := h.tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}); err != nil { // the first row, Close
		t.Fatal(err)
	}
	menuOpen(false)
	if len(ran) != 1 || ran[0] != b || open {
		t.Fatalf("Close ran for %v (want beta, %s), the menu open %v", ran, b, open)
	}
	// Enter on the focused strip opens it for the active tab
	h.onLoopInternal(func() { tabs.Context().RequestFocus() })
	if err := h.tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}); err != nil {
		t.Fatal(err)
	}
	if !menuOpen(true) {
		t.Fatalf("Enter on the strip opened no menu for %s:\n%s", a, h.tb.String())
	}
}

// TestTerminalTabsPlusOpensTheAddMenu: SetAddMenu's rows open under + (a click on it, or + typed on
// the focused strip), instead of the host's add callback; a chosen row runs with no tab. The strip's
// spans name + and ≡ for its own menus, as for the host's callbacks.
func TestTerminalTabsPlusOpensTheAddMenu(t *testing.T) {
	tabs := NewTerminalTabs()
	p1 := newResultProc()
	defer close(p1.release)
	addTestTerminal(t, tabs, "alpha", p1)
	added, picked := 0, []TerminalTabID{"unset"}
	tabs.SetActions(func() { added++ }, nil)
	tabs.SetMenu(func(TerminalTabID) []MenuItemModel { return []MenuItemModel{NewCommand("x", "Tab row", nil)} })
	tabs.SetAddMenu(func() []MenuItemModel {
		return []MenuItemModel{NewCommand("claude", "Claude", TerminalTabsMenuAction{ID: "add.claude", Run: func(id TerminalTabID) { picked = []TerminalTabID{id} }})}
	})
	h := startAppInternal(t, NewOverlayHost(tabs), 40, 8)
	defer h.stopInternal()
	var spans []TabSpan
	var plus int
	h.onLoopInternal(func() { spans, plus = tabs.TabSpans(), tabs.addX() })
	if len(spans) != 3 || spans[1].X != plus {
		t.Fatalf("spans %v: want the tab, + at %d and ≡", spans, plus)
	}
	var open bool
	menuOpen := func(want bool) bool {
		for range 300 {
			h.onLoopInternal(func() { open = tabs.MenuOpen() })
			if open == want {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: plus + 1, Y: 0}); err != nil {
		t.Fatal(err)
	}
	if !menuOpen(true) {
		t.Fatalf("a click on + opened no menu:\n%s", h.tb.String())
	}
	h.syncInternal()
	if !strings.Contains(h.tb.String(), "Claude") {
		t.Fatalf("the add menu shows no Claude:\n%s", h.tb.String())
	}
	if err := h.tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}); err != nil {
		t.Fatal(err)
	}
	if !menuOpen(false) || len(picked) != 1 || picked[0] != "" || added != 0 {
		t.Fatalf("Claude ran with %v (want no tab), the add callback %d times, the menu open %v", picked, added, open)
	}
	h.onLoopInternal(func() { tabs.Context().RequestFocus() })
	if err := h.tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Text: "+"}); err != nil {
		t.Fatal(err)
	}
	if !menuOpen(true) || added != 0 {
		t.Fatalf("+ on the strip opened no add menu (the add callback %d times):\n%s", added, h.tb.String())
	}
}

// TestTerminalTabsAddMenuAloneShowsPlus: SetAddMenu with no add callback (and no menu) still gives
// the strip its + span, and + typed on it opens the menu.
func TestTerminalTabsAddMenuAloneShowsPlus(t *testing.T) {
	tabs := NewTerminalTabs()
	p1 := newResultProc()
	defer close(p1.release)
	addTestTerminal(t, tabs, "alpha", p1)
	tabs.SetAddMenu(func() []MenuItemModel { return []MenuItemModel{NewCommand("claude", "Claude", nil)} })
	h := startAppInternal(t, NewOverlayHost(tabs), 40, 8)
	defer h.stopInternal()
	var spans []TabSpan
	var plus int
	h.onLoopInternal(func() { spans, plus = tabs.TabSpans(), tabs.addX() })
	if len(spans) != 2 || spans[1].X != plus {
		t.Fatalf("spans %v: want the tab and + at %d", spans, plus)
	}
	h.onLoopInternal(func() { tabs.Context().RequestFocus() })
	if err := h.tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Text: "+"}); err != nil {
		t.Fatal(err)
	}
	for range 300 {
		var open bool
		h.onLoopInternal(func() { open = tabs.MenuOpen() })
		if open {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("+ opened no add menu:\n%s", h.tb.String())
}

// TestTerminalTabsDrawTheirTabsInTheirFaceAndTheStripInTheWindows: SetCellFace reaches every tab,
// those added later too; the strip, and a menu opened over a tab, are in the window's face, and
// the menu closed, the tab's cells beneath come back in its face.
func TestTerminalTabsDrawTheirTabsInTheirFaceAndTheStripInTheWindows(t *testing.T) {
	tabs := NewTerminalTabs()
	p1, p2 := newResultProc(), newResultProc()
	defer close(p1.release)
	defer close(p2.release)
	a := addTestTerminal(t, tabs, "alpha", p1)
	tabs.SetCellFace("Iosevka Term")
	b := addTestTerminal(t, tabs, "beta", p2)
	tabs.SetCellFace("Iosevka Term") // the same again: nothing to change
	for _, id := range []TerminalTabID{a, b} {
		if got := pickTerminal(tabs, id).CellFace(); got != "Iosevka Term" {
			t.Fatalf("%s: face %q", id, got)
		}
	}
	tabs.SetMenu(func(TerminalTabID) []MenuItemModel { return []MenuItemModel{NewCommand("close", "Close tab", nil)} })
	h := startAppInternal(t, NewOverlayHost(tabs), 40, 8)
	defer h.stopInternal()
	face := func(x, y int) uint16 { return h.tb.Snapshot()[y][x].Face }
	var bodyFace uint16
	for range 300 {
		h.syncInternal()
		if bodyFace = face(20, 3); bodyFace != 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if bodyFace == 0 {
		t.Fatal("the tab's body is in the window's face")
	}
	if got := face(1, 0); got != 0 {
		t.Errorf("the strip is in face %d, want the window's", got)
	}
	var bx int
	h.onLoopInternal(func() { bx = tabs.tabX(b) })
	if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: bx + 1, Y: 0}); err != nil {
		t.Fatal(err)
	}
	var open bool
	for range 300 {
		h.onLoopInternal(func() { open = tabs.MenuOpen() })
		if open {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.syncInternal()
	row, col := -1, -1
	for y, cells := range h.tb.Snapshot() {
		for x := range cells {
			if x+5 <= len(cells) && cells[x].Content == "C" && cells[x+1].Content == "l" && cells[x+2].Content == "o" {
				row, col = y, x
			}
		}
	}
	if row < 1 {
		t.Fatalf("no menu over the tab:\n%s", h.tb.String())
	}
	if got := face(col, row); got != 0 {
		t.Errorf("the menu over the tab is in face %d, want the window's", got)
	}
	h.onLoopInternal(tabs.closeMenu)
	for range 300 {
		h.syncInternal()
		if face(col, row) == bodyFace {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the menu closed, the cell beneath is in face %d, want the tab's %d", face(col, row), bodyFace)
}

// TestTerminalTabsOpenTheirMenuAnywhereOnTheStripAndAnEmptyBody: with their own menu, the strip
// shows ≡ with no tab, and a right press on a body no terminal fills opens the menu (for no tab)
// where it was; with a tab, a right press on the strip's blank opens it for the active tab, and the
// live terminal's body stays the terminal's.
func TestTerminalTabsOpenTheirMenuAnywhereOnTheStripAndAnEmptyBody(t *testing.T) {
	tabs := NewTerminalTabs()
	var opened []TerminalTabID
	tabs.SetMenu(func(id TerminalTabID) []MenuItemModel {
		opened = append(opened, id)
		return []MenuItemModel{NewCommand("hide", "Hide", nil)}
	})
	h := startAppInternal(t, NewOverlayHost(tabs), 40, 8)
	defer h.stopInternal()
	var spans []TabSpan
	h.onLoopInternal(func() { spans = tabs.TabSpans() })
	if len(spans) != 1 {
		t.Fatalf("no tab: spans %v, want ≡ alone", spans)
	}
	press := func(x, y int) {
		t.Helper()
		if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: x, Y: y}); err != nil {
			t.Fatal(err)
		}
	}
	menuOpen := func(want bool) bool {
		var open bool
		for range 300 {
			h.onLoopInternal(func() { open = tabs.MenuOpen() })
			if open == want {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	press(10, 4) // the empty body
	if !menuOpen(true) || len(opened) != 1 || opened[0] != "" {
		t.Fatalf("a right press on the empty body: open %v, for %q", tabs.MenuOpen(), opened)
	}
	h.onLoopInternal(tabs.closeMenu)
	menuOpen(false)

	p := newResultProc()
	defer close(p.release)
	var a TerminalTabID
	h.onLoopInternal(func() {
		id, err := tabs.Add("alpha", WithStarter(func(pty.Cmd) (TerminalProcess, error) { return p, nil }))
		if err != nil {
			t.Error(err)
		}
		a = id
		if err := tabs.Select(a); err != nil {
			t.Error(err)
		}
	})
	press(30, 0) // the strip's blank
	if !menuOpen(true) || len(opened) != 2 || opened[1] != a {
		t.Fatalf("a right press on the strip's blank: open %v, for %q (want %s)", tabs.MenuOpen(), opened, a)
	}
	h.onLoopInternal(tabs.closeMenu)
	menuOpen(false)
	press(10, 4) // the live terminal's body
	time.Sleep(100 * time.Millisecond)
	if tabs.MenuOpen() || len(opened) != 2 {
		t.Fatalf("a right press on a live terminal opened the strip's menu (for %q)", opened)
	}
}

// SetHostKeys reaches every tab, those added later too.
func TestTerminalTabsGiveEveryTabTheirHostKeys(t *testing.T) {
	tabs := NewTerminalTabs()
	p1, p2 := newResultProc(), newResultProc()
	defer close(p1.release)
	defer close(p2.release)
	a := addTestTerminal(t, tabs, "a", p1)
	tabs.SetHostKeys(func(k tui.KeyEvent) bool { return k.Code == tui.KeyF11 })
	b := addTestTerminal(t, tabs, "b", p2)
	f11 := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF11}
	for _, id := range []TerminalTabID{a, b} {
		if hk := pickTerminal(tabs, id).hostKeys; hk == nil || !hk(f11) {
			t.Errorf("%s: no host keys", id)
		}
	}
}

// SetNormalHint is shown at the strip's right end while the active tab is in normal mode, and only
// then: a host tells a user who never learnt normal mode how to get back to typing.
func TestTerminalTabsShowTheNormalHintWhileTheActiveTabIsInNormalMode(t *testing.T) {
	tabs := NewTerminalTabs()
	p := newResultProc()
	defer close(p.release)
	id := addTestTerminal(t, tabs, "one", p)
	h := startAppInternal(t, tabs, 60, 4)
	defer h.stopInternal()
	h.syncInternal()
	const hint = "Ctrl+T or i returns to typing"
	shown := func() bool { h.syncInternal(); return strings.Contains(h.tb.String(), hint) }
	h.onLoopInternal(func() { tabs.SetNormalHint(hint) })
	if shown() {
		t.Fatal("the hint shows in input mode")
	}
	e, _ := tabs.find(id)
	h.onLoopInternal(func() { e.terminal.setMode(TerminalNormal) })
	if !shown() {
		t.Fatalf("no hint in normal mode:\n%s", h.tb.String())
	}
	h.onLoopInternal(func() { e.terminal.setMode(TerminalInput) })
	if shown() {
		t.Fatal("the hint stays after input mode returned")
	}
	h.onLoopInternal(func() { tabs.SetNormalHint(""); e.terminal.setMode(TerminalNormal) })
	if shown() {
		t.Fatal("no hint set, yet one shows")
	}
}
