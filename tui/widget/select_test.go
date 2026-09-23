package widget_test

// Select: overlay, focus trap, filter, async options.

import (
	"context"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
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

// TestSelectOpenOverlayAndCommit asserts the open state renders on
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

// TestSelectFocusTrapAndEsc asserts that Tab cycles within the open
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

// TestSelectFilter asserts filter-as-you-type narrows options;
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

// TestSelectAsyncOptions asserts options loaded via App.Go with the
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

// release completes a click. The default recognizer arms on press and
// activates on the RELEASE inside the target, so a press alone proves nothing.
func release(x, y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y}
}

// A CLICK OPENS THE OPTIONS, which is the whole of what activating a dropdown
// can mean.
//
// Select answered Enter, Space and Down from the keyboard and was inert under
// the mouse: the runtime starts a pointer gesture only on a target that
// implements tui.Activatable, and Select did not. A click hit-tested to the
// field, moved focus to it, and stopped — reported as "it just focuses".
func TestSelectOpensOnClick(t *testing.T) {
	h, _, sh := selectFixture(t, widget.WithOptions(selectItems("alpha", "beta")))
	h.wantNotContains("beta") // closed: only the field is on screen

	h.inject(click(0, 0), release(0, 0))
	h.barrier(sh)
	h.wantContains("beta")
}

// A PRESS THAT LEAVES THE FIELD BEFORE RELEASE DOES NOT OPEN IT. Without this
// the cell above passes for an implementation that opens on any press, which
// is the behaviour that makes a dropdown impossible to dismiss by changing
// your mind mid-click.
func TestSelectClickAbandonedOutsideDoesNotOpen(t *testing.T) {
	h, _, sh := selectFixture(t, widget.WithOptions(selectItems("alpha", "beta")))
	h.inject(click(0, 0), release(0, 9)) // press on the field, release far away
	h.barrier(sh)
	h.wantNotContains("beta")
}

// THE PLACEHOLDER SAYS THE FIELD IS EMPTY. A blank field cannot be told apart
// from one still loading its options, or one whose selection has an empty
// label.
func TestSelectPlaceholderShowsOnlyWhileNothingIsSelected(t *testing.T) {
	h, _, sh := selectFixture(t,
		widget.WithOptions(selectItems("alpha", "beta")),
		widget.WithSelectPlaceholder[string]("empty…"))
	h.wantContains("empty…")

	// Commit a real choice; the placeholder must give way to it.
	h.inject(key(tui.KeyEnter), key(tui.KeyDown), key(tui.KeyEnter))
	h.barrier(sh)
	h.wantNotContains("empty…")
	h.wantContains("beta")
}

// THE AFFORDANCE CAN BE WITHHELD, for a host that already marks the focused
// row itself and does not want the same thing said twice in two places.
func TestSelectAffordanceIsDrawnByDefaultAndCanBeWithheld(t *testing.T) {
	h, _, _ := selectFixture(t, widget.WithOptions(selectItems("alpha")))
	h.wantContains("▾")

	h2, _, _ := selectFixture(t,
		widget.WithOptions(selectItems("alpha")),
		widget.WithAffordance[string](false))
	h2.wantNotContains("▾")
}

// j AND k MOVE THE HIGHLIGHT on a select that is not filtering.
func TestSelectVimMotionWhenNotFiltering(t *testing.T) {
	h, sel, sh := selectFixture(t, widget.WithOptions(selectItems("alpha", "beta", "gamma")))
	h.inject(key(tui.KeyEnter), key('j'), key('j'), key(tui.KeyEnter))
	h.barrier(sh)

	got, ok := sel.Value()
	if !ok || got != "GAMMA" {
		t.Fatalf("j j Enter committed %q (ok=%v), want GAMMA", got, ok)
	}
}

// AND ON A FILTERING SELECT THEY ARE TEXT, because j and k are letters the
// operator most needs to type: "jetbrains", "sqlite".
func TestSelectFilteringKeepsJAndKAsText(t *testing.T) {
	h, _, sh := selectFixture(t,
		widget.WithOptions(selectItems("jetbrains", "kotlin", "alpha")),
		widget.WithFilter[string](true))
	h.inject(key(tui.KeyEnter), key('j'))
	h.barrier(sh)

	// The query narrowed to the one row beginning with j; a j that had moved
	// the highlight instead would have left every row on screen.
	h.wantContains("jetbrains")
	h.wantNotContains("kotlin")
}

