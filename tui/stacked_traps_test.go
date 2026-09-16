package tui

// STACKED TRAPS: a dialog opened over a dialog.
//
// golib handled two shapes of focus trap and assumed there were only two. A
// NESTED trap passes the confinement check on ancestry, because its node lies
// inside the outer scope. An UNRELATED trap never collides, because only one is
// live. The shape it had no answer for is the one every overlay host produces:
// each layer's trap is mounted inside its own Float, and the Floats are layers
// of one Stack, so two traps are COUSINS and neither is an ancestor of the
// other. Ancestry could only refuse them, and the refusal was a deadlock — the
// new dialog could not be focused because it was not on the scope stack, and
// could not reach the stack because nothing inside it could be focused.
//
// Measured downstream before it was understood here: autodb opens a connection
// form over its connection manager and the form paints and is then inert. Its
// runtime trace says "focus refused: target outside the active focus scope",
// once per text input.
//
// THE FIXTURE IS A REAL STACK, and its geometry is ASSERTED. An earlier version
// of this file built the same shape out of a test container that lays children
// out LEFT TO RIGHT, so the "upper" trap sat at X=20 on a 20-column terminal —
// off screen, beside rather than above. Every cell passed, and passed for the
// wrong reason: not because a layer in front may be entered, but because the
// node happened to come later in document order, which is exactly the weakness
// these tests are supposed to rule out. A fixture that does not stack cannot
// test stacking, so newStackedFixture now checks the rects it built.

import "testing"

// stackFixture is the overlay-host shape, on the container that actually
// declares its children to be layers:
//
//	root
//	└── Stack  (FocusLayerHost)
//	    ├── base   (no trap)        → inBase
//	    ├── lower  (traps)          → inLower
//	    ├── middle (traps)          → inMiddle
//	    └── upper  (traps)          → inUpper
//
// THREE trapping layers, not two, because "in front of the active one" and "the
// topmost one" only differ when there is a layer between them.
//
// All three occupy the same rect, because that is what layers do, and the test
// asserts it rather than trusting it.
type stackFixture struct {
	h                          *harness
	stack                      *Stack
	base, lower, middle, upper *trapScope
	inBase                     *focusableCounter
	inLower, inMiddle, inUpper *focusableCounter
	id                         map[any]NodeID
}

// plainScope is a layer that does NOT trap: the base layer of a stack, and the
// control proving a refusal is about trapping rather than about layering.
type plainScope struct{ trapScope }

func (p *plainScope) TrapsFocus() bool { return false }

func newStackFixture(t *testing.T) *stackFixture {
	t.Helper()
	f := &stackFixture{
		stack:    NewStack(),
		lower:    &trapScope{size: Size{W: 20, H: 4}},
		middle:   &trapScope{size: Size{W: 20, H: 4}},
		upper:    &trapScope{size: Size{W: 20, H: 4}},
		inLower:  &focusableCounter{},
		inMiddle: &focusableCounter{},
		inUpper:  &focusableCounter{},
		inBase:   &focusableCounter{},
		id:       map[any]NodeID{},
	}
	base := &plainScope{trapScope{size: Size{W: 20, H: 4}}}
	f.base = &base.trapScope
	base.Add(f.inBase)
	f.lower.Add(f.inLower)
	f.middle.Add(f.inMiddle)
	f.upper.Add(f.inUpper)
	f.stack.Add(base, f.lower, f.middle, f.upper)

	f.h = startApp(t, f.stack, 20, 4)
	f.h.sync()
	f.h.onLoop(func() {
		a := f.h.app
		for _, c := range []Component{base, f.lower, f.middle, f.upper,
			f.inBase, f.inLower, f.inMiddle, f.inUpper} {
			f.id[c] = a.byComp[c].id
		}
		f.id[f.base] = a.byComp[base].id
	})

	// THE ASSERTION THE OLD FIXTURE LACKED: the layers overlap. Without it a
	// container that placed them side by side would still satisfy every cell
	// below, by document order alone.
	var rl, ru Rect
	f.h.onLoop(func() {
		rl = f.h.app.nodes[f.id[f.lower]].rect
		ru = f.h.app.nodes[f.id[f.upper]].rect
	})
	if rl != ru {
		t.Fatalf("the fixture does not stack: lower=%+v upper=%+v — these cells "+
			"would then be testing document order, not layering", rl, ru)
	}
	if rl.W == 0 || rl.H == 0 {
		t.Fatalf("the fixture laid out to nothing: %+v", rl)
	}
	return f
}

