package widget

import (
	"fmt"
	"slices"
)

// THE MODAL STACK on an OverlayHost: opening and dismissing dialogs in stack order, the one scrim
// under the top dialog, and the top dialog itself. The generic layer host is overlay.go.

// openModal mounts a dialog as the topmost layer, with its scrim beneath it if
// it asked for one.
//
// It reports an error rather than panicking on a failed mount, so Open can roll
// back and leave no trace. A descendant that is already mounted elsewhere is the
// reachable case: the runtime refuses it, and without the rollback the host was
// left holding a dialog it had half-added.
func (h *OverlayHost) openModal(m *Modal) (err error) {
	// Exactly one scrim exists at a time, and it belongs to whichever dialog is
	// on top. The one below is now covered, so its backdrop is removed before
	// the new pair goes on.
	hadScrim := h.scrim
	h.dropScrim()

	// Same preflight as the anchored path: a Modal already mounted elsewhere
	// must be refused before the add, because the unwind below removes the
	// layer, and removing an already-mounted component unmounts it from where
	// it legitimately lives.
	if h.ctx != nil && h.ctx.MountedComponent(m) {
		return fmt.Errorf("%w: the dialog is already mounted elsewhere", ErrModalNotMountable)
	}

	var scrim *Scrim
	if m.wantsScrim() {
		scrim = &Scrim{st: m.scrimStyle()}
		h.Stack.Add(scrim)
		h.scrim = scrim
	}

	// The mount is the step that can fail, and it fails by PANICKING from deep
	// inside the runtime — a descendant already mounted elsewhere is refused
	// there, not here, because only the runtime knows what is mounted. Recovering
	// is not a way of ignoring that: the failure is converted and returned. It is
	// the only way to get back the chance to undo what did happen.
	//
	// A failed mount is PARTIAL, which is what makes the unwind necessary rather
	// than cosmetic. The Modal node and its card are created before the offending
	// descendant is reached, so simply dropping the stack entry would leave a
	// mounted card whose body never mounted, and the next layout pass would panic
	// measuring a child that is not there. Removing the layer runs the ordinary
	// unmount cascade over whatever did mount, which is the rollback.
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		h.Stack.Remove(m) // unmounts the partial subtree, if any
		if scrim != nil {
			h.Stack.Remove(scrim)
		}
		h.scrim = nil
		if hadScrim != nil {
			// Whoever was topmost before keeps their backdrop: a refused open
			// must leave the stack exactly as it found it.
			h.restoreScrimForTop()
		}
		err = fmt.Errorf("%w: %v", ErrModalNotMountable, r)
	}()
	h.Stack.Add(m)
	h.modals = append(h.modals, m)
	h.raiseTopmost()
	return nil
}

// dismissModal performs one dismissal as a stack transition, unwinding anything
// above the target first.
//
// LIFO IS THE WHOLE POINT. Dialogs are a stack, and closing one in the middle of
// it while leaving the ones above open produces a state the model does not have:
// a dialog on top of nothing, still trapping focus, whose own dismissal will
// later try to unwind a modal that is already gone. So a non-top dismissal
// closes every dialog above the target first, topmost first, and each of those
// is a real dismissal with its own event — a listener counting closures counts
// all of them, not just the one that was asked for.
//
// The ones above close as DismissReplaced: they were not what the user acted on,
// and reporting the caller's reason for them would attribute an accept or a
// cancel to a dialog nobody answered.
func (h *OverlayHost) dismissModal(m *Modal, reason DismissReason) {
	// Topmost first, so each close sees a stack that is valid beneath it.
	//
	// The target's position is RE-DERIVED every iteration rather than captured
	// once, because each close runs a callback and a callback may dismiss another
	// dialog on this same stack. Indexing by a position taken before that ran
	// walked off the end of a slice the callback had already shortened.
	//
	// A target the host does not hold yields -1 and unwinds nothing, which is the
	// right answer rather than a special case: there is no transition to make on
	// a stack this dialog is not part of, and its own close below still settles
	// its flags. The counter bounds the loop so that no bookkeeping slip can turn
	// a re-entrant dismissal into a hang.
	for n := len(h.modals); n > 0; n-- {
		i := slices.Index(h.modals, m)
		if i < 0 || i == len(h.modals)-1 {
			break
		}
		above := h.modals[len(h.modals)-1]
		above.finishDismiss(DismissReplaced, func() { h.removeModal(above) })
	}
	m.finishDismiss(reason, func() { h.removeModal(m) })
}

// removeModal takes one dialog off the stack and hands the backdrop back to
// whoever is left on top.
func (h *OverlayHost) removeModal(m *Modal) {
	h.dropScrim()
	h.Stack.Remove(m)
	if i := slices.Index(h.modals, m); i >= 0 {
		h.modals = slices.Delete(h.modals, i, i+1)
	}
	h.restoreScrimForTop()
}

// restoreScrimForTop gives the topmost remaining dialog its backdrop, placed
// immediately beneath it.
//
// THE SURVIVING DIALOG IS MOVED, NEVER REMOUNTED. Inserting a layer below an
// existing one by removing and re-adding that one is not a reorder — it is a
// full unmount and remount, which gives the dialog a new NodeID, cancels its
// lifetime context, runs its unmount hooks, drops its focus and re-runs Init on
// everything inside it. All of that for a change the user cannot see, and none
// of it visible in a rendered-output test, because the screen looks identical
// either way. Stack.Move reorders and keeps the node.
func (h *OverlayHost) restoreScrimForTop() {
	n := len(h.modals)
	if n == 0 {
		return
	}
	top := h.modals[n-1]
	if !top.wantsScrim() {
		return
	}
	scrim := &Scrim{st: top.scrimStyle()}
	h.Stack.Add(scrim)
	h.scrim = scrim
	// Added on top, then moved to the slot directly below the surviving dialog: the dialog's own
	// index, which the move pushes it one above. The topmost floats may sit above the dialog, so
	// the dialog is not always the last layer.
	for i, layer := range h.Stack.All() {
		if layer == top {
			h.Stack.Move(scrim, i)
			break
		}
	}
}

// restyleScrim repaints the backdrop when its owning dialog is restyled. Only
// the topmost dialog owns one, so a restyle of any other is a no-op here.
func (h *OverlayHost) restyleScrim(owner *Modal, st *ModalStyle) {
	if h.scrim == nil || len(h.modals) == 0 || h.modals[len(h.modals)-1] != owner {
		return
	}
	h.scrim.st = st
	if ctx := h.scrim.Context(); ctx != nil {
		ctx.MarkDirty()
	}
}

// dropScrim removes the current backdrop, if any.
func (h *OverlayHost) dropScrim() {
	if h.scrim == nil {
		return
	}
	h.Stack.Remove(h.scrim)
	h.scrim = nil
}

// TopModal reports the dialog currently on top, or nil when none is open.
func (h *OverlayHost) TopModal() *Modal {
	if n := len(h.modals); n > 0 {
		return h.modals[n-1]
	}
	return nil
}
