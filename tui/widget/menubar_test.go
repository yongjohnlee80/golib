package widget_test

// MenuBar is a placement shell and MenuItem is a leaf, and the thing worth
// testing about each is what it does NOT own. A bar that grew its own model, or
// an item that grew its own activation path, would work until the day it
// disagreed with the thing it was supposed to be a view of.

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TestABarLaysItsRowsAlongOneLineAndDropsAwayFromItsEdge.
//
// A bar does NOT position itself on screen — its parent does, like every other
// component. What BarPlacement decides is orientation and which way a dropdown
// opens: a bottom bar must drop UPWARD, or the dropdown covers the row that
// opened it.
func TestABarLaysItsRowsAlongOneLineAndDropsAwayFromItsEdge(t *testing.T) {
	for _, tc := range []struct {
		placement widget.BarPlacement
		wantAbove bool
	}{
		{widget.BarPlacementTop, false},
		{widget.BarPlacementBottom, true},
	} {
		t.Run(tc.placement.String(), func(t *testing.T) {
			m := widget.NewMenu()
			if err := m.SetModel([]widget.MenuItemModel{
				widget.NewSubmenu("file", "File", []widget.MenuItemModel{
					widget.NewCommand("new", "New", nil),
				}),
				widget.NewCommand("edit", "Edit", nil),
			}); err != nil {
				t.Fatalf("SetModel: %v", err)
			}
			bar := widget.NewMenuBar(m, widget.WithBarPlacement(tc.placement))
			// Placed against the matching edge by an ordinary layout, which is
			// how a caller does it and is the point of the test above.
			// The body is WEIGHTED so it fills, which is what actually pushes the
			// bar against its edge. Two unweighted children would both take their
			// natural one line and leave the "bottom" bar on line 1, where there
			// is no room above it and the test would be measuring the flip
			// policy's fallback rather than the bar's drop direction.
			body := widget.NewText("body")
			root := tui.NewFlex(tui.Vertical)
			if tc.wantAbove {
				root.AddWeighted(body, 1)
				root.Add(bar)
			} else {
				root.Add(bar)
				root.AddWeighted(body, 1)
			}
			host := widget.NewOverlayHost(root)
			h := startApp(t, host, 40, 12)
			defer h.stop()
			h.onLoop(func() { m.Context().RequestFocus() })
			h.settle()

			fx, fy := cellOfLabel(t, h, "File")
			ex, ey := cellOfLabel(t, h, "Edit")
			if fy != ey {
				t.Errorf("rows are on lines %d and %d; a bar lays them along one line", fy, ey)
			}
			if ex <= fx {
				t.Errorf("Edit is at column %d and File at %d; the rows are out of order", ex, fx)
			}

			h.onLoop(func() {
				if err := m.Open("file"); err != nil {
					t.Errorf("Open: %v", err)
				}
			})
			h.settle()
			h.settle()
			_, dy := cellOfLabel(t, h, "New")
			if tc.wantAbove && dy >= fy {
				t.Errorf("the dropdown opened at line %d, at or below the bar on line %d; "+
					"a bottom bar must drop upward or it covers the row that opened it", dy, fy)
			}
			if !tc.wantAbove && dy <= fy {
				t.Errorf("the dropdown opened at line %d, at or above the bar on line %d", dy, fy)
			}
		})
	}
}

// TestABarDelegatesItsWholeLifecycleToItsMenu.
//
// One lifecycle, not two. The bar owns placement; everything a caller can ask
// about the menu is asked of the menu, so the two cannot come to disagree about
// what is selected or what is open.
func TestABarDelegatesItsWholeLifecycleToItsMenu(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", nil),
		}),
		widget.NewCommand("help", "Help", nil),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	bar := widget.NewMenuBar(m)
	if bar.Menu() != m {
		t.Fatal("Menu() does not return the menu the bar was built over")
	}
	host := widget.NewOverlayHost(bar)
	h := startApp(t, host, 40, 12)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()

	h.onLoop(func() {
		if err := bar.Menu().Open("file"); err != nil {
			t.Errorf("Open through the bar's menu: %v", err)
		}
	})
	h.settle()
	h.settle()

	if got := openLevelsOn(t, h, bar.Menu()); got != 1 {
		t.Errorf("OpenLevels() = %d, want 1", got)
	}
	if got := h.grid(); !strings.Contains(got, "New") {
		t.Errorf("the dropdown is not on screen:\n%s", got)
	}
}

// TestABarStepsWithTheHorizontalArrows.
//
// Arrow direction follows the layout. A bar driven by Up/Down would behave like
// a list rotated ninety degrees, which every user notices at once — and Down on
// a bar should OPEN the dropdown, not move along it.
func TestABarStepsWithTheHorizontalArrows(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", nil),
		}),
		widget.NewCommand("edit", "Edit", nil),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	bar := widget.NewMenuBar(m)
	host := widget.NewOverlayHost(bar)
	h := startApp(t, host, 40, 12)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()

	if id := selectedOn(t, h, m); id != "file" {
		t.Fatalf("initial selection is %q", id)
	}
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight})
	h.settle()
	if id := selectedOn(t, h, m); id != "edit" {
		t.Errorf("Right selected %q, want edit", id)
	}
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyLeft})
	h.settle()
	if id := selectedOn(t, h, m); id != "file" {
		t.Errorf("Left selected %q, want file", id)
	}

	// Down opens the dropdown rather than stepping.
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown})
	h.waitFor("the dropdown opened", func() bool { return openLevelsOn(t, h, m) == 1 })
}

