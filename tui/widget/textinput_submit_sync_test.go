package widget_test

// Enter's SYNCHRONOUS hook, and why it had to exist.
//
// A form that advances focus on Enter can only do it from a hook that runs
// before the next keystroke is dispatched. Doing it from SubmitEvent cannot:
// Bus.Publish queues delivery onto the program lane, the App selects between
// the input lane and the program lane, and when a keystroke is already waiting
// Go picks between the two pseudo-randomly. Half the time the keystroke wins
// and lands in the field the operator has just left.
//
// The consumer that found it saw a connection form take "demo" Enter "sqlite"
// and produce a name of "demos" with an engine of "qlite" -- intermittently, so
// it passed locally and failed in CI at the same commit.

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

var errRejected = errors.New("rejected by validation (test)")

// twoFieldForm is the smallest thing that can exhibit the race: two inputs
// where Enter on the first is supposed to hand the second the next character.
type twoFieldForm struct {
	flex   *tui.Flex
	first  *widget.TextInput
	second *widget.TextInput
	ctx    *tui.Context
}

func (f *twoFieldForm) Init(ctx *tui.Context) {
	f.ctx = ctx
	ctx.Mount(f.flex)
}
func (f *twoFieldForm) Layout(c tui.Constraints) tui.Size {
	sz := f.ctx.LayoutChild(f.flex, c)
	f.ctx.PlaceChild(f.flex, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
	return sz
}
func (f *twoFieldForm) Render(tui.Surface)         {}
func (f *twoFieldForm) HandleEvent(tui.Event) bool { return false }

// ENTER HANDS THE NEXT CHARACTER TO THE NEXT FIELD.
//
// The keystroke is injected in the SAME batch as the Enter, which is what a
// paste is and what a fast typist is close enough to. With the advance driven
// from SubmitEvent this fails about half the time; from the synchronous hook it
// cannot fail, because the focus move completes inside the key handler.
//
// Repeated, because a race that is reported once is a race that has been
// observed once: 40 rounds with the queued path is a vanishing chance of all
// passing, and this cell reddens if the hook is dropped.
func TestTextInputOnSubmitRunsBeforeTheNextKey(t *testing.T) {
	for round := 0; round < 40; round++ {
		form := &twoFieldForm{}
		form.second = widget.NewTextInput()
		form.first = widget.NewTextInput(widget.WithOnSubmit(func(string) {
			form.ctx.FocusComponent(form.second)
		}))
		form.flex = tui.NewFlex(tui.Vertical)
		form.flex.Add(form.first)
		form.flex.Add(form.second)

		sh := newShell(form)
		h := startApp(t, sh, 20, 4)
		h.inject(tab()) // focus the first field
		h.barrier(sh)

		// "ab", Enter, "c" — all in one batch, no pause anywhere.
		evs := append(typeString("ab"), key(tui.KeyEnter))
		evs = append(evs, typeString("c")...)
		h.inject(evs...)
		h.barrier(sh)

		var got1, got2 string
		h.onLoop(func() { got1 = form.first.Value(); got2 = form.second.Value() })
		if got1 != "ab" {
			t.Fatalf("round %d: the first field holds %q, want \"ab\" — the character typed "+
				"after Enter landed in the field the operator had left", round, got1)
		}
		if got2 != "c" {
			t.Fatalf("round %d: the second field holds %q, want \"c\" — focus had not moved "+
				"when the next key was dispatched", round, got2)
		}
	}
}

// AND THE EVENT IS STILL PUBLISHED. The hook is additive: a subscriber that
// existed before this option must see exactly what it saw.
func TestTextInputOnSubmitDoesNotReplaceTheEvent(t *testing.T) {
	var hookCalls int
	h, in, sh := focusedInput(t, widget.WithOnSubmit(func(string) { hookCalls++ }))
	submits := record[widget.SubmitEvent](h)

	h.inject(typeString("hi")...)
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)

	if hookCalls != 1 {
		t.Errorf("the hook ran %d times, want 1", hookCalls)
	}
	var id tui.NodeID
	h.onLoop(func() { id = in.NodeID() })
	if ev, ok := submits.last(); !ok || ev.Owner != id || ev.Value != "hi" {
		t.Errorf("SubmitEvent = %+v, want owner %d value %q — the hook must not have "+
			"replaced it", ev, id, "hi")
	}
}

// A FAILING VALIDATION REACHES NEITHER. Enter that does not submit must not
// advance a form, or the operator is moved off the field carrying the error
// they have to fix.
func TestTextInputOnSubmitSkippedWhenValidationFails(t *testing.T) {
	var hookCalls int
	h, _, sh := focusedInput(t,
		widget.WithValidate(func(string) error { return errRejected }),
		widget.WithOnSubmit(func(string) { hookCalls++ }))
	submits := record[widget.SubmitEvent](h)

	h.inject(typeString("x")...)
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)

	if hookCalls != 0 {
		t.Errorf("the hook ran %d times on a failing validation, want 0", hookCalls)
	}
	if _, ok := submits.last(); ok {
		t.Error("a failing validation published a SubmitEvent")
	}
}