func (f *stackFixture) focus(c Component) NodeID {
	var got NodeID
	f.h.onLoop(func() {
		f.h.app.requestFocusByID(f.id[c])
		got = f.h.app.focused
	})
	return got
}

// TestALayerInFrontCanTakeFocusFromTheOneBehind is the defect.
func TestALayerInFrontCanTakeFocusFromTheOneBehind(t *testing.T) {
	f := newStackFixture(t)
	defer f.h.wait()

	if got := f.focus(f.inLower); got != f.id[f.inLower] {
		t.Fatalf("precondition failed: focus %v, want the lower layer %v",
			got, f.id[f.inLower])
	}
	f.h.onLoop(func() {
		if f.h.app.confinement() == nil {
			t.Fatal("precondition failed: the lower trap is not governing, so " +
				"the refusal under test could not occur either way")
		}
	})

	if got := f.focus(f.inUpper); got != f.id[f.inUpper] {
		t.Errorf("focus %v after requesting the layer in front, want %v — a "+
			"dialog opened over another cannot be typed into", got, f.id[f.inUpper])
	}
}

// TestALayerBehindCannotStealFocusBack is the half that must not regress.
//
// Without it, "stacked traps work" is indistinguishable from having deleted the
// confinement check.
func TestALayerBehindCannotStealFocusBack(t *testing.T) {
	f := newStackFixture(t)
	defer f.h.wait()

	f.focus(f.inLower)
	if got := f.focus(f.inUpper); got != f.id[f.inUpper] {
		t.Fatalf("precondition failed: could not reach the layer in front (%v)", got)
	}
	if got := f.focus(f.inLower); got != f.id[f.inUpper] {
		t.Errorf("focus moved to %v: the layer BEHIND pulled the keyboard out of "+
			"the one in front; want it held at %v", got, f.id[f.inUpper])
	}
}

// TestOnlyTheTopmostLayerMayBeEntered.
//
// "In front of the active one" is NOT the rule; "the one the user is looking
// at" is. With three trapping layers open and focus in the bottom one, the
// middle layer is genuinely in front of it — and must still be refused, because
// something is above the middle. Otherwise a dialog two deep could take the
// keyboard from the dialog actually on top.
//
// This is the case that distinguishes the ordering check from the topmost scan.
// An earlier version of this cell moved focus to a layer BEHIND the active one,
// which the ordering check alone already refuses, so it passed without the scan
// existing at all.
func TestOnlyTheTopmostLayerMayBeEntered(t *testing.T) {
	f := newStackFixture(t)
	defer f.h.wait()

	if got := f.focus(f.inLower); got != f.id[f.inLower] {
		t.Fatalf("precondition failed: focus %v, want the bottom layer %v",
			got, f.id[f.inLower])
	}

	// In front of the active layer, but not in front of everything.
	if got := f.focus(f.inMiddle); got != f.id[f.inLower] {
		t.Errorf("focus reached the MIDDLE layer (%v) while a layer above it was "+
			"still open; only the topmost may be entered, want focus held at %v",
			got, f.id[f.inLower])
	}

	// THE CONTROL: the topmost layer IS reachable from the same state, so the
	// refusal above is about what sits above the middle layer and not about
	// stacked entry being off.
	if got := f.focus(f.inUpper); got != f.id[f.inUpper] {
		t.Errorf("the topmost layer could not be entered (%v), want %v",
			got, f.id[f.inUpper])
	}
}