// TestANilMenuIsRefusedAtConstruction.
func TestANilMenuIsRefusedAtConstruction(t *testing.T) {
	if f := fatalFromWidgetExt(func() { widget.NewMenuBar(nil) }); f == nil {
		t.Error("NewMenuBar(nil) was accepted; a placement shell over nothing shows nothing")
	}
	// And the placement bound is probed at its edge, not at a far value.
	if f := fatalFromWidgetExt(func() {
		widget.NewMenuBar(widget.NewMenu(), widget.WithBarPlacement(widget.BarPlacementRight))
	}); f != nil {
		t.Errorf("the last valid placement was rejected: %v", f.Rule)
	}
	if f := fatalFromWidgetExt(func() {
		widget.NewMenuBar(widget.NewMenu(), widget.WithBarPlacement(widget.BarPlacementRight+1))
	}); f == nil {
		t.Error("WithBarPlacement accepted the first value past the declared set")
	}
}

// ─── MenuItem ────────────────────────────────────────────────────────────────

// TestAStandaloneItemActivatesThroughTheRuntime.
//
// MenuItem is its own node, so it is armed and activated by the SAME generic
// recogniser a Button uses, and emits ControlActivatedEvent because Owner
// identifies it. A model row cannot use either — which is the whole reason the
// two types exist.
func TestAStandaloneItemActivatesThroughTheRuntime(t *testing.T) {
	var ran atomic.Int64
	item := widget.NewMenuItem("Rename", widget.WithOnRun(func() { ran.Add(1) }))
	host := widget.NewOverlayHost(item)
	h := startApp(t, host, 30, 6)
	defer h.stop()
	h.onLoop(func() { item.Context().RequestFocus() })
	h.settle()

	var events []tui.ControlActivatedEvent
	unsub := tui.Subscribe(h.app.Bus(), func(ev tui.ControlActivatedEvent) {
		events = append(events, ev)
	})
	defer unsub()

	x, y := cellOfLabel(t, h, "Rename")
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
	h.waitFor("activated by pointer", func() bool { return ran.Load() == 1 })
	h.settle()

	if len(events) != 1 {
		t.Fatalf("%d ControlActivatedEvent, want 1", len(events))
	}
	if events[0].Owner != item.NodeID() {
		t.Errorf("the event names node %d, want the item %d", events[0].Owner, item.NodeID())
	}

	// And the keyboard reaches the same path.
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.waitFor("activated by keyboard", func() bool { return ran.Load() == 2 })
}

// TestACheckItemTogglesBeforeItsCallbackRuns.
func TestACheckItemTogglesBeforeItsCallbackRuns(t *testing.T) {
	var sawChecked atomic.Bool
	var item *widget.MenuItem
	item = widget.NewMenuItem("Wrap",
		widget.WithItemKind(widget.ItemKindCheck),
		widget.WithOnRun(func() { sawChecked.Store(item.Checked()) }))
	host := widget.NewOverlayHost(item)
	h := startApp(t, host, 30, 6)
	defer h.stop()
	h.onLoop(func() { item.Context().RequestFocus() })
	h.settle()

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.waitFor("toggled", func() bool {
		var c bool
		h.onLoop(func() { c = item.Checked() })
		return c
	})
	if !sawChecked.Load() {
		t.Error("the callback saw the OLD state; the toggle must happen first")
	}
}

// TestADisabledItemIsNeitherFocusableNorActivatable.
func TestADisabledItemIsNeitherFocusableNorActivatable(t *testing.T) {
	var ran atomic.Int64
	item := widget.NewMenuItem("Delete",
		widget.WithItemEnabled(false),
		widget.WithOnRun(func() { ran.Add(1) }))
	other := widget.NewButton("Other")
	root := tui.NewFlex(tui.Vertical)
	root.Add(item, other)
	host := widget.NewOverlayHost(root)
	h := startApp(t, host, 30, 8)
	defer h.stop()
	h.settle()

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
	h.settle()
	var itemFocused bool
	h.onLoop(func() { itemFocused = item.Context() != nil && item.Context().Focused() })
	if itemFocused {
		t.Error("Tab landed on a disabled item")
	}

	x, y := cellOfLabel(t, h, "Delete")
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
	h.settle()
	h.settle()
	if ran.Load() != 0 {
		t.Errorf("a disabled item activated %d times", ran.Load())
	}

	// Enabling it makes it reachable, which is the control proving the assertions
	// above observed disabled-ness rather than a broken fixture.
	h.onLoop(func() { item.SetEnabled(true) })
	h.settle()
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
	h.waitFor("the enabled item activates", func() bool { return ran.Load() == 1 })
}

// TestAStandaloneItemRefusesTheKindsItCannotBe.
//
// A submenu needs a Menu to cascade into and a separator is not a control, so
// neither is a thing a standalone item can be. Refused at construction, where
// the kind is written in source.
func TestAStandaloneItemRefusesTheKindsItCannotBe(t *testing.T) {
	for _, k := range []widget.ItemKind{widget.ItemKindSubmenu, widget.ItemKindSeparator, widget.ItemKindRadio + 1} {
		if f := fatalFromWidgetExt(func() {
			widget.NewMenuItem("X", widget.WithItemKind(k))
		}); f == nil {
			t.Errorf("NewMenuItem accepted kind %v", k)
		}
	}
	for _, k := range []widget.ItemKind{widget.ItemKindCommand, widget.ItemKindCheck, widget.ItemKindRadio} {
		if f := fatalFromWidgetExt(func() {
			widget.NewMenuItem("X", widget.WithItemKind(k))
		}); f != nil {
			t.Errorf("NewMenuItem rejected the legal kind %v: %v", k, f.Rule)
		}
	}
}
