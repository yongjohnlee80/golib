package widget

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// tab_strip_test.go: TabStrip, the strip TerminalTabs draws, alone: the host's tabs and active one,
// a press choosing a tab, and its menus and + as TerminalTabs's.

// waitInternal polls cond on the loop: injected events and the loop's Updates are unordered.
func waitInternal(h *internalHarness, cond func() bool) bool {
	var ok bool
	for range 300 {
		h.onLoopInternal(func() { ok = cond() })
		if ok {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestATabStripShowsTheHostsTabsAndChoosesByAPress: the strip draws the tabs it is given and marks
// the active one; a left press on another makes it active and tells the host; the active tab
// stays across SetTabs while it is still there, and goes when it is not.
func TestATabStripShowsTheHostsTabsAndChoosesByAPress(t *testing.T) {
	strip := NewTabStrip()
	strip.SetTabs([]StripTab{{"a", "alpha"}, {"b", "beta"}})
	strip.SetActive("a")
	var chosen []string
	strip.SetOnSelect(func(id string) { chosen = append(chosen, id) })
	h := startAppInternal(t, strip, 40, 1)
	defer h.stopInternal()
	h.syncInternal()
	if got := h.tb.String(); !strings.Contains(got, " alpha ") || !strings.Contains(got, " beta ") {
		t.Fatalf("the strip:\n%s", got)
	}
	var spans []TabSpan
	var active int
	h.onLoopInternal(func() { spans, active = strip.TabSpans(), strip.ActiveTabIndex() })
	if len(spans) != 2 || spans[1].X != len(" alpha ")+1 || active != 0 {
		t.Fatalf("spans %v, active %d: want two tabs, beta after alpha's cell, alpha active", spans, active)
	}

	if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: spans[1].X + 1, Y: 0}); err != nil {
		t.Fatal(err)
	}
	if !waitInternal(h, func() bool { return strip.Active() == "b" }) || len(chosen) != 1 || chosen[0] != "b" {
		t.Fatalf("a press on beta: active %q, chosen %v", strip.Active(), chosen)
	}

	h.onLoopInternal(func() {
		strip.SetTabs([]StripTab{{"c", "gamma"}, {"b", "beta"}})
		active = strip.ActiveTabIndex()
	})
	if active != 1 {
		t.Fatalf("beta still there: active at %d, want 1", active)
	}
	h.onLoopInternal(func() {
		strip.SetTabs([]StripTab{{"c", "gamma"}})
		active = strip.ActiveTabIndex()
	})
	if active != -1 || strip.Active() != "" {
		t.Fatalf("beta gone: active %q at %d, want none", strip.Active(), active)
	}
	h.onLoopInternal(func() { strip.SetActive("nowhere") })
	if strip.Active() != "" || len(chosen) != 1 {
		t.Fatalf("SetActive of no tab: %q; chosen %v (SetActive tells no one)", strip.Active(), chosen)
	}
}

// TestATabStripOpensItsMenusAsTerminalTabsDoes: SetMenu's rows open on a right press on a tab, a
// chosen row running with that tab; + opens SetAddMenu's, a row running with ""; with the host's
// callbacks instead (SetActions), a right press and + call them.
func TestATabStripOpensItsMenusAsTerminalTabsDoes(t *testing.T) {
	strip := NewTabStrip()
	strip.SetTabs([]StripTab{{"a", "alpha"}, {"b", "beta"}})
	strip.SetActive("a")
	var ran []string
	strip.SetMenu(func(id string) []MenuItemModel {
		return []MenuItemModel{NewCommand("close", "Close", TabStripMenuAction{ID: "close", Run: func(id string) { ran = append(ran, "close:"+id) }})}
	})
	strip.SetAddMenu(func() []MenuItemModel {
		return []MenuItemModel{NewCommand("new", "New", TabStripMenuAction{ID: "new", Run: func(id string) { ran = append(ran, "new:"+id) }})}
	})
	h := startAppInternal(t, NewOverlayHost(strip), 40, 8)
	defer h.stopInternal()
	h.syncInternal()
	var spans []TabSpan
	h.onLoopInternal(func() { spans = strip.TabSpans() })
	if len(spans) != 4 {
		t.Fatalf("spans %v: want two tabs, + and ≡", spans)
	}
	menuOpen := func(want bool) bool { return waitInternal(h, func() bool { return strip.MenuOpen() == want }) }

	if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: spans[1].X + 1, Y: 0}); err != nil {
		t.Fatal(err)
	}
	if !menuOpen(true) {
		t.Fatalf("a right press on beta opened no menu:\n%s", h.tb.String())
	}
	if err := h.tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}); err != nil {
		t.Fatal(err)
	}
	if !menuOpen(false) || !waitInternal(h, func() bool { return len(ran) == 1 }) || ran[0] != "close:b" {
		t.Fatalf("Close from beta's menu ran %v", ran)
	}
	if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: spans[2].X + 1, Y: 0}); err != nil {
		t.Fatal(err)
	}
	if !menuOpen(true) {
		t.Fatalf("a press on + opened no menu:\n%s", h.tb.String())
	}
	if err := h.tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}); err != nil {
		t.Fatal(err)
	}
	if !waitInternal(h, func() bool { return len(ran) == 2 }) || ran[1] != "new:" {
		t.Fatalf("New from +'s menu ran %v", ran)
	}

	var added int
	var asked []string
	h.onLoopInternal(func() {
		strip.SetMenu(nil)
		strip.SetAddMenu(nil)
		strip.SetActions(func() { added++ }, func(id string) { asked = append(asked, id) })
		spans = strip.TabSpans()
	})
	if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: spans[0].X + 1, Y: 0}); err != nil {
		t.Fatal(err)
	}
	if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: spans[2].X + 1, Y: 0}); err != nil {
		t.Fatal(err)
	}
	if !waitInternal(h, func() bool { return added == 1 && len(asked) == 1 }) || asked[0] != "a" {
		t.Fatalf("the host's callbacks: added %d, menu asked for %v", added, asked)
	}
}

