package widget_test

// OWNERSHIP AND BOUNDARIES.
//
// A Menu's logical level stack and the popups actually mounted on an overlay
// host are two records of one fact, and every test here is about them
// disagreeing: a request nobody owns, a level that outlives its Menu, a row
// that is not on screen but still has a rectangle, a shell that reconfigures a
// widget it does not own.
//
// The failures this file names are not crashes. Each one leaves a Menu that
// reports a state it does not have, which is the kind of defect an application
// built on it discovers much later and somewhere else.

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// fileMenuModel is a two-row model whose second row is a submenu, used wherever
// a test needs a level to open.
func fileMenuModel() []widget.MenuItemModel {
	return []widget.MenuItemModel{
		widget.NewCommand("quit", "Quit", nil),
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "NewDoc", nil),
		}),
	}
}

// fixedBox lays its child out at exactly the size given, whatever the parent
// allows, so a test can constrain one widget without constraining the host that
// has to have room for its popups.
type fixedBox struct {
	widget.Base
	child tui.Component
	w, hh int
}

func (f *fixedBox) Init(ctx *tui.Context) {
	f.Base.Init(ctx)
	ctx.Mount(f.child)
}

func (f *fixedBox) Layout(cs tui.Constraints) tui.Size {
	ctx := f.Context()
	got := ctx.LayoutChild(f.child, tui.Tight(tui.Size{W: f.w, H: f.hh}))
	ctx.PlaceChild(f.child, tui.Rect{X: 0, Y: 0, W: got.W, H: got.H})
	return cs.Constrain(tui.Size{W: f.w, H: f.hh})
}

func (f *fixedBox) Render(tui.Surface) {}

// TestOpeningALevelConcernsOnlyTheHostThatContainsTheMenu.
//
// The request used to go out on the bus with no addressee, so EVERY mounted
// OverlayHost in the application received it and tried to satisfy it. The one
// containing the Menu succeeded; the others refused a request that was never
// theirs and published a failure saying so — an application watching for those
// failures to detect a broken menu saw one every time a menu opened correctly.
//
// It is now a call to exactly one host: the nearest enclosing one. The
// unrelated host is not merely quiet, it is uninvolved, and that is what this
// asserts — no layer of any kind appears in it.
func TestOpeningALevelConcernsOnlyTheHostThatContainsTheMenu(t *testing.T) {
	menu := widget.NewMenu()
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	mine := widget.NewOverlayHost(menu)
	theirs := widget.NewOverlayHost(widget.NewText("unrelated"))
	root := tui.NewFlex(tui.Vertical)
	root.AddWeighted(mine, 1)
	root.AddWeighted(theirs, 1)
	h := startApp(t, root, 40, 20)
	h.settle()

	var err error
	h.onLoop(func() { err = menu.Open("file") })
	h.settle()
	h.settle()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	h.wantContains("NewDoc") // the level really did open

	var inMine, inTheirs int
	h.onLoop(func() {
		for range mine.AnchoredLayers() {
			inMine++
		}
		for range theirs.AnchoredLayers() {
			inTheirs++
		}
	})
	if inMine != 1 {
		t.Errorf("the containing host holds %d anchored layers, want 1", inMine)
	}
	if inTheirs != 0 {
		t.Errorf("an unrelated host holds %d anchored layers; opening a level is a "+
			"request to ONE host, not a broadcast", inTheirs)
	}
}

// TestANestedHostServesTheMenuInsideIt.
//
// "Nearest enclosing" is the rule, and nesting is where it earns its keep: a
// Menu inside an inner host must use that one, so its popup is clipped and
// ordered by the container it actually lives in rather than escaping to an
// outer host that knows nothing about it.
func TestANestedHostServesTheMenuInsideIt(t *testing.T) {
	menu := widget.NewMenu()
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	inner := widget.NewOverlayHost(menu)
	outer := widget.NewOverlayHost(inner)
	h := startApp(t, outer, 40, 20)
	h.settle()

	h.onLoop(func() {
		if err := menu.Open("file"); err != nil {
			t.Errorf("Open: %v", err)
		}
	})
	h.settle()

	var innerN, outerN int
	h.onLoop(func() {
		for range inner.AnchoredLayers() {
			innerN++
		}
		for range outer.AnchoredLayers() {
			outerN++
		}
	})
	if innerN != 1 || outerN != 0 {
		t.Errorf("layers: inner=%d outer=%d, want 1 and 0 — the nearest enclosing "+
			"host serves the menu", innerN, outerN)
	}
}