// TestAnInnerTrapCannotEscapeIntoTheDialogAroundIt.
//
// THE NESTED CASE, and this fixture is what it actually looks like: a trapping
// scope mounted inside a dialog's own content, a real descendant of it. While
// that inner scope is active the rest of the dialog is off limits, which is the
// point of it being a trap at all.
//
// Deliberately NOT a Select. The shipped dropdown projects its popup onto the
// overlay host, so it is a later LAYER of that host and takes the stacked route
// — it looks nested on screen and is not. Which route a composite takes is
// decided by where it mounts its popup, so the nested route needs a component
// that genuinely descends.
//
// The stacked rule must not become a way out: the dialog is the inner scope's
// own ANCESTOR, so there are no two branches to order, and the request is
// refused before layering is ever consulted.
func TestAnInnerTrapCannotEscapeIntoTheDialogAroundIt(t *testing.T) {
	stack := NewStack()
	outer := &trapScope{size: Size{W: 20, H: 4}}
	inner := &trapScope{size: Size{W: 20, H: 4}}
	inInner, inOuter := &focusableCounter{}, &focusableCounter{}
	inner.Add(inInner)
	// inOuter belongs to the dialog but sits OUTSIDE the dropdown.
	outer.Add(inner, inOuter)
	stack.Add(outer)

	h := startApp(t, stack, 20, 4)
	defer h.wait()
	h.sync()

	var idInner, idOuter NodeID
	h.onLoop(func() {
		idInner, idOuter = h.app.byComp[inInner].id, h.app.byComp[inOuter].id
	})
	focus := func(id NodeID) NodeID {
		var got NodeID
		h.onLoop(func() { h.app.requestFocusByID(id); got = h.app.focused })
		return got
	}

	if got := focus(idInner); got != idInner {
		t.Fatalf("precondition failed: focus %v, want inside the inner trap %v",
			got, idInner)
	}
	if got := focus(idOuter); got != idInner {
		t.Errorf("focus escaped the inner trap into the surrounding dialog (%v); "+
			"want it held at %v", got, idInner)
	}
}

// TestUnconfinedSpaceIsStillUnreachable is the original guarantee, asserted
// from the stacked state rather than the simple one.
//
// inBase sits on a layer that does not trap at all. It is the escape the guard
// was written to stop, and layering must not have opened a way to it.
func TestUnconfinedSpaceIsStillUnreachable(t *testing.T) {
	f := newStackFixture(t)
	defer f.h.wait()

	f.focus(f.inLower)
	if got := f.focus(f.inBase); got != f.id[f.inLower] {
		t.Errorf("focus escaped a live trap onto an untrapped layer (%v); want it "+
			"held at %v", got, f.id[f.inLower])
	}
}

// TestSideBySideTrapsCannotStealFromEachOther is the finding that killed the
// first implementation.
//
// Two trapping scopes in an ORDINARY container are not layers. Neither is in
// front, and neither may take the keyboard from the other — in either
// direction. The first version of this fix compared whole-tree document order,
// which made the one declared second able to steal focus from the one declared
// first, purely because of where it appeared in the source. Reproduced on a
// live runtime before it was corrected.
func TestSideBySideTrapsCannotStealFromEachOther(t *testing.T) {
	// counter lays its children out LEFT TO RIGHT and declares no layer
	// hosting, which is exactly the arrangement under test.
	root := &counter{size: Size{W: 20, H: 4}}
	left := &trapScope{size: Size{W: 10, H: 4}}
	right := &trapScope{size: Size{W: 10, H: 4}}
	inLeft, inRight := &focusableCounter{}, &focusableCounter{}
	left.Add(inLeft)
	right.Add(inRight)
	root.Add(left, right)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	var idLeft, idRight NodeID
	var rl, rr Rect
	h.onLoop(func() {
		idLeft, idRight = h.app.byComp[inLeft].id, h.app.byComp[inRight].id
		rl, rr = h.app.nodes[h.app.byComp[left].id].rect, h.app.nodes[h.app.byComp[right].id].rect
	})
	// Beside, not above: the premise of the whole cell.
	if rl == rr {
		t.Fatalf("precondition failed: the two traps overlap (%+v), so this is "+
			"the stacked case, not the side-by-side one", rl)
	}

	focus := func(id NodeID) NodeID {
		var got NodeID
		h.onLoop(func() { h.app.requestFocusByID(id); got = h.app.focused })
		return got
	}

	if got := focus(idLeft); got != idLeft {
		t.Fatalf("precondition failed: focus %v, want the left trap %v", got, idLeft)
	}
	// LATER IN DOCUMENT ORDER, and that must not be enough.
	if got := focus(idRight); got != idLeft {
		t.Errorf("the trap declared SECOND took focus from the one declared "+
			"first (focus %v, want %v). Neither is in front of the other; "+
			"declaration order is not layering", got, idLeft)
	}
	// And the other direction, so the refusal is not an artefact of order.
	if got := focus(idLeft); got != idLeft {
		t.Errorf("focus %v, want %v", got, idLeft)
	}
}