// TestATabStripIsAsWideAsItsTabs: one row as wide as its tabs, + and ≡ need, within the constraints;
// nothing at all with no tab and no menus.
func TestATabStripIsAsWideAsItsTabs(t *testing.T) {
	strip := NewTabStrip()
	if sz := strip.Layout(tui.Constraints{MaxW: 80, MaxH: 1}); sz.W != 0 || sz.H != 1 {
		t.Fatalf("an empty strip: %v", sz)
	}
	strip.SetTabs([]StripTab{{"a", "alpha"}, {"b", "beta"}})
	want := len(" alpha ") + 1 + len(" beta ")
	if sz := strip.Layout(tui.Constraints{MaxW: 80, MaxH: 1}); sz.W != want {
		t.Fatalf("two tabs: %v, want %d wide", sz, want)
	}
	strip.SetActions(func() {}, nil)
	if sz := strip.Layout(tui.Constraints{MaxW: 80, MaxH: 1}); sz.W != want+4 {
		t.Fatalf("two tabs and +: %v, want %d wide", sz, want+4)
	}
	if sz := strip.Layout(tui.Constraints{MaxW: 10, MaxH: 1}); sz.W != 10 {
		t.Fatalf("in 10 cells: %v", sz)
	}
}

// TestATabStripNarrowerThanItsTabsScrollsThem: tabs wider than the strip scroll between < and >,
// + and ≡ keeping their place within it; > and < move the window, the active tab out of it then;
// a tab made active is scrolled into view.
func TestATabStripNarrowerThanItsTabsScrollsThem(t *testing.T) {
	strip := NewTabStrip()
	var tabs []StripTab
	for i := range 6 {
		tabs = append(tabs, StripTab{ID: fmt.Sprint(i), Label: fmt.Sprintf("tab-%d", i)})
	}
	strip.SetTabs(tabs)
	strip.SetActive("0")
	strip.SetActions(func() {}, nil)
	strip.SetMenu(func(string) []MenuItemModel { return nil })
	h := startAppInternal(t, strip, 30, 1)
	defer h.stopInternal()
	h.syncInternal()
	var spans []TabSpan
	var active int
	look := func() { h.onLoopInternal(func() { spans, active = strip.TabSpans(), strip.ActiveTabIndex() }) }
	look()
	n := len(spans)
	if n < 5 || spans[0].X != 0 || spans[n-1].X+spans[n-1].W > 30 || active != 1 {
		t.Fatalf("spans %v, active at %d: want <, the window from tab-0 (active), >, + and ≡ within 30 cells", spans, active)
	}
	if got := h.tb.String(); !strings.Contains(got, " ← ") || !strings.Contains(got, " → ") || !strings.Contains(got, " + ") || !strings.Contains(got, " ≡ ") {
		t.Fatalf("the strip:\n%s", got)
	}
	press := func(x int) {
		t.Helper()
		if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: 0}); err != nil {
			t.Fatal(err)
		}
	}
	next := spans[n-3] // →, before + and ≡
	press(next.X + 1)
	if !waitInternal(h, func() bool { return strip.ActiveTabIndex() == -1 }) {
		t.Fatalf("> did not scroll tab-0 out of the window:\n%s", h.tb.String())
	}
	press(1) // ←
	if !waitInternal(h, func() bool { return strip.ActiveTabIndex() == 1 }) {
		t.Fatalf("< did not scroll back to tab-0:\n%s", h.tb.String())
	}
	h.onLoopInternal(func() { strip.SetActive("5") })
	if !waitInternal(h, func() bool { i := strip.ActiveTabIndex(); return i > 0 }) || !strings.Contains(h.tb.String(), "tab-5") {
		t.Fatalf("tab-5 made active is not shown:\n%s", h.tb.String())
	}
}

