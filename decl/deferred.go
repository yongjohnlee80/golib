package decl

import (
	"errors"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// DEFERRED SIGNALS.
//
// A widget may raise a signal while the tree is mid-operation — mounting,
// reconciling, or fanning out a source change — because the operation itself
// changed the widget. Emit queues such a signal rather than running its
// handlers then, and the operation delivers the queue once it has committed.

// deferring reports whether the tree is in the middle of an operation during
// which no handler may run.
func (t *Tree) deferring() bool {
	switch t.ph {
	case phaseMounting, phaseReconciling, phasePropagating:
		return true
	}
	return false
}

// deferredSignal is one queued signal and the parameters it was raised with.
type deferredSignal struct {
	key  signalKey
	args []qml.SpecValue
}

// deferSignal queues a signal, COALESCING a repeat of one already waiting.
//
// A handler reads the tree's state when it runs, not the state at the moment
// the signal was raised, so two raises of one signal during one operation are
// one delivery: running the handler twice would repeat an effect against the
// same state. The delivery carries the LATEST parameters, for the same reason:
// they describe the state the handler will find.
func (t *Tree) deferSignal(k signalKey, args []qml.SpecValue) {
	for i, q := range t.deferred {
		if q.key == k {
			t.deferred[i].args = args
			return
		}
	}
	t.deferred = append(t.deferred, deferredSignal{key: k, args: args})
}

// settle delivers the signals deferred during an operation, once that
// operation has returned, and joins their errors with the operation's own.
//
// It is the ONE way an operation finishes, called by every public operation
// that can defer — Mount, Reconcile, SetSources — so none of them can forget
// to deliver what its widgets raised.
//
// Only the OUTERMOST operation delivers. An operation nested inside another
// leaves the queue for the outer one, which is still mid-operation. A handler
// that starts an operation of its own drains the queue at that operation's
// end — while its own emission is still on the active stack, so a feedback
// loop through a widget (a handler moving a source whose widget re-raises the
// same signal) is caught by the ordinary cycle detector rather than spinning.
func (t *Tree) settle(opErr error) error {
	if t.deferring() || len(t.deferred) == 0 {
		return opErr
	}
	var errs []error
	for len(t.deferred) > 0 {
		d := t.deferred[0]
		t.deferred = t.deferred[1:]
		// The node that raised it may be gone — removed by the very reconcile
		// that caused the raise. Its signal is then moot rather than an error.
		if _, live := t.nodes[d.key.node]; !live {
			continue
		}
		if err := t.Emit(d.key.node, d.key.signal, d.args...); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(append([]error{opErr}, errs...)...)
}