// TestAMenuWithNoHostSaysSoRatherThanSilentlyDoingNothing.
//
// Under the broadcast there was nobody to answer, so Open returned nil and the
// caller waited for a popup that could never arrive. A Menu needs an overlay
// host; saying so is the whole of the fix.
func TestAMenuWithNoHostSaysSoRatherThanSilentlyDoingNothing(t *testing.T) {
	menu := widget.NewMenu()
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	root := tui.NewFlex(tui.Vertical)
	root.Add(menu) // no OverlayHost anywhere above it
	h := startApp(t, root, 40, 20)
	h.settle()

	var err error
	h.onLoop(func() { err = menu.Open("file") })
	h.settle()
	if !errors.Is(err, widget.ErrAnchorUnusable) {
		t.Errorf("Open returned %v with no host above the Menu, want ErrAnchorUnusable", err)
	}
	if n := openLevelsOn(t, h, menu); n != 0 {
		t.Errorf("OpenLevels() = %d after an open that mounted nothing, want 0", n)
	}
}

// TestALevelDoesNotOutliveTheMenuThatOpenedIt.
//
// Unmounting a Menu left its logical level stack untouched, so the Menu went on
// reporting an open level with nothing mounted. The second half is what makes
// that fatal rather than untidy: re-mounting and opening the same row LOOKED
// idempotent against the stale entry, so Open returned success and mounted
// nothing at all. The menu was then permanently unable to open that submenu.
func TestALevelDoesNotOutliveTheMenuThatOpenedIt(t *testing.T) {
	menu := widget.NewMenu()
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	inner := tui.NewFlex(tui.Vertical)
	inner.Add(menu)
	host := widget.NewOverlayHost(inner)
	h := startApp(t, host, 40, 20)
	h.settle()

	h.onLoop(func() {
		if err := menu.Open("file"); err != nil {
			t.Errorf("Open: %v", err)
		}
	})
	h.settle()
	h.wantContains("NewDoc")

	// SAME TURN. The anchor-loss commit would eventually dismiss a level whose
	// owner has gone, so asserting after a settle would pass without the Menu
	// cleaning up after itself at all. The guarantee is that the model is
	// correct the moment the Menu unmounts, before any further layout.
	var sameTurn int
	h.onLoop(func() {
		inner.Remove(menu)
		sameTurn = menu.OpenLevels()
	})
	if sameTurn != 0 {
		t.Errorf("OpenLevels() = %d immediately after the Menu was unmounted, want 0; "+
			"the level is the host's child, so nothing else takes it down", sameTurn)
	}
	h.settle()
	h.wantNotContains("NewDoc")

	// Re-mounted, the same row must open again.
	h.onLoop(func() { inner.Add(menu) })
	h.settle()
	var err error
	h.onLoop(func() { err = menu.Open("file") })
	h.settle()
	h.settle()
	if err != nil {
		t.Fatalf("Open after remount: %v", err)
	}
	if n := openLevelsOn(t, h, menu); n != 1 {
		t.Errorf("OpenLevels() = %d after remounting and reopening, want 1", n)
	}
	h.wantContains("NewDoc")
}

