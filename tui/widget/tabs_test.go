package widget_test

// Tabs switching, keep-mounted semantics, navigation, and autofocus tests.
//
// This file pairs with tabs.go and validates the tab bar navigation, lifecycle,
// and layout contracts.

import (
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// lifeProbe records mounts/unmounts (Tabs keep-mounted semantics).
type lifeProbe struct {
	widget.Base
	label    string
	inits    atomic.Int32
	unmounts atomic.Int32
}

func (p *lifeProbe) Init(ctx *tui.Context) {
	p.Base.Init(ctx)
	p.inits.Add(1)
	ctx.OnUnmount(func() { p.unmounts.Add(1) })
}

func (p *lifeProbe) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: 5, H: 1})
}

func (p *lifeProbe) Render(s tui.Surface) {
	for x, r := range p.label {
		s.SetCell(x, 0, string(r), style.New())
	}
}

func TestTabsSwitching(t *testing.T) {
	a := &lifeProbe{label: "AAAAA"}
	b := &lifeProbe{label: "BBBBB"}
	tabs := widget.NewTabs(widget.WithTab("one", a), widget.WithTab("two", b))
	sh := newShell(tabs)
	h := startApp(t, sh, 20, 4)
	changed := record[widget.TabChangedEvent](h)
	h.inject(tab()) // focus the bar
	h.barrier(sh)

	h.wantContains("one")
	h.wantContains("AAAAA")
	h.wantNotContains("BBBBB")
	if b.inits.Load() != 0 {
		t.Fatalf("inactive tab content mounted eagerly")
	}

	h.inject(key(']'))
	h.barrier(sh)
	h.wantContains("BBBBB")
	h.wantNotContains("AAAAA")
	if a.unmounts.Load() != 1 {
		t.Fatalf("switching did not unmount the old tab (default mode)")
	}
	var id tui.NodeID
	h.onLoop(func() { id = tabs.NodeID() })
	if ev, ok := changed.last(); !ok || ev.Owner != id || ev.Index != 1 || ev.Label != "two" {
		t.Fatalf("TabChangedEvent = %+v", ev)
	}

	// Ctrl+PgUp cycles back even bubbling from content.
	h.inject(keyMod(tui.KeyPageUp, tui.ModCtrl))
	h.barrier(sh)
	h.wantContains("AAAAA")
	if a.inits.Load() != 2 {
		t.Fatalf("returning to tab one did not remount it (inits=%d)", a.inits.Load())
	}
}

func TestTabsKeepMounted(t *testing.T) {
	a := &lifeProbe{label: "AAAAA"}
	b := &lifeProbe{label: "BBBBB"}
	tabs := widget.NewTabs(widget.WithTab("one", a), widget.WithTab("two", b), widget.WithKeepMounted(true))
	sh := newShell(tabs)
	h := startApp(t, sh, 20, 4)
	h.inject(tab())
	h.inject(key(']'), key('['))
	h.barrier(sh)
	if a.unmounts.Load() != 0 || b.unmounts.Load() != 0 {
		t.Fatalf("keep-mounted content was unmounted (a=%d b=%d)", a.unmounts.Load(), b.unmounts.Load())
	}
	h.wantContains("AAAAA")
	h.wantNotContains("BBBBB") // mounted but not laid out → invisible
}

// TestTabsArrowSwitching: the ←/→ arrows cycle the focused bar, alongside [ ].
func TestTabsArrowSwitching(t *testing.T) {
	a := &lifeProbe{label: "AAAAA"}
	b := &lifeProbe{label: "BBBBB"}
	tabs := widget.NewTabs(widget.WithTab("one", a), widget.WithTab("two", b), widget.WithKeepMounted(true))
	sh := newShell(tabs)
	h := startApp(t, sh, 20, 4)
	h.inject(tab()) // focus the bar
	h.barrier(sh)
	h.wantContains("AAAAA")

	h.inject(key(tui.KeyRight))
	h.barrier(sh)
	h.wantContains("BBBBB")
	h.wantNotContains("AAAAA")

	h.inject(key(tui.KeyLeft))
	h.barrier(sh)
	h.wantContains("AAAAA")
	h.wantNotContains("BBBBB")
}

// TestTabsAutoFocus: WithAutoFocus makes the bar take focus on Init, so the
// arrows drive it without a preceding Tab.
func TestTabsAutoFocus(t *testing.T) {
	a := &lifeProbe{label: "AAAAA"}
	b := &lifeProbe{label: "BBBBB"}
	tabs := widget.NewTabs(widget.WithTab("one", a), widget.WithTab("two", b),
		widget.WithKeepMounted(true), widget.WithAutoFocus(true))
	sh := newShell(tabs)
	h := startApp(t, sh, 20, 4)
	h.barrier(sh)
	h.wantContains("AAAAA")

	h.inject(key(tui.KeyRight)) // no tab() first — the bar auto-focused
	h.barrier(sh)
	h.wantContains("BBBBB")
}

// TestTabsWithoutBar: bar-less mode gives the active content the full height
// (row 0) and paints no bar (no tab labels).
func TestTabsWithoutBar(t *testing.T) {
	a := &lifeProbe{label: "AAAAA"}
	b := &lifeProbe{label: "BBBBB"}
	tabs := widget.NewTabs(
		widget.WithTab("one", a), widget.WithTab("two", b),
		widget.WithKeepMounted(true), widget.WithoutBar())
	sh := newShell(tabs)
	h := startApp(t, sh, 20, 4)
	h.barrier(sh)
	h.wantContains("AAAAA")
	h.wantNotContains("one") // no bar drawn
	h.wantNotContains("two")
}
