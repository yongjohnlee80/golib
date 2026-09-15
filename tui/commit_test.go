package tui

// The commit phase exists so that geometry-derived side effects have a legal
// home, and its whole value is in the lifecycle rules rather than in the
// mechanism. A queue that ran callbacks for unmounted owners, or queued the same
// one twice, or let a callback's own registration join the drain it is running
// in, would still "work" in the easy case and fail exactly where a component
// stopped being able to reason about it.

import (
	"strings"
	"testing"
)

// commitProbe registers a commit callback from its own Layout, which is the
// only phase AfterLayout accepts.
type commitProbe struct {
	probe
	key      CommitKey
	onCommit func()
	// perPass, when set, is called during Layout and may change what the probe
	// registers — the shape a component that re-registers every pass has.
	perPass func(p *commitProbe)
}

func newCommitProbe(name string, key CommitKey, onCommit func()) *commitProbe {
	return &commitProbe{probe: probe{name: name, pref: Size{W: 2, H: 1}}, key: key, onCommit: onCommit}
}

func (p *commitProbe) Layout(c Constraints) Size {
	if p.perPass != nil {
		p.perPass(p)
	}
	if p.onLayout != nil {
		p.onLayout(&p.probe, c)
	}
	if p.ctx != nil && p.onCommit != nil {
		p.ctx.AfterLayout(p.key, p.onCommit)
	}
	return c.Constrain(p.pref)
}

// TestCommitRunsAfterGeometryIsFinalAndBeforeAnyRender.
//
// The ordering IS the contract: a callback that ran before placement would see
// stale geometry, and one that ran after render would publish a size the frame
// on screen does not have.
func TestCommitRunsAfterGeometryIsFinalAndBeforeAnyRender(t *testing.T) {
	t.Parallel()
	var order []string
	var rectAtCommit Rect

	p := newCommitProbe("p", "k", nil)
	p.fill = "x"
	p.onCommit = func() {
		order = append(order, "commit")
		rectAtCommit = p.ctx.app.nodes[p.ctx.node.id].rect
	}
	p.onLayout = func(_ *probe, _ Constraints) Size {
		order = append(order, "layout")
		return Size{} // the probe's own Layout returns; this hook only records
	}
	root := NewFlex(Vertical)
	root.Add(p)
	h := startApp(t, root, 8, 8)
	h.sync()

	var got []string
	var r Rect
	h.onLoop(func() { got = append(got, order...); r = rectAtCommit })

	if len(got) < 2 || got[0] != "layout" || got[1] != "commit" {
		t.Errorf("order = %v, want layout before commit", got)
	}
	if r.Empty() {
		t.Error("the commit callback saw an empty rect; geometry must be final by then")
	}
}

// TestReRegisteringReplacesTheRecordAndKeepsItsPlace.
//
// A component that lays out twice in one frame must not commit twice — and the
// replacement must keep its original queue position, because re-registration is
// an update, not a reschedule. Moving it would silently reorder side effects
// that a composition may depend on.
func TestReRegisteringReplacesTheRecordAndKeepsItsPlace(t *testing.T) {
	t.Parallel()
	var fired []string

	first := newCommitProbe("first", "k", nil)
	second := newCommitProbe("second", "k", nil)
	// first registers TWICE per pass under one key, with different closures.
	first.perPass = func(p *commitProbe) {
		if p.ctx != nil {
			p.ctx.AfterLayout("k", func() { fired = append(fired, "first-stale") })
		}
	}
	first.onCommit = func() { fired = append(fired, "first") }
	second.onCommit = func() { fired = append(fired, "second") }

	root := NewFlex(Vertical)
	root.Add(first, second)
	h := startApp(t, root, 8, 8)
	h.sync()

	var got []string
	h.onLoop(func() { got = append(got, fired...) })

	if len(got) != 2 {
		t.Fatalf("fired = %v, want exactly 2 — one per owner", got)
	}
	if got[0] != "first" {
		t.Errorf("fired = %v; the replacement lost its original position, or the "+
			"stale closure ran", got)
	}
	if got[1] != "second" {
		t.Errorf("fired = %v, want the second owner last", got)
	}
}

// TestARecordWhoseOwnerHasGoneIsDiscardedUnrun.
//
// Callbacks unmount things — that is one of the side effects commit exists for —
// so an earlier one can legitimately remove the owner of a later one. Running
// that later callback would call into a component the runtime has already told
// everyone is gone.
func TestARecordWhoseOwnerHasGoneIsDiscardedUnrun(t *testing.T) {
	t.Parallel()
	var fired []string

	root := NewFlex(Vertical)
	victim := newCommitProbe("victim", "k", nil)
	victim.onCommit = func() { fired = append(fired, "victim") }
	killer := newCommitProbe("killer", "k", nil)
	killer.onCommit = func() {
		fired = append(fired, "killer")
		root.Remove(victim) // registered earlier in document order, runs first
	}
	root.Add(killer, victim)
	h := startApp(t, root, 8, 8)
	h.sync()
	h.sync()

	var got []string
	h.onLoop(func() { got = append(got, fired...) })
	for _, name := range got {
		if name == "victim" {
			t.Errorf("fired = %v; a record ran for an owner unmounted earlier in the "+
				"same drain", got)
		}
	}
	if len(got) == 0 || got[0] != "killer" {
		t.Errorf("fired = %v, want the surviving record to have run", got)
	}
}