// TestAHostDrivenDismissalReachesTheMenusLevelStack.
//
// The host can close a layer without asking: an explicit CloseAnchored, or the
// anchor-loss commit when the row a level hangs off stops being laid out. The
// Menu owns the logical stack, so it has to hear about it — otherwise the level
// is unmounted and still counted, and the next Open on that row is refused as a
// duplicate.
func TestAHostDrivenDismissalReachesTheMenusLevelStack(t *testing.T) {
	menu := widget.NewMenu()
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	host := widget.NewOverlayHost(menu)
	h := startApp(t, host, 40, 20)
	h.settle()

	h.onLoop(func() {
		if err := menu.Open("file"); err != nil {
			t.Errorf("Open: %v", err)
		}
	})
	h.settle()
	h.wantContains("NewDoc")

	// An EXPLICIT close by the application, through the host's own API. Nothing
	// tells the Menu; it has to be listening.
	var layer widget.LayerID
	h.onLoop(func() {
		for id := range host.AnchoredLayers() {
			layer = id
		}
	})
	if layer == "" {
		t.Fatal("no anchored layer to close")
	}
	h.onLoop(func() { host.CloseAnchored(layer, widget.DismissProgrammatic) })
	h.settle()
	h.settle()
	if n := openLevelsOn(t, h, menu); n != 0 {
		t.Errorf("OpenLevels() = %d after the host closed the layer, want 0", n)
	}
	h.wantNotContains("NewDoc")

	// And the model is consistent enough to open it again — the stale entry
	// used to make this look like a duplicate and do nothing.
	h.onLoop(func() {
		if err := menu.Open("file"); err != nil {
			t.Errorf("Open after a host-driven dismissal: %v", err)
		}
	})
	h.settle()
	h.wantContains("NewDoc")
}

// TestALostAnchorReachesTheMenusLevelStackToo.
//
// The other way a host closes a level without being asked: the row it hangs off
// stops being laid out, and the anchor-loss commit dismisses it.
func TestALostAnchorReachesTheMenusLevelStackToo(t *testing.T) {
	menu := widget.NewMenu()
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	host := widget.NewOverlayHost(menu)
	h := startApp(t, host, 40, 20)
	h.settle()

	h.onLoop(func() {
		if err := menu.Open("file"); err != nil {
			t.Errorf("Open: %v", err)
		}
	})
	h.settle()
	h.wantContains("NewDoc")

	h.onLoop(func() { menu.SetVisible("file", false) })
	h.settle()
	h.settle()
	if n := openLevelsOn(t, h, menu); n != 0 {
		t.Errorf("OpenLevels() = %d after the row it hung off was hidden, want 0", n)
	}
	h.wantNotContains("NewDoc")
}

// TestAClippedRowIsNotAnAnchorAndIsNotClickable.
//
// A row outside the rect the parent gave the Menu is not on screen. Declaring a
// region for it anyway gave it an anchor a submenu could hang off and a
// rectangle a click could land in, so a menu squeezed to one line would open a
// popup pointing at a row the user cannot see.
func TestAClippedRowIsNotAnAnchorAndIsNotClickable(t *testing.T) {
	menu := widget.NewMenu()
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	// One line: only "Quit" is painted, and "File" — the submenu — is clipped.
	host := widget.NewOverlayHost(&fixedBox{child: menu, w: 20, hh: 1})
	h := startApp(t, host, 40, 20)
	h.settle()

	h.wantContains("Quit")
	h.wantNotContains("File")

	var err error
	h.onLoop(func() { err = menu.Open("file") })
	h.settle()
	h.settle()
	if err == nil {
		t.Error("Open succeeded for a row that is not on screen; a clipped row has no " +
			"region to anchor to, and the popup it produces points at nothing")
	} else if !errors.Is(err, widget.ErrAnchorUnusable) {
		t.Errorf("Open returned %v, want an ErrAnchorUnusable", err)
	}
	if n := openLevelsOn(t, h, menu); n != 0 {
		t.Errorf("OpenLevels() = %d after a refused open, want 0; a refusal must "+
			"change nothing", n)
	}
	h.wantNotContains("NewDoc")
}