// A FOCUSED SELECT LOOKS FOCUSED. Closed, it is one row of text; without a
// focus look it is the same row whether the keyboard is in it or not, so an
// operator tabbing through a form could not tell they had reached it.
func TestSelectFieldIsMarkedWhileFocused(t *testing.T) {
	h, _, sh := selectFixture(t, widget.WithOptions(selectItems("alpha")))
	// The fixture Tabs onto the Select, so it holds the keyboard here.
	if cellAttrs(h, 0, 0).Mask&tui.AttrReverse == 0 {
		t.Error("a focused select's field is not marked")
	}

	h.inject(tab()) // move to the sibling text input
	h.barrier(sh)
	if cellAttrs(h, 0, 0).Mask&tui.AttrReverse != 0 {
		t.Error("the select is still marked after the keyboard left it")
	}
}

// rowOfLabel is the screen row a label appears on, or -1.
func rowOfLabel(h *harness, want string) int {
	for y, line := range strings.Split(h.grid(), "\n") {
		if strings.Contains(line, want) {
			return y
		}
	}
	return -1
}

// A SECOND ACTIVATION REPORTS THAT IT DID NOTHING. Activate is the pointer and
// keyboard's shared entry point, and one that claimed success on an already
// open list would tell the runtime a gesture had an effect it did not have.
func TestSelectActivateIsNotRepeatable(t *testing.T) {
	h, sel, sh := selectFixture(t, widget.WithOptions(selectItems("alpha")))

	// Activate and SetArmed mutate loop-owned state — both reach MarkDirty,
	// which writes the dirty flag the running loop reads in maybeFrame. Called
	// straight from the test goroutine they race the loop, and the detector
	// catches it. onLoop is the sanctioned way for a test to touch this state.
	var first, second bool
	h.onLoop(func() { first = sel.Activate(tui.OriginProgrammatic) })
	if !first {
		t.Fatal("the first activation did not open the list")
	}
	h.barrier(sh)
	h.onLoop(func() { second = sel.Activate(tui.OriginProgrammatic) })
	if second {
		t.Error("a second activation reported that it opened an already-open list")
	}
	// SetArmed is idempotent; the second call must not churn a redraw.
	h.onLoop(func() {
		sel.SetArmed(true)
		sel.SetArmed(true)
		sel.SetArmed(false)
	})
}

// A CUSTOM FOCUS LOOK REPLACES THE REVERSED DEFAULT.
func TestSelectFocusedStyleIsReplaceable(t *testing.T) {
	h, _, _ := selectFixture(t,
		widget.WithOptions(selectItems("alpha")),
		widget.WithSelectFocusedStyle[string](style.New().Bold(true)))
	at := cellAttrs(h, 0, 0)
	if at.Mask&tui.AttrReverse != 0 {
		t.Error("the default reversed look survived a replacement style")
	}
	if at.Mask&tui.AttrBold == 0 {
		t.Error("the replacement focus style was not applied")
	}
}

// AND k IS TEXT WHILE FILTERING TOO. The j cell alone passes for an
// implementation that special-cased one letter.
func TestSelectFilteringKeepsKAsText(t *testing.T) {
	h, _, sh := selectFixture(t,
		widget.WithOptions(selectItems("kotlin", "alpha")),
		widget.WithFilter[string](true))
	h.inject(key(tui.KeyEnter), key('k'))
	h.barrier(sh)
	h.wantContains("kotlin")
	h.wantNotContains("alpha")
}

// placedSelectFixture puts the Select at a given offset inside the screen, so
// the anchored placement's edge cases have an edge to meet.
func placedSelectFixture(t *testing.T, padRows, padCols, w, h int,
	opts ...widget.SelectOption[string]) (*harness, *shell) {
	t.Helper()
	sel := widget.NewSelect[string](opts...)
	col := tui.NewFlex(tui.Vertical)
	for range padRows {
		col.Add(widget.NewText(" "))
	}
	row := tui.NewFlex(tui.Horizontal)
	if padCols > 0 {
		row.Add(widget.NewText(strings.Repeat(" ", padCols)))
	}
	row.Add(sel)
	col.Add(row)
	sh := newShell(widget.NewOverlayHost(col))
	hh := startApp(t, sh, w, h)
	hh.inject(tab())
	hh.barrier(sh)
	return hh, sh
}