// TestACallbackThatDirtiesLayoutGetsAnotherPass.
//
// A commit that changes what the next pass measures must not be painted against
// the geometry it invalidated. The pair alternates until it settles.
func TestACallbackThatDirtiesLayoutGetsAnotherPass(t *testing.T) {
	t.Parallel()
	var layouts, commits int

	p := newCommitProbe("p", "k", nil)
	p.perPass = func(pp *commitProbe) { layouts++ }
	p.onCommit = func() {
		commits++
		if commits == 1 {
			p.pref = Size{W: 4, H: 2} // a different answer next pass
			p.ctx.RequestLayout()
		}
	}
	root := NewFlex(Vertical)
	root.Add(p)
	h := startApp(t, root, 8, 8)
	h.sync()

	var gotL, gotC int
	var size Size
	h.onLoop(func() {
		gotL, gotC = layouts, commits
		size = h.app.nodes[p.ctx.node.id].size
	})
	if gotL < 2 || gotC < 2 {
		t.Errorf("%d layouts and %d commits; a commit that dirtied layout must "+
			"produce another pass before render", gotL, gotC)
	}
	// The MAIN axis is the one a vertical Flex leaves to the child; it stretches
	// the cross axis, so asserting width here would be asserting Flex's policy
	// rather than the relayout.
	if size.H != 2 {
		t.Errorf("the painted size is %+v; the frame rendered geometry its own "+
			"commit had invalidated", size)
	}
}

// TestAnEndlessLayoutCommitCycleFailsLoudly.
//
// A component that never settles would otherwise hang the loop with no frame and
// no error, which is the worst failure shape available. The bound turns it into
// a message naming the cause.
func TestAnEndlessLayoutCommitCycleFailsLoudly(t *testing.T) {
	t.Parallel()
	p := newCommitProbe("p", "k", nil)
	n := 0
	p.onCommit = func() {
		n++
		p.pref = Size{W: 1 + n%3, H: 1} // never the same answer twice running
		p.ctx.RequestLayout()
	}
	root := NewFlex(Vertical)
	root.Add(p)

	tb := NewTestBackend(8, 8)
	app := NewApp(root, WithBackend(tb), WithMinFrameInterval(0))
	var rec any
	func() {
		defer func() { rec = recover() }()
		// Run the frame directly: the panic is the point, and a background Run
		// would take the harness down with it.
		app.rootNode = app.mount(nil, root)
		app.size = Size{W: 8, H: 8}
		app.buf = newBuffer(8, 8)
		app.layoutDirty = true
		app.renderFrame()
	}()
	if rec == nil {
		t.Fatal("an endless layout/commit cycle did not fail")
	}
	if msg := errText(rec); !strings.Contains(msg, "settle") {
		t.Errorf("the failure says %q, which does not name the cause", msg)
	}
}

// errText renders a recovered value for an assertion about its message.
func errText(v any) string {
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return ""
}

// TestAfterLayoutIsRefusedOutsideLayout.
//
// Geometry is not final anywhere else, so a registration from elsewhere would be
// scheduling against a frame that may never come — and the owner check that
// makes the queue safe could not mean anything.
func TestAfterLayoutIsRefusedOutsideLayout(t *testing.T) {
	t.Parallel()
	p := newFocusProbe("p", Size{W: 2, H: 1})
	root := NewFlex(Vertical)
	root.Add(p)
	h := startApp(t, root, 8, 8)

	var rec any
	h.onLoop(func() {
		defer func() { rec = recover() }()
		p.ctx.AfterLayout("k", func() {})
	})
	if rec == nil {
		t.Error("AfterLayout was accepted outside Layout")
	}
}