// TestConstructingOneBarDoesNotReconfigureAnother.
//
// A MenuBar is a placement shell. Writing the orientation into the Menu at
// CONSTRUCTION time made it a mutation of shared state: building a second bar
// around the same Menu silently re-laid-out the first, and a Menu that had ever
// been in a bar stayed horizontal when later mounted on its own.
func TestConstructingOneBarDoesNotReconfigureAnother(t *testing.T) {
	menu := widget.NewMenu()
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	top := widget.NewMenuBar(menu, widget.WithBarPlacement(widget.BarPlacementTop))
	// Merely CONSTRUCTED, never mounted: it must not touch the Menu.
	_ = widget.NewMenuBar(menu, widget.WithBarPlacement(widget.BarPlacementLeft))

	host := widget.NewOverlayHost(top)
	h := startApp(t, host, 40, 20)
	h.settle()

	row := h.row(0)
	if !strings.Contains(row, "Quit") || !strings.Contains(row, "File") {
		t.Errorf("the top bar's first row is %q; both rows belong on one line, so the "+
			"bar was re-laid-out vertically by a bar nobody mounted\n%s", row, h.grid())
	}
}

// TestABarThatWasNeverMountedChangesNothing.
//
// The case applying the orientation at mount cannot cover, and therefore the
// one that pins the constructor down: build a bar around a Menu, never mount
// the bar, and mount the Menu on its own. A constructor that wrote the
// orientation would have made it horizontal with no bar anywhere in the tree
// and nothing to ever undo it.
func TestABarThatWasNeverMountedChangesNothing(t *testing.T) {
	menu := widget.NewMenu()
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	_ = widget.NewMenuBar(menu, widget.WithBarPlacement(widget.BarPlacementTop))

	host := widget.NewOverlayHost(menu) // the MENU is mounted, not the bar
	h := startApp(t, host, 40, 20)
	h.settle()

	if r := h.row(0); strings.Contains(r, "File") {
		t.Errorf("row 0 is %q: a bare Menu stacks its rows, so File belongs on row 1 — "+
			"constructing a bar reconfigured a Menu the bar never owned\n%s", r, h.grid())
	}
}

// TestAMenuLeavesABarTheWayItFoundIt.
//
// The other half of the same rule: a Menu that has been in a bar and is later
// mounted bare is a vertical menu again. Orientation belongs to the mounted
// shell's lifetime, not to the Menu for ever.
func TestAMenuLeavesABarTheWayItFoundIt(t *testing.T) {
	menu := widget.NewMenu()
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	bar := widget.NewMenuBar(menu, widget.WithBarPlacement(widget.BarPlacementTop))
	inner := tui.NewFlex(tui.Vertical)
	inner.Add(bar)
	host := widget.NewOverlayHost(inner)
	h := startApp(t, host, 40, 20)
	h.settle()
	if r := h.row(0); !strings.Contains(r, "Quit") || !strings.Contains(r, "File") {
		t.Fatalf("the bar did not lay its menu out horizontally: %q", r)
	}

	// Take the menu out of the bar and mount it on its own.
	h.onLoop(func() {
		inner.Remove(bar)
		inner.Add(menu)
	})
	h.settle()
	h.settle()
	if r := h.row(0); strings.Contains(r, "File") {
		t.Errorf("row 0 is %q: a bare Menu stacks its rows, so File belongs on row 1 — "+
			"the bar's orientation outlived the bar\n%s", r, h.grid())
	}
}

// nilRenderer is a POINTER-shaped RowRenderer, so a nil *nilRenderer satisfies
// the interface with a live type descriptor — the shape `!= nil` cannot see.
type nilRenderer struct{}

func (*nilRenderer) RenderRow(tui.Surface, widget.RowView, widget.RowState) {}

// TestATypedNilRowRendererIsNoRenderer.
//
// The renderer is stored at construction and called during a render pass, so a
// typed nil reaching storage panics frames later with nothing naming the option
// that supplied it. Normalising at the boundary means the built-in painter runs
// and the menu is simply drawn.
func TestATypedNilRowRendererIsNoRenderer(t *testing.T) {
	var rr *nilRenderer
	menu := widget.NewMenu(widget.WithRowRenderer(rr))
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	host := widget.NewOverlayHost(menu)
	h := startApp(t, host, 30, 10)
	h.settle()

	// The built-in painter ran: a typed-nil renderer is absent, not a renderer
	// that paints nothing.
	h.wantContains("Quit")
	h.wantContains("File")
}