// TestTrapsOnSeparateHostsCannotReachEachOther.
//
// Two Stacks are two hosts. A layer of one is not in front of a layer of the
// other, however the tree happens to order them, because their common ancestor
// is an ordinary container that never claimed its children were layers.
func TestTrapsOnSeparateHostsCannotReachEachOther(t *testing.T) {
	// A FLEX, not the test counter, because a Stack takes whatever width it is
	// offered — two of them under a container that hands each the full screen
	// both claim it, and the second lands off-screen. Flex divides. It is also
	// the more faithful container for this cell: two hosts inside something that
	// is NOT a layer host is exactly the arrangement under test.
	build := func(t *testing.T) (*harness, NodeID, NodeID) {
		t.Helper()
		root := NewFlex(Horizontal)
		hostA, hostB := NewStack(), NewStack()
		trapA := &trapScope{size: Size{W: 20, H: 4}}
		trapB := &trapScope{size: Size{W: 20, H: 4}}
		inA, inB := &focusableCounter{}, &focusableCounter{}
		trapA.Add(inA)
		trapB.Add(inB)
		hostA.Add(trapA)
		hostB.Add(trapB)
		root.AddWeighted(hostA, 1)
		root.AddWeighted(hostB, 1)

		h := startApp(t, root, 40, 4)
		h.sync()

		var idA, idB NodeID
		var ra, rb Rect
		h.onLoop(func() {
			idA, idB = h.app.byComp[inA].id, h.app.byComp[inB].id
			ra = h.app.nodes[h.app.byComp[hostA].id].rect
			rb = h.app.nodes[h.app.byComp[hostB].id].rect
		})
		// Both hosts must be on screen and must not overlap, or "B cannot reach
		// A" would be satisfied by B never having been laid out, or by the two
		// being one stack after all.
		for name, r := range map[string]Rect{"hostA": ra, "hostB": rb} {
			if r.W <= 0 || r.H <= 0 || r.X < 0 || r.X+r.W > 40 {
				t.Fatalf("precondition failed: %s is not on screen (%+v)", name, r)
			}
		}
		if ra == rb {
			t.Fatalf("precondition failed: the two hosts occupy the same rect "+
				"(%+v), so this is the stacked case rather than two separate "+
				"hosts", ra)
		}
		return h, idA, idB
	}
	focus := func(h *harness, id NodeID) NodeID {
		var got NodeID
		h.onLoop(func() { h.app.requestFocusByID(id); got = h.app.focused })
		return got
	}

	// EACH DIRECTION GETS ITS OWN FIXTURE, because the second request in a
	// single fixture is answered by whichever trap is already active. Asserting
	// both directions against one active side is not two directions; it is the
	// same one twice, and an earlier version of this cell did exactly that.
	t.Run("B cannot reach A's host", func(t *testing.T) {
		h, idA, idB := build(t)
		defer h.wait()
		if got := focus(h, idA); got != idA {
			t.Fatalf("precondition failed: focus %v, want %v", got, idA)
		}
		if got := focus(h, idB); got != idA {
			t.Errorf("a trap on a DIFFERENT host took focus (%v); the two are not "+
				"layers of one stack, want focus held at %v", got, idA)
		}
	})

	t.Run("A cannot reach B's host", func(t *testing.T) {
		h, idA, idB := build(t)
		defer h.wait()
		// B active from the start, on a fixture where nothing has been focused
		// yet, so this is genuinely the opposite direction.
		if got := focus(h, idB); got != idB {
			t.Fatalf("precondition failed: focus %v, want %v", got, idB)
		}
		if got := focus(h, idA); got != idB {
			t.Errorf("the host declared FIRST took focus from the active one "+
				"(%v); want focus held at %v", got, idB)
		}
	})
}

