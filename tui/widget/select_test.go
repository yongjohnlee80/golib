package widget_test

// Select: overlay, focus trap, filter, async options (ADR-0007 §2.4, §2.6,
// §5.6).

import (
	"context"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func selectItems(labels ...string) []widget.SelectItem[string] {
	items := make([]widget.SelectItem[string], len(labels))
	for i, l := range labels {
		items[i] = widget.SelectItem[string]{Label: l, Value: strings.ToUpper(l)}
	}
	return items
}

// selectFixture: an OverlayHost root wrapping a Select and a sibling
// TextInput (a second tab stop proving the trap).
func selectFixture(t *testing.T, opts ...widget.SelectOption[string]) (*harness, *widget.Select[string], *shell) {
	t.Helper()
	sel := widget.NewSelect[string](opts...)
	flex := tui.NewFlex(tui.Vertical)
	flex.Add(sel)
	flex.Add(widget.NewTextInput(widget.WithPlaceholder("other stop")))
	sh := newShell(widget.NewOverlayHost(flex))
	h := startApp(t, sh, 30, 10)
	h.inject(tab()) // focus the Select (first stop)
	h.barrier(sh)
	return h, sel, sh
}

// TestSelectOpenOverlayAndCommit asserts §5.6: the open state renders on
// the overlay layer, Enter commits with SelectionChangedEvent, and the
// popup closes.
func TestSelectOpenOverlayAndCommit(t *testing.T) {
	h, sel, sh := selectFixture(t, widget.WithOptions(selectItems("alpha", "beta", "gamma")))
	opened := record[widget.OpenedEvent](h)
	closed := record[widget.ClosedEvent](h)
	sels := record[widget.SelectionChangedEvent](h)

	// Closed state: one-line field with the ▾ affordance; options hidden.
	h.settle()
	h.wantContains("▾")
	h.wantNotContains("beta")

	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	if opened.count() != 1 {
		t.Fatalf("OpenedEvent count = %d", opened.count())
	}
	h.wantContains("alpha") // the option list paints on the overlay
	h.wantContains("beta")

	h.inject(key(tui.KeyDown), key(tui.KeyEnter))
	h.barrier(sh)
	var id tui.NodeID
	var val string
	var ok bool
	h.onLoop(func() { id = sel.NodeID(); val, ok = sel.Value() })
	if ev, evOk := sels.last(); !evOk || ev.Owner != id || ev.Index != 1 || ev.Label != "beta" {
		t.Fatalf("SelectionChangedEvent = %+v, want owner %d index 1 beta", ev, id)
	}
	if !ok || val != "BETA" {
		t.Fatalf("Value = (%q,%v), want (BETA,true)", val, ok)
	}
	if closed.count() != 1 {
		t.Fatalf("ClosedEvent count = %d", closed.count())
	}
	h.wantNotContains("gamma") // overlay gone
	h.wantContains("beta")     // field shows the committed label
}

// TestSelectFocusTrapAndEsc asserts §5.6: Tab cycles within the open
// overlay (the trap), Esc restores the prior focus and selection.
func TestSelectFocusTrapAndEsc(t *testing.T) {
	h, sel, sh := selectFixture(t, widget.WithOptions(selectItems("alpha", "beta")))
	h.inject(key(tui.KeyEnter)) // open
	h.barrier(sh)
	h.wantContains("beta")

	// Tab must NOT escape the trap: the option list stays open and the
	// sibling input never gains focus (the popup stays the only stop).
	h.inject(tab(), tab())
	h.barrier(sh)
	h.wantContains("beta") // still open — Tab stayed inside the trap

	// Esc: closes without change, restores focus to the Select.
	h.inject(key(tui.KeyEscape))
	h.barrier(sh)
	h.wantNotContains("beta")
	var val string
	var ok bool
	h.onLoop(func() { val, ok = sel.Value() })
	if ok {
		t.Fatalf("Esc committed a selection: %q", val)
	}
	// Focus restored to the Select: Enter reopens immediately.
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	h.wantContains("beta")
}

// TestSelectFilter asserts §5.6: filter-as-you-type narrows options;
// Backspace edits the filter.
func TestSelectFilter(t *testing.T) {
	h, _, sh := selectFixture(t,
		widget.WithOptions(selectItems("apple", "banana", "cherry")),
		widget.WithFilter[string](true))
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	h.wantContains("banana")

	h.inject(typeString("che")...)
	h.barrier(sh)
	h.wantContains("cherry")
	h.wantNotContains("banana")
	h.wantNotContains("apple")

	h.inject(key(tui.KeyBackspace), key(tui.KeyBackspace), key(tui.KeyBackspace))
	h.barrier(sh)
	h.wantContains("banana") // filter cleared: all options back

	// Commit the filtered highlight.
	h.inject(typeString("ban")...)
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	h.wantNotContains("cherry")
	h.wantContains("banana") // committed label in the field
}

// TestSelectAsyncOptions asserts §2.6: options loaded via App.Go with the
// Select's NodeID as owner arrive through the addressed TaskResult.
func TestSelectAsyncOptions(t *testing.T) {
	h, sel, sh := selectFixture(t)
	h.settle()
	baseline := h.tb.Flushes()
	var id tui.NodeID
	h.onLoop(func() { id = sel.NodeID() })
	h.app.Go(id, func(context.Context) (any, error) {
		return selectItems("loaded-1", "loaded-2"), nil
	})
	// SetOptions (from the addressed TaskResult) marks dirty → a frame.
	h.waitFor("async options installed", func() bool { return h.tb.Flushes() > baseline })
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	h.wantContains("loaded-1")
	h.wantContains("loaded-2")
}

// TestSelectClickOutsideCloses: the overlay layer sees outside clicks
// first and closes.
func TestSelectClickOutsideCloses(t *testing.T) {
	h, _, sh := selectFixture(t, widget.WithOptions(selectItems("alpha", "beta")))
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	h.wantContains("beta")
	h.inject(click(0, 9)) // far corner, outside the centered panel
	h.barrier(sh)
	h.wantNotContains("beta")
}