// TestLayoutAndCommitSettleInsideONEFrame.
//
// The alternation is a WITHIN-FRAME contract: a commit that changes what the
// next pass measures must be re-laid-out before anything paints, so the frame
// on screen is never geometry its own commit has already invalidated.
//
// Counting layouts and commits across an app's frames cannot see that — a
// second frame produces a second pair either way, so the count is the same
// whether the runtime settled the geometry or merely re-ran later. This drives
// ONE renderFrame directly, which is the only way to ask "did it settle before
// it painted?" rather than "did it settle eventually?".
func TestLayoutAndCommitSettleInsideONEFrame(t *testing.T) {
	t.Parallel()
	var layouts, commits int
	p := newCommitProbe("p", "k", nil)
	p.perPass = func(*commitProbe) { layouts++ }
	p.onCommit = func() {
		commits++
		if commits == 1 {
			p.pref = Size{W: 4, H: 2} // a different answer next pass
			p.ctx.RequestLayout()
		}
	}
	root := NewFlex(Vertical)
	root.Add(p)

	tb := NewTestBackend(8, 8)
	app := NewApp(root, WithBackend(tb), WithMinFrameInterval(0))
	app.rootNode = app.mount(nil, root)
	app.size = Size{W: 8, H: 8}
	app.buf = newBuffer(8, 8)
	app.layoutDirty = true

	app.renderFrame() // exactly one frame

	if layouts != 2 || commits != 2 {
		t.Errorf("%d layouts and %d commits in ONE frame, want 2 and 2; the commit "+
			"dirtied layout, so the frame owed another pass before painting",
			layouts, commits)
	}
	if got := app.nodes[p.ctx.node.id].size; got.H != 2 {
		t.Errorf("the painted size is %+v; the frame rendered geometry its own commit "+
			"had already invalidated", got)
	}
	if !app.layoutDirtyIsClear() {
		t.Error("the frame ended with layout still dirty, so it did not settle")
	}
}

// layoutDirtyIsClear reports whether the frame settled, for the test above. A
// method rather than a direct field read so the assertion reads as the question
// it is asking.
func (a *App) layoutDirtyIsClear() bool { return !a.layoutDirty }

// TestACommitRunsONCEPerRegistration.
//
// The drain takes the queue and clears it before running anything. Without that
// clear, the records would still be there on the next frame and every callback
// would run again — a split would publish its ratio once per frame for the rest
// of the application's life, and the event would stop meaning "this changed".
//
// The probe registers only on its FIRST layout, so a second frame that
// re-registers nothing must produce no second call.
func TestACommitRunsONCEPerRegistration(t *testing.T) {
	t.Parallel()
	var commits int
	p := newCommitProbe("p", "k", func() { commits++ })
	first := true
	p.perPass = func(pp *commitProbe) {
		if !first {
			pp.onCommit = nil // register nothing from here on
		}
		first = false
	}
	root := NewFlex(Vertical)
	root.Add(p)

	tb := NewTestBackend(8, 8)
	app := NewApp(root, WithBackend(tb), WithMinFrameInterval(0))
	app.rootNode = app.mount(nil, root)
	app.size = Size{W: 8, H: 8}
	app.buf = newBuffer(8, 8)

	app.layoutDirty = true
	app.renderFrame()
	if commits != 1 {
		t.Fatalf("%d commits after the frame that registered one, want 1", commits)
	}

	// A second frame, registering nothing.
	app.layoutDirty = true
	app.renderFrame()
	if commits != 1 {
		t.Errorf("%d commits after a frame that registered none, want still 1; the "+
			"drain left its records in the queue", commits)
	}
}

// TestAfterLayoutRejectsASiblingsRetainedContext.
//
// A Context outlives the call it was handed to, so a component can hold a
// sibling's. Checking only "some Layout is running" let such a holder register
// a commit record OWNED BY THE SIBLING: the record then lives or dies by the
// wrong node's lifetime, and if the registrant is unmounted while the named
// owner survives, a callback nobody can account for still runs against final
// geometry.
//
// The positive control is in the same test: the node's OWN context is accepted
// from its own Layout, so the rejection below is about identity rather than the
// check being broken.
func TestAfterLayoutRejectsASiblingsRetainedContext(t *testing.T) {
	t.Parallel()
	var selfOK, siblingRejected bool

	victim := newCommitProbe("victim", "k", func() {})
	thief := newCommitProbe("thief", "k", nil)
	thief.perPass = func(p *commitProbe) {
		if p.ctx == nil || victim.ctx == nil {
			return
		}
		// Its OWN context, from its own Layout: legal.
		p.ctx.AfterLayout("mine", func() {})
		selfOK = true
		// The SIBLING's retained context, from this node's Layout: refused.
		func() {
			defer func() {
				if recover() != nil {
					siblingRejected = true
				}
			}()
			victim.ctx.AfterLayout("forged", func() {})
		}()
	}

	root := NewFlex(Vertical)
	root.Add(victim, thief)
	h := startApp(t, root, 8, 8)
	h.sync()

	var gotSelf, gotSibling bool
	h.onLoop(func() { gotSelf, gotSibling = selfOK, siblingRejected })
	if !gotSelf {
		t.Fatal("a node could not register with its own context from its own Layout, " +
			"so the rejection below proves nothing")
	}
	if !gotSibling {
		t.Error("registering through a SIBLING's retained context was accepted; a " +
			"commit record must be owned by the node that registers it")
	}
}
