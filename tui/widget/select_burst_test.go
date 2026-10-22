package widget_test

import (
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Keys typed after the Enter that chooses an option reach where the closed options leave the focus,
// though no frame runs between them. The options close in the commit after a layout, and a key routed
// before it went to the options, which still trapped the focus: the Tab cycled inside them and the
// text after it was lost (AutoDB's TestAddingAConnection, its DSN typed into the name, on a starved
// loop). A frame cap of an hour keeps every frame out after the first.
func TestKeysAfterAChoiceReachTheFieldAfterTheSelect(t *testing.T) {
	sel := widget.NewSelect[string](widget.WithOptions(selectItems("alpha", "beta")))
	in := widget.NewTextInput()
	flex := tui.NewFlex(tui.Vertical)
	flex.Add(sel, in)
	sh := newShell(widget.NewOverlayHost(flex))
	h := startAppOpts(t, sh, 30, 10, tui.WithMinFrameInterval(time.Hour))
	h.inject(tab())
	h.barrier(sh)
	h.inject(key(tui.KeyEnter)) // open the options
	h.barrier(sh)
	h.inject(key(tui.KeyDown), key(tui.KeyEnter), tab(), key('z')) // beta, then on to the field
	h.barrier(sh)
	var val, typed string
	h.onLoop(func() { val, _ = sel.Value(); typed = in.Value() })
	if val != "BETA" || typed != "z" {
		t.Errorf("the Select holds %q and the field %q; want BETA and \"z\"", val, typed)
	}
}

// A press right after the Enter that chooses an option is hit-tested against the screen the choice
// leaves: the field under the closed options takes it, and the key after it. Before, the press
// reached the options' layer, still mounted until the next layout, over the field.
func TestAPressAfterAChoiceReachesTheFieldUnderTheOptions(t *testing.T) {
	sel := widget.NewSelect[string](widget.WithOptions(selectItems("alpha", "beta")))
	in := widget.NewTextInput()
	flex := tui.NewFlex(tui.Vertical)
	flex.Add(sel, in)
	sh := newShell(widget.NewOverlayHost(flex))
	h := startAppOpts(t, sh, 30, 10, tui.WithMinFrameInterval(time.Hour))
	h.inject(tab())
	h.barrier(sh)
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	h.inject(key(tui.KeyDown), key(tui.KeyEnter),
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 2, Y: 1},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 2, Y: 1},
		key('z'))
	h.barrier(sh)
	var val, typed string
	h.onLoop(func() { val, _ = sel.Value(); typed = in.Value() })
	if val != "BETA" || typed != "z" {
		t.Errorf("the Select holds %q and the field %q; want BETA and \"z\"", val, typed)
	}
}