func TestALayerWhoseTrapIsNestedInsideItStillCounts(t *testing.T) {
	stack := NewStack()
	lower := &trapScope{size: Size{W: 20, H: 4}}
	mid := &trapScope{size: Size{W: 20, H: 4}}
	// decor is a layer with nothing trapping anywhere inside it.
	decor := &plainScope{trapScope{size: Size{W: 20, H: 4}}}
	decor.Add(&focusableCounter{})
	// wrapper mirrors Float: not a trap itself, with the trap mounted inside.
	wrapper := &plainScope{trapScope{size: Size{W: 20, H: 4}}}
	deep := &trapScope{size: Size{W: 20, H: 4}}
	inDeep := &focusableCounter{}
	deep.Add(inDeep)
	wrapper.Add(deep)

	inLower, inMid := &focusableCounter{}, &focusableCounter{}
	lower.Add(inLower)
	mid.Add(inMid)
	stack.Add(lower, mid, decor, wrapper)

	h := startApp(t, stack, 20, 4)
	defer h.wait()
	h.sync()

	var idLower, idMid, idDeep NodeID
	h.onLoop(func() {
		idLower = h.app.byComp[inLower].id
		idMid = h.app.byComp[inMid].id
		idDeep = h.app.byComp[inDeep].id
	})
	focus := func(id NodeID) NodeID {
		var got NodeID
		h.onLoop(func() { h.app.requestFocusByID(id); got = h.app.focused })
		return got
	}

	if got := focus(idLower); got != idLower {
		t.Fatalf("precondition failed: focus %v, want %v", got, idLower)
	}
	// Above mid sit decor (no trap anywhere) and wrapper (trap one level down).
	// The wrapper must be found, or mid would be entered while a dialog is open
	// in front of it.
	if got := focus(idMid); got != idLower {
		t.Errorf("focus reached the middle layer (%v) although a layer above it "+
			"holds a trap NESTED inside it; the scan did not descend. Want %v",
			got, idLower)
	}
	// THE CONTROL: the nested trap itself is the topmost and is reachable, so
	// the refusal is about what is above the middle layer rather than about
	// nested traps being unreachable.
	if got := focus(idDeep); got != idDeep {
		t.Errorf("the topmost layer's nested trap could not be entered (%v), "+
			"want %v", got, idDeep)
	}
}

// TestNestedLayerHostsStillEnforceTopmost.
//
// LAYER HOSTS COMPOSE, and the first version of this rule only asked the host
// where the two paths happened to meet. Here the outer Stack holds the active
// trap and, after it, an inner Stack holding two more. Asked only at the outer
// host, the inner Stack is one opaque branch with nothing after it, so the
// middle trap looks topmost — while the top trap sits in front of it inside
// that very branch. The candidate has to be in front at EVERY level that
// stacks, not only where the paths meet.
func TestNestedLayerHostsStillEnforceTopmost(t *testing.T) {
	outer := NewStack()
	inner := NewStack()
	lower := &trapScope{size: Size{W: 20, H: 4}}
	middle := &trapScope{size: Size{W: 20, H: 4}}
	top := &trapScope{size: Size{W: 20, H: 4}}
	inLower, inMiddle, inTop := &focusableCounter{}, &focusableCounter{}, &focusableCounter{}
	lower.Add(inLower)
	middle.Add(inMiddle)
	top.Add(inTop)
	inner.Add(middle, top)
	outer.Add(lower, inner)

	h := startApp(t, outer, 20, 4)
	defer h.wait()
	h.sync()

	var idLower, idMiddle, idTop NodeID
	h.onLoop(func() {
		idLower = h.app.byComp[inLower].id
		idMiddle = h.app.byComp[inMiddle].id
		idTop = h.app.byComp[inTop].id
	})
	focus := func(id NodeID) NodeID {
		var got NodeID
		h.onLoop(func() { h.app.requestFocusByID(id); got = h.app.focused })
		return got
	}

	if got := focus(idLower); got != idLower {
		t.Fatalf("precondition failed: focus %v, want the outer host's first "+
			"layer %v", got, idLower)
	}
	if got := focus(idMiddle); got != idLower {
		t.Errorf("focus reached the middle trap (%v) although the inner host "+
			"stacks another trap in front of it; want focus held at %v",
			got, idLower)
	}
	// THE CONTROL: the genuinely topmost trap, two hosts deep, IS reachable —
	// so the refusal is about what is in front of the middle one and not about
	// nested layer hosts being unreachable.
	if got := focus(idTop); got != idTop {
		t.Errorf("the topmost trap inside the nested host could not be entered "+
			"(%v), want %v", got, idTop)
	}
}