// TestAHorizontalBarDoesNotAddressRowsPastItsEdge.
//
// The same rule as a clipped vertical row, on the other axis. A bar narrower
// than its rows used to declare a region and a hit rect for every row in the
// model, so a row pushed entirely off the end was still an anchor a submenu
// could hang from.
//
// " Quit " is six cells, so a six-column bar has nothing left for "File" — and
// the wide case below is the control that proves the narrow one is measuring
// the edge rather than something else about the fixture.
func TestAHorizontalBarDoesNotAddressRowsPastItsEdge(t *testing.T) {
	for _, tc := range []struct {
		name    string
		width   int
		wantErr bool
	}{
		{"the row fits", 30, false},
		{"the row is pushed off the end", 6, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			menu := widget.NewMenu()
			if err := menu.SetModel(fileMenuModel()); err != nil {
				t.Fatalf("SetModel: %v", err)
			}
			bar := widget.NewMenuBar(menu, widget.WithBarPlacement(widget.BarPlacementTop))
			host := widget.NewOverlayHost(&fixedBox{child: bar, w: tc.width, hh: 1})
			h := startApp(t, host, 40, 20)
			h.settle()

			var err error
			h.onLoop(func() { err = menu.Open("file") })
			h.settle()
			h.settle()

			switch {
			case tc.wantErr && err == nil:
				t.Errorf("Open succeeded for a bar row with no painted cells; a row "+
					"past the edge has no region to anchor to\n%s", h.grid())
			case !tc.wantErr && err != nil:
				t.Errorf("Open failed for a row that fits: %v", err)
			}
			want := 1
			if tc.wantErr {
				want = 0
			}
			if n := openLevelsOn(t, h, menu); n != want {
				t.Errorf("OpenLevels() = %d, want %d", n, want)
			}
		})
	}
}

// TestAPopupSmallerThanItsOwnFrameDoesNotInvertItsInterior.
//
// A popup constrained below the two cells its frame occupies would compute a
// NEGATIVE interior, and a negative extent is not a smaller rectangle — it is
// one whose arithmetic silently inverts every bound derived from it, so the
// row loop's own clipping test stops meaning anything.
func TestAPopupSmallerThanItsOwnFrameDoesNotInvertItsInterior(t *testing.T) {
	menu := widget.NewMenu()
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	// The whole host is one cell, so the popup cannot fit even its frame.
	host := widget.NewOverlayHost(menu)
	h := startApp(t, host, 1, 1)
	h.settle()

	// Opening is refused or succeeds, but either way the frame must not panic
	// and the app must still be running afterwards.
	h.onLoop(func() { _ = menu.Open("file") })
	h.settle()
	h.settle()
	h.onLoop(func() {}) // a live loop is the assertion: nothing panicked
}

// refusingHost is a CONSUMER-SUPPLIED overlay host: it implements the same two
// operations a Menu needs, and nothing else in this package names its type.
// That is the point of resolving the host through an interface rather than a
// concrete *OverlayHost — and it is the only way to exercise a host that
// REFUSES, which the real one does only in races this test cannot stage.
type refusingHost struct {
	widget.Base
	child tui.Component
	err   error
	opens int
}

func (h *refusingHost) Init(ctx *tui.Context) {
	h.Base.Init(ctx)
	ctx.Mount(h.child)
}

func (h *refusingHost) Layout(cs tui.Constraints) tui.Size {
	ctx := h.Context()
	got := ctx.LayoutChild(h.child, cs)
	ctx.PlaceChild(h.child, tui.Rect{X: 0, Y: 0, W: got.W, H: got.H})
	return got
}

func (h *refusingHost) Render(tui.Surface) {}

func (h *refusingHost) OpenAnchored(widget.LayerID, tui.Component, widget.AnchorSpec, widget.AnchorPolicy) error {
	h.opens++
	return h.err
}

func (h *refusingHost) CloseAnchored(widget.LayerID, widget.DismissReason) {}