// TestATabStripCapsATabsWidth: SetMaxTabWidth cuts a long label to the cap, its end an ellipsis; a
// short one is as it was.
func TestATabStripCapsATabsWidth(t *testing.T) {
	strip := NewTabStrip()
	strip.SetTabs([]StripTab{{"a", "a-very-long-file-name.md"}, {"b", "b.md"}})
	strip.SetMaxTabWidth(10)
	h := startAppInternal(t, strip, 60, 1)
	defer h.stopInternal()
	h.syncInternal()
	var spans []TabSpan
	h.onLoopInternal(func() { spans = strip.TabSpans() })
	if len(spans) != 2 || spans[0].W != 10 || spans[1].W != len(" b.md ") {
		t.Fatalf("spans %v: want the long tab capped at 10 cells, the short one whole", spans)
	}
	if got := h.tb.String(); !strings.Contains(got, "…") || strings.Contains(got, "file-name") || !strings.Contains(got, " b.md ") {
		t.Fatalf("the strip:\n%s", got)
	}
}

// TestATabStripsCloseMark: with SetOnClose, each tab starts with a ×; a press on it closes that tab
// (the host's callback) and chooses nothing; a press on the rest of the tab chooses it. The × is
// outside the width cap. Without SetOnClose, no ×.
func TestATabStripsCloseMark(t *testing.T) {
	strip := NewTabStrip()
	strip.SetTabs([]StripTab{{"a", "alpha"}, {"b", "a-name-longer-than-the-cap"}})
	strip.SetActive("a")
	strip.SetMaxTabWidth(10)
	var closed, chosen []string
	strip.SetOnClose(func(id string) { closed = append(closed, id) })
	strip.SetOnSelect(func(id string) { chosen = append(chosen, id) })
	h := startAppInternal(t, strip, 60, 1)
	defer h.stopInternal()
	h.syncInternal()
	if got := h.tb.String(); !strings.Contains(got, " × alpha ") || !strings.Contains(got, " × a-name-") {
		t.Fatalf("the strip:\n%s", got)
	}
	var spans []TabSpan
	h.onLoopInternal(func() { spans = strip.TabSpans() })
	if len(spans) != 2 || spans[1].W != 2+10 {
		t.Fatalf("spans %v: want the capped tab 10 cells, its × 2 more", spans)
	}
	press := func(x int) {
		t.Helper()
		if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: 0}); err != nil {
			t.Fatal(err)
		}
	}
	press(spans[1].X + 1) // b's ×
	if !waitInternal(h, func() bool { return len(closed) == 1 }) || closed[0] != "b" || len(chosen) != 0 {
		t.Fatalf("a press on b's ×: closed %v, chosen %v", closed, chosen)
	}
	press(spans[1].X + 4) // b's name
	if !waitInternal(h, func() bool { return len(chosen) == 1 }) || chosen[0] != "b" || len(closed) != 1 {
		t.Fatalf("a press on b's name: closed %v, chosen %v", closed, chosen)
	}
	h.onLoopInternal(func() { strip.SetOnClose(nil) })
	h.syncInternal()
	if got := h.tb.String(); strings.Contains(got, "×") {
		t.Fatalf("SetOnClose(nil) left a ×:\n%s", got)
	}
}

// TestACutTabsHiddenCloseMarkDoesNotClose: a tab cut at the window's edge to less than its × does
// not close on a press where the × would be; one whose × shows does.
func TestACutTabsHiddenCloseMarkDoesNotClose(t *testing.T) {
	strip := NewTabStrip()
	strip.SetTabs([]StripTab{{"a", "alpha"}, {"b", "beta"}, {"c", "gamma"}})
	strip.SetActive("a")
	var closed []string
	strip.SetOnClose(func(id string) { closed = append(closed, id) })
	h := startAppInternal(t, strip, 10, 1) // room for ← and → and two cells: a cut to " …"
	defer h.stopInternal()
	h.syncInternal()
	var spans []TabSpan
	h.onLoopInternal(func() { spans = strip.TabSpans() })
	if len(spans) < 3 {
		t.Fatalf("spans %v: want ←, a cut tab and →", spans)
	}
	if err := h.tb.Inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: spans[1].X, Y: 0}); err != nil {
		t.Fatal(err)
	}
	h.syncInternal()
	time.Sleep(50 * time.Millisecond)
	h.syncInternal()
	if len(closed) != 0 {
		t.Fatalf("a press on a tab cut to %q closed %v", h.tb.String(), closed)
	}
}
