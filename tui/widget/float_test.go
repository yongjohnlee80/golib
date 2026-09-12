package widget_test

import (
	"iter"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestFloatModal(t *testing.T) {
	// Base UI: an input in a box; a modal float with its own input.
	baseInput := widget.NewTextInput(widget.WithPlaceholder("base"))
	dlgInput := widget.NewTextInput(widget.WithPlaceholder("dialog"))
	dialog := widget.NewFloat(
		widget.NewBox(dlgInput, widget.WithTitle("Commit message")),
		widget.WithModal(true), widget.WithDimBackground(true))
	host := widget.NewOverlayHost(widget.NewBox(baseInput, widget.WithTitle("Base")))
	host.Attach(dialog)
	sh := newShell(host)
	h := startApp(t, sh, 30, 9)
	dismissed := record[widget.DismissEvent](h)
	h.inject(tab()) // focus the base input
	h.barrier(sh)
	h.wantNotContains("Commit message")

	h.onLoop(dialog.Show)
	h.settle()
	h.wantContains("Commit message")
	h.wantContains("░") // dim scrim

	// Focus seeded into the dialog input: typing lands there.
	h.inject(typeString("msg")...)
	h.barrier(sh)
	var dlgVal, baseVal string
	h.onLoop(func() { dlgVal = dlgInput.Value(); baseVal = baseInput.Value() })
	if dlgVal != "msg" || baseVal != "" {
		t.Fatalf("typing went to dialog=%q base=%q, want dialog only", dlgVal, baseVal)
	}

	// Tab cycles inside the trap only: after Tab, typing still lands in
	// the dialog (its input is the only stop).
	h.inject(tab())
	h.inject(typeString("!")...)
	h.barrier(sh)
	h.onLoop(func() { dlgVal = dlgInput.Value(); baseVal = baseInput.Value() })
	if dlgVal != "msg!" || baseVal != "" {
		t.Fatalf("Tab escaped the trap: dialog=%q base=%q", dlgVal, baseVal)
	}

	// Esc dismisses with DismissEvent; focus restores to the base input.
	h.inject(key(tui.KeyEscape))
	h.barrier(sh)
	h.wantNotContains("Commit message")
	h.wantNotContains("░")
	var id tui.NodeID
	h.onLoop(func() { id = dialog.NodeID() })
	if ev, ok := dismissed.last(); !ok || ev.Owner != id {
		t.Fatalf("DismissEvent = %+v, want owner %d", ev, id)
	}
	h.inject(typeString("back")...)
	h.barrier(sh)
	h.onLoop(func() { baseVal = baseInput.Value() })
	if baseVal != "back" {
		t.Fatalf("focus did not restore to the base input: %q", baseVal)
	}
}

// lateContent builds its focusable child during Init — the common shape
// for content whose data arrives with the float. Focus seeding must
// still reach it: focusFirst at Show() time cannot focus a component
// that is not mounted yet, and treating that as "no focus stop" left
// the modal trapping every key but Esc.
type lateContent struct {
	widget.Base
	ctx  *tui.Context
	list *widget.List[string]
}

func (c *lateContent) Init(ctx *tui.Context) {
	c.Base.Init(ctx)
	c.ctx = ctx
	c.list = widget.NewList(widget.WithItems([]string{"one", "two"},
		func(s string) string { return s }))
	ctx.Mount(c.list)
}
func (c *lateContent) Layout(cs tui.Constraints) tui.Size {
	sz := c.ctx.LayoutChild(c.list, cs)
	c.ctx.PlaceChild(c.list, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
	return cs.Constrain(sz)
}
func (c *lateContent) Render(tui.Surface)            {}
func (c *lateContent) HandleEvent(ev tui.Event) bool { return false }
func (c *lateContent) Add(...tui.Component)          {}
func (c *lateContent) Remove(tui.Component)          {}
func (c *lateContent) Move(tui.Component, int)       {}
func (c *lateContent) Children() iter.Seq[tui.Component] {
	return func(yield func(tui.Component) bool) {
		if c.list != nil {
			yield(c.list)
		}
	}
}

func TestModalFloatSeedsFocusIntoLateMountedContent(t *testing.T) {
	body := &lateContent{}
	f := widget.NewFloat(body, widget.WithModal(true))
	host := widget.NewOverlayHost(widget.NewText("背景"))
	sh := newShell(host)
	h := startApp(t, sh, 40, 10)
	h.onLoop(func() {
		host.Attach(f)
		f.Show()
	})
	h.barrier(sh)
	h.settle()

	// The list must own focus, so its keys work: j moves the cursor.
	h.inject(key('j'))
	h.barrier(sh)
	var idx int
	var ok bool
	h.onLoop(func() { idx, ok = body.list.Selected() })
	if !ok || idx != 1 {
		t.Fatalf("modal focus never reached the late-mounted list: cursor=%d ok=%v", idx, ok)
	}
}

// A float can size itself as a fraction of the screen — what a working
// surface (history browser, log viewer) actually wants, and what a fixed
// column count cannot express across terminal sizes.
func TestFloatSizeFraction(t *testing.T) {
	body := widget.NewBox(widget.NewText("content"))
	f := widget.NewFloat(body, widget.WithModal(true), widget.WithSizeFraction(90, 50))
	host := widget.NewOverlayHost(widget.NewText("背景"))
	sh := newShell(host)
	h := startApp(t, sh, 40, 20)
	h.onLoop(func() {
		host.Attach(f)
		f.Show()
	})
	h.barrier(sh)
	h.settle()

	w, hgt := boxExtent(h.grid())
	if want := 40 * 90 / 100; w != want {
		t.Errorf("float width = %d, want %d (90%% of 40)", w, want)
	}
	if want := 20 * 50 / 100; hgt != want {
		t.Errorf("float height = %d, want %d (50%% of 20)", hgt, want)
	}
}

// boxExtent measures the drawn border box on the grid.
func boxExtent(grid string) (w, h int) {
	for _, line := range strings.Split(grid, "\n") {
		start := strings.IndexAny(line, "╭┌│╰└")
		end := strings.LastIndexAny(line, "╮┐│╯┘")
		if start < 0 || end <= start {
			continue
		}
		h++
		if got := len([]rune(line[start:end])) + 1; got > w {
			w = got
		}
	}
	return w, h
}