// TestAHostRefusalLeavesTheMenuExactlyAsItWas.
//
// The ordering that makes opening a transaction: the level is recorded only
// AFTER the host has mounted it. Recording first and asking afterwards is what
// left a level in the model with nothing on screen — and because reopening an
// already-open row is idempotent, that stale entry made every later Open on
// that row look like a duplicate and do nothing at all.
func TestAHostRefusalLeavesTheMenuExactlyAsItWas(t *testing.T) {
	menu := widget.NewMenu()
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	host := &refusingHost{child: menu, err: errors.New("refused by the host")}
	h := startApp(t, host, 40, 20)
	h.settle()

	var err error
	h.onLoop(func() { err = menu.Open("file") })
	h.settle()

	if host.opens != 1 {
		t.Fatalf("the host saw %d open requests, want 1 — the Menu did not resolve "+
			"the consumer-supplied host at all", host.opens)
	}
	if err == nil {
		t.Error("Open returned nil though the host refused; the host's error is the " +
			"caller's answer")
	}
	if n := openLevelsOn(t, h, menu); n != 0 {
		t.Errorf("OpenLevels() = %d after the host refused, want 0", n)
	}

	// And the row is still openable: no stale entry is making the next attempt
	// look like a duplicate.
	h.onLoop(func() {
		host.err = nil
		err = menu.Open("file")
	})
	h.settle()
	if err != nil {
		t.Fatalf("Open after a refusal: %v", err)
	}
	if n := openLevelsOn(t, h, menu); n != 1 {
		t.Errorf("OpenLevels() = %d once the host accepted, want 1", n)
	}
}

// TestASelectOpensInItsOwnHostOnly.
//
// The same defect as the Menu's, in the widget that had it first, and worse:
// Select's open request was an unaddressed Bus event, so EVERY mounted
// OverlayHost received it and each tried to mount the same popup component.
// The runtime refuses to mount one component twice — by panicking — so an
// application with two hosts CRASHED when the user opened a dropdown.
//
// The assertion is therefore that the app is still running, plus that the
// unrelated host stayed empty.
func TestASelectOpensInItsOwnHostOnly(t *testing.T) {
	sel := widget.NewSelect(widget.WithOptions([]widget.SelectItem[string]{
		{Label: "alpha", Value: "alpha"},
		{Label: "beta", Value: "beta"},
	}))
	mine := widget.NewOverlayHost(sel)
	theirs := widget.NewOverlayHost(widget.NewText("unrelated"))
	root := tui.NewFlex(tui.Vertical)
	root.AddWeighted(mine, 1)
	root.AddWeighted(theirs, 1)
	h := startApp(t, root, 40, 20)
	h.settle()

	h.onLoop(func() { sel.Context().RequestFocus() })
	h.settle()
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.waitFor("the dropdown opened", func() bool {
		return strings.Contains(h.grid(), "alpha")
	})

	// The containing host holds it; the other one holds only its own content.
	var mineN, theirsN int
	h.onLoop(func() {
		mineN, theirsN = mine.Stack.Len(), theirs.Stack.Len()
	})
	if mineN != 2 {
		t.Errorf("the containing host holds %d layers, want 2 (content + popup)", mineN)
	}
	if theirsN != 1 {
		t.Errorf("an unrelated host holds %d layers, want 1 (its own content only)", theirsN)
	}

	// Closing takes it down again, and the loop is still alive to say so.
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	h.waitFor("the dropdown closed", func() bool {
		return !strings.Contains(h.grid(), "alpha")
	})
}

// TestASelectWithNoHostDoesNotClaimToBeOpen.
//
// A Select needs somewhere to put its dropdown. With no OverlayHost above it
// there is nowhere, and the honest response is to stay closed: the old code
// published its request into the void, set its own open flag, and then drew a
// field claiming a list was showing that no host had ever mounted.
func TestASelectWithNoHostDoesNotClaimToBeOpen(t *testing.T) {
	sel := widget.NewSelect(widget.WithOptions([]widget.SelectItem[string]{
		{Label: "alpha", Value: "alpha"},
	}))
	root := tui.NewFlex(tui.Vertical)
	root.Add(sel) // no OverlayHost anywhere above it
	h := startApp(t, root, 30, 8)
	h.settle()

	h.onLoop(func() { sel.Context().RequestFocus() })
	h.settle()
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.settle()
	h.settle()

	// No option list on screen, and the loop is still alive to be asked.
	h.wantNotContains("alpha")
	h.onLoop(func() {})
}

