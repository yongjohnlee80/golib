package tui

import (
	"fmt"

	"github.com/yongjohnlee80/golib/errs"
)

// THE COMMIT PHASE.
//
// Layout is pure: it measures and places, and it does not publish, mount,
// unmount or store. That rule is what lets the runtime run a layout pass
// whenever it needs one — twice in a frame, or not at all — without a component
// observing the difference.
//
// But some things genuinely depend on geometry and genuinely have to happen: a
// split that publishes the ratio it actually reached, a wrapper storing the size
// it was clamped to, an overlay closing because the row it hung off is no longer
// laid out. Doing them in Layout is what an earlier design did, and it made the
// purity rule a comment rather than a rule.
//
// So the frame gains one ordered phase between the two pure ones:
//
//	… → layout (pure) → COMMIT → render (pure) → present
//
// Commit runs on the loop, after geometry is final and before anything paints.
// It is the ONLY legal place for a geometry-derived side effect.

// CommitKey identifies one commit record within its owner, so a component that
// registers on every pass replaces its own pending record rather than queuing a
// second one.
type CommitKey string

// commitRecord is one pending callback, bound to the node that registered it.
type commitRecord struct {
	owner NodeID
	key   CommitKey
	fn    func()
}

// maxCommitPasses bounds layout↔commit ping-pong within one frame.
//
// A commit callback may legitimately dirty layout — a size it stored changes
// what the next pass measures — so the two alternate until they settle. A cycle
// that never settles would otherwise hang the loop with no frame and no error,
// which is the worst failure shape available: silent. Exhaustion is loud.
const maxCommitPasses = 8

// AfterLayout registers fn to run in the commit phase that follows this layout
// pass.
//
// LEGAL ONLY INSIDE Layout. Geometry is not final anywhere else, so a
// registration from elsewhere would be scheduling against a frame that may never
// come, and the owner check below could not mean anything.
//
// Keyed by (owner, key): re-registering in the same pass REPLACES the pending
// record and keeps its original queue position. Re-registration is an update,
// not a reschedule — a component that lays out twice in one frame must not have
// its commit run twice.
//
// It is not a subscription. "Run once after this pass" is the whole contract;
// persistent behaviour is a component's own state.
func (c *Context) AfterLayout(key CommitKey, fn func()) {
	a := c.app
	// BOTH the phase and the CALLER'S IDENTITY. Checking only the phase let any
	// code running during somebody else's Layout register a record owned by a
	// node that is not laying out — a Context outlives the handler it was given
	// to, so a component holding a sibling's retained Context could register in
	// that sibling's name. The record then runs, or is discarded, according to
	// the WRONG node's lifetime: if the registrant disappears and the named
	// owner survives, a callback nobody can account for still fires.
	//
	// LayoutChild restores the parent's identity when a child's Layout returns,
	// so a parent may still register after laying its children out. This is the
	// same rule CapturePointer enforces for handlers, and for the same reason:
	// identity, not merely "some phase is running".
	if !a.inLayout || a.layingOut != c.node {
		panic(errs.Fatal{
			Op: "tui: Context.AfterLayout",
			Rule: "legal only inside this component's own Layout; geometry is not " +
				"final anywhere else, and a record must be owned by the node registering it",
		})
	}
	if fn == nil {
		return
	}
	for i := range a.commits {
		if a.commits[i].owner == c.node.id && a.commits[i].key == key {
			a.commits[i].fn = fn // same position, new closure
			return
		}
	}
	a.commits = append(a.commits, commitRecord{owner: c.node.id, key: key, fn: fn})
}

// runCommitPhase drains the records registered by the pass that just finished.
//
// FIFO, full stop. Registration order is what a single-threaded layout pass
// produces deterministically, and layout already visits in document order, so
// the two coincide without a second sort.
//
// A record whose owner is no longer mounted when its turn comes is DISCARDED
// UNRUN. Callbacks unmount things — that is one of the side effects commit
// exists for — so an earlier one can legitimately remove the owner of a later
// one, and running that later callback would call into a component the runtime
// has already told everyone is gone.
//
// The queue is taken and cleared before draining, so a callback that registers
// during the drain is not appended to the list being drained: its registration
// belongs to the pass it causes, not to this one.
func (a *App) runCommitPhase() {
	pending := a.commits
	a.commits = nil
	for _, rec := range pending {
		if n := a.nodes[rec.owner]; n == nil || !n.mounted {
			continue
		}
		rec.fn()
	}
}

// layoutAndCommit runs the layout↔commit cycle until it settles, then leaves
// geometry final for render.
//
// Alternating rather than one-shot, because a commit callback may dirty layout
// and the frame must not paint geometry that its own commit has already
// invalidated. Bounded, and exhaustion is a fatal rather than a dropped frame:
// a component that cannot settle in eight passes has a feedback loop, and
// silently rendering the eighth attempt would hide it forever.
func (a *App) layoutAndCommit() {
	for pass := 0; ; pass++ {
		if pass >= maxCommitPasses {
			panic(errs.Fatal{
				Op: "tui: layout/commit",
				Rule: fmt.Sprintf("geometry did not settle within %d passes; a commit "+
					"callback is dirtying layout every pass", maxCommitPasses),
			})
		}
		a.layoutTree()
		a.layoutDirty = false
		a.runCommitPhase()
		if !a.layoutDirty {
			return
		}
	}
}