// TestSideBySideRefusalHoldsInTheOtherDirectionToo.
//
// A fresh fixture with the RIGHT trap active from the start. The earlier cell
// asserted this direction by re-focusing the left trap after the right had been
// refused — which is a no-op, since focus was already there, and proved nothing.
// Refusal has to be symmetric or it is just an ordering preference.
func TestSideBySideRefusalHoldsInTheOtherDirectionToo(t *testing.T) {
	root := &counter{size: Size{W: 20, H: 4}}
	left := &trapScope{size: Size{W: 10, H: 4}}
	right := &trapScope{size: Size{W: 10, H: 4}}
	inLeft, inRight := &focusableCounter{}, &focusableCounter{}
	left.Add(inLeft)
	right.Add(inRight)
	root.Add(left, right)

	h := startApp(t, root, 20, 4)
	defer h.wait()
	h.sync()

	var idLeft, idRight NodeID
	var rl, rr Rect
	h.onLoop(func() {
		idLeft, idRight = h.app.byComp[inLeft].id, h.app.byComp[inRight].id
		rl = h.app.nodes[h.app.byComp[left].id].rect
		rr = h.app.nodes[h.app.byComp[right].id].rect
	})
	if rl == rr {
		t.Fatalf("precondition failed: the traps overlap (%+v); this is the "+
			"stacked case, not the side-by-side one", rl)
	}
	focus := func(id NodeID) NodeID {
		var got NodeID
		h.onLoop(func() { h.app.requestFocusByID(id); got = h.app.focused })
		return got
	}

	// RIGHT first this time — the one declared second.
	if got := focus(idRight); got != idRight {
		t.Fatalf("precondition failed: focus %v, want the right trap %v", got, idRight)
	}
	if got := focus(idLeft); got != idRight {
		t.Errorf("the trap declared FIRST took focus from the active one (%v); "+
			"neither is in front of the other, want focus held at %v", got, idRight)
	}
}

// TestTheLayerHelpersRefuseWhenTheirPremiseDoesNotHold.
//
// The three helpers behind the stacked rule are searches, and each has an
// answer for "not found" that the call sites cannot currently produce. They are
// pinned here anyway, because the DIRECTION of those answers is load-bearing: a
// childIndex that returned 0 instead of -1 for a non-child, or a topmostUpTo
// that fell through to true after missing its boundary, would GRANT focus
// across a boundary rather than refuse it. Refusing is the safe answer, and a
// future caller that breaks a precondition must inherit it.
func TestTheLayerHelpersRefuseWhenTheirPremiseDoesNotHold(t *testing.T) {
	stack := NewStack()
	inside := &trapScope{size: Size{W: 20, H: 4}}
	outside := &trapScope{size: Size{W: 20, H: 4}}
	inside.Add(&focusableCounter{})
	stack.Add(inside)

	// `outside` is deliberately NOT added to the stack: it is the node whose
	// premise does not hold.
	other := NewStack()
	other.Add(outside)
	root := NewFlex(Horizontal)
	root.AddWeighted(stack, 1)
	root.AddWeighted(other, 1)

	h := startApp(t, root, 40, 4)
	defer h.wait()
	h.sync()

	h.onLoop(func() {
		a := h.app
		nStack, nInside := a.byComp[stack], a.byComp[inside]
		nOther, nOutside := a.byComp[other], a.byComp[outside]

		// childIndex: a node that is not a child of the parent asked about.
		if got := childIndex(nStack, nOutside); got != -1 {
			t.Errorf("childIndex for a non-child = %d, want -1; any other answer "+
				"makes it look like a position in the layer order", got)
		}
		// And the positive control, so the above is about absence.
		if got := childIndex(nStack, nInside); got != 0 {
			t.Errorf("childIndex for the real child = %d, want 0", got)
		}

		// topmostUpTo: a boundary that is not an ancestor of the candidate. The
		// walk runs out of parents without meeting it.
		if a.topmostUpTo(nInside, nOther) {
			t.Error("topmostUpTo granted with a boundary that is not an ancestor " +
				"of the candidate; it must refuse rather than fall through")
		}
		// The control: the real boundary is reached and grants.
		if !a.topmostUpTo(nInside, nStack) {
			t.Error("topmostUpTo refused the only layer of its own host")
		}

		// lcaBranches: when one node IS the other's ancestor, the ancestor is
		// its own meeting point and has no branch on its side. That nil is what
		// makes mayEnterStackedTrap refuse the nested shape before it ever asks
		// about layering — the two are not side by side, so there is nothing to
		// order.
		lca, bx, by := lcaBranches(nInside, nStack)
		if lca != nStack {
			t.Errorf("lcaBranches(child, its ancestor) met at %v, want the ancestor", lca != nil)
		}
		if bx != nInside {
			t.Error("the descendant's own branch is not the descendant")
		}
		if by != nil {
			t.Error("the ancestor was given a branch on its own side; the nested " +
				"shape would then be ordered as if it were two layers")
		}
	})
}
