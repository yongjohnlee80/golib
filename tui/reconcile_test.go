package tui

import (
	"errors"
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// byName keys a probe by its name.
func byName(c Component) any { return c.(*probe).name }

func names(c Container) []string {
	var out []string
	for ch := range c.Children() {
		out = append(out, ch.(*probe).name)
	}
	return out
}

// Reconcile keeps the mount of every held child whose key is in next (moved, not remounted: one
// Init, the same NodeID), adds the new ones, removes the rest, and leaves next's order; next's own
// value for a kept key is dropped.
func TestReconcileKeepsMatchingMountsAndFollowsNext(t *testing.T) {
	a, b, c := &probe{name: "a"}, &probe{name: "b"}, &probe{name: "c"}
	flex := NewFlex(Vertical)
	flex.Add(a, b, c)
	h := startApp(t, flex, 4, 4)
	var aID, cID NodeID
	h.onLoop(func() { aID, cID = a.ctx.ID(), c.ctx.ID() })

	d, aAgain := &probe{name: "d"}, &probe{name: "a"}
	h.onLoop(func() { Reconcile(flex, []Component{c, aAgain, d}, byName) })
	h.sync()
	h.onLoop(func() {
		if got := names(flex); !slices.Equal(got, []string{"c", "a", "d"}) {
			t.Errorf("children %q, want [c a d]", got)
		}
		held := slices.Collect(flex.Children())
		if held[1] != Component(a) {
			t.Error("next's own value for the key a replaced the held child")
		}
		if a.ctx.ID() != aID || c.ctx.ID() != cID {
			t.Error("a kept child has a new NodeID: it was remounted")
		}
	})
	if a.inits.Load() != 1 || c.inits.Load() != 1 {
		t.Errorf("Init ran %d and %d times for the kept a and c, want once each", a.inits.Load(), c.inits.Load())
	}
	if b.unmounts.Load() != 1 {
		t.Errorf("b, not in next, was unmounted %d times, want once", b.unmounts.Load())
	}
	if d.inits.Load() != 1 || aAgain.inits.Load() != 0 {
		t.Errorf("Init ran %d times for the new d (want 1) and %d for next's dropped a (want 0)", d.inits.Load(), aAgain.inits.Load())
	}
}

// A key repeated in next is refused before the container changes.
func TestReconcileRefusesARepeatedKey(t *testing.T) {
	a := &probe{name: "a"}
	flex := NewFlex(Vertical)
	flex.Add(a)
	func() {
		defer func() {
			err, _ := recover().(error)
			var fatal errs.Fatal
			if !errors.As(err, &fatal) {
				t.Errorf("recovered %v, want an errs.Fatal", err)
			}
		}()
		Reconcile(flex, []Component{&probe{name: "x"}, &probe{name: "x"}}, byName)
	}()
	if got := names(flex); !slices.Equal(got, []string{"a"}) {
		t.Errorf("children %q after a refused Reconcile, want [a]", got)
	}
}