// TestAHostCloseIsVisibleToTheMenuInTheSameTurn.
//
// CloseAnchored unmounts the popup SYNCHRONOUSLY, so by the time it returns the
// level does not exist. Reconciling through the bus made the Menu's own
// invariant depend on a Lane-B delivery that has not happened yet: in the same
// loop closure, OpenLevels() still counted the level, and reopening the row hit
// the already-deepest early return — returning success and mounting nothing.
//
// No settle between the close and the reopen. That is the whole test: the
// previous version of it settled twice and therefore observed eventual
// reconciliation rather than the one-lifecycle invariant.
func TestAHostCloseIsVisibleToTheMenuInTheSameTurn(t *testing.T) {
	menu := widget.NewMenu()
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	host := widget.NewOverlayHost(menu)
	h := startApp(t, host, 40, 20)
	h.settle()

	h.onLoop(func() {
		if err := menu.Open("file"); err != nil {
			t.Errorf("Open: %v", err)
		}
	})
	h.settle()
	h.wantContains("NewDoc")

	var layer widget.LayerID
	h.onLoop(func() {
		for id := range host.AnchoredLayers() {
			layer = id
		}
	})
	if layer == "" {
		t.Fatal("no anchored layer to close")
	}

	var afterClose int
	var reopenErr error
	var afterReopen int
	h.onLoop(func() {
		host.CloseAnchored(layer, widget.DismissProgrammatic)
		afterClose = menu.OpenLevels()
		reopenErr = menu.Open("file")
		afterReopen = menu.OpenLevels()
	})
	h.settle()

	if afterClose != 0 {
		t.Errorf("OpenLevels() = %d immediately after CloseAnchored returned, want 0; "+
			"the popup was already unmounted, so the model was describing a level "+
			"that no longer existed", afterClose)
	}
	if reopenErr != nil {
		t.Errorf("reopening in the same turn: %v", reopenErr)
	}
	if afterReopen != 1 {
		t.Errorf("OpenLevels() = %d after reopening, want 1", afterReopen)
	}
	// And the level really is on screen, not merely counted.
	h.settle()
	h.wantContains("NewDoc")
	var layers int
	h.onLoop(func() {
		for range host.AnchoredLayers() {
			layers++
		}
	})
	if layers != 1 {
		t.Errorf("the host holds %d anchored layers after the reopen, want 1", layers)
	}
}

// TestAClippedBarRowNeverReachesTheRowRenderer.
//
// "Not on screen" has to mean one set of rows, not three. Horizontal layout
// stopped creating rectangles at the edge, but Render went on iterating every
// visible row in the model and indexing the map — a missing entry reads as the
// zero rect, and the consumer's RowRenderer was still called with it. A row the
// package has declared unaddressable was still executing consumer code.
func TestAClippedBarRowNeverReachesTheRowRenderer(t *testing.T) {
	rr := &stateRecorder{}
	menu := widget.NewMenu(widget.WithRowRenderer(rr))
	if err := menu.SetModel(fileMenuModel()); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	bar := widget.NewMenuBar(menu, widget.WithBarPlacement(widget.BarPlacementTop))
	// Six columns: " Quit " fills them, so "File" has no painted cells at all.
	host := widget.NewOverlayHost(&fixedBox{child: bar, w: 6, hh: 1})
	h := startApp(t, host, 40, 20)
	h.settle()
	h.settle()

	if !rr.sawRow("quit") {
		t.Fatal("the renderer never saw the row that IS on screen, so its silence " +
			"about the clipped one would prove nothing")
	}
	if rr.sawRow("file") {
		t.Error("the renderer was invoked for a row with no painted cells; layout, " +
			"hit-testing and rendering must agree on which rows exist")
	}
}
