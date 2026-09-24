package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// deferred_test.go covers signals raised while the tree is mid-operation.
//
// The contract has three halves and each is asserted: the handler does NOT
// run during the operation, it DOES run after, and it runs ONCE however many
// times the signal was raised. A test of only the second would pass against the
// old engine that ran handlers mid-fan-out.

// TestASignalRaisedMidPropagationIsDeferred.
func TestASignalRaisedMidPropagationIsDeferred(t *testing.T) {
	rec := newReactor()
	var tr *decl.Tree
	during, raised := false, false
	var ranWhileMid []bool

	rec.handlers["save"] = func() error {
		ranWhileMid = append(ranWhileMid, during)
		return nil
	}
	pass := func(a []qml.SpecValue) (qml.SpecValue, error) {
		if tr != nil && raised {
			// Raised TWICE inside one fan-out: the handler reads state when it
			// runs, so it must run once, not once per raise.
			during = true
			if err := tr.Emit(tr.Children(tr.Root())[0], "clicked"); err != nil {
				t.Errorf("Emit mid-propagation = %v, want it queued", err)
			}
			_ = tr.Emit(tr.Children(tr.Root())[0], "clicked")
			during = false
		}
		return sv(a[0].Raw), nil
	}
	tr = tree(t, rec, `Flex { Button { id: b onClicked: save() } Text { id: a text: pass(x) } }`,
		map[string]string{"x": "1"}, map[string]decl.ValueFunc{"pass": pass})

	raised = true
	if _, err := tr.SetSource("x", sv("2")); err != nil {
		t.Fatalf("SetSource: %v", err)
	}
	if len(ranWhileMid) != 1 {
		t.Fatalf("the handler ran %d times, want exactly once after the fan-out", len(ranWhileMid))
	}
	if ranWhileMid[0] {
		t.Error("the handler ran while the propagation was still fanning out")
	}
}

// TestASignalFromANodeTheReconcileRemovedIsDropped: the widget that raised it
// is gone, so its signal is moot rather than an error.
func TestASignalFromANodeTheReconcileRemovedIsDropped(t *testing.T) {
	rec := newSplicer()
	tr := mounted(t, rec, rec.recorder,
		`Flex { Button { id: b onClicked: save() } Text { id: a text: "one" } }`)
	btn := tr.Children(tr.Root())[0]
	rec.onApply = func(decl.Application) { _ = tr.Emit(btn, "clicked") }

	if _, err := tr.Reconcile(mustSpec(t, `Flex { Text { id: a text: "two" } }`)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if strings.Contains(strings.Join(rec.trace, "\n"), "run save") {
		t.Error("a handler ran for a node the reconcile had already removed")
	}
}

// TestAFeedbackLoopThroughADeferredSignalIsStillCaught.
//
// A handler that moves a source, whose binding makes the widget re-raise the
// same signal, is a loop. Deferral must not turn it into an endless queue:
// the drain inside the handler's own operation runs while that handler's
// emission is still active, so the cycle detector sees it.
func TestAFeedbackLoopThroughADeferredSignalIsStillCaught(t *testing.T) {
	rec := newReactor()
	var tr *decl.Tree
	n := 0
	rec.handlers["save"] = func() error {
		n++
		if n > 50 {
			t.Fatal("the loop was not caught")
		}
		_, err := tr.SetSource("x", sv(strings.Repeat("y", n)))
		return err
	}
	pass := func(a []qml.SpecValue) (qml.SpecValue, error) {
		if tr != nil {
			_ = tr.Emit(tr.Children(tr.Root())[0], "clicked")
		}
		return sv(a[0].Raw), nil
	}
	tr = tree(t, rec, `Flex { Button { id: b onClicked: save() } Text { id: a text: pass(x) } }`,
		map[string]string{"x": "1"}, map[string]decl.ValueFunc{"pass": pass})

	_, err := tr.SetSource("x", sv("2"))
	if err == nil {
		t.Fatal("a feedback loop through a deferred signal reported no error")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("err = %v, want the cycle detector's diagnostic", err)
	}
}

// TestASignalRaisedMidMountIsDeferredUntilTheTreeExists.
//
// The case that motivated deferral in practice: the engine applies a property
// while mounting, the property changes the widget's state, and the widget
// reports it — an editor mounted with a bound keyset that takes it out of
// Normal mode. A handler run THEN sees a half-built tree: no root yet, and the
// nodes after this one not built at all.
//
// Root() is set only when the mount completes, which makes "did the handler
// run mid-mount" directly observable.
func TestASignalRaisedMidMountIsDeferredUntilTheTreeExists(t *testing.T) {
	rec := newReactor()
	tr := decl.New(rec)
	var rootWhenRan []decl.NodeID
	rec.handlers["save"] = func() error {
		rootWhenRan = append(rootWhenRan, tr.Root())
		return nil
	}
	raised := false
	rec.onApply = func(a decl.Application) {
		if raised || a.Prop != "text" {
			return
		}
		raised = true
		b, ok := tr.NodeByID("b")
		if !ok {
			t.Fatal("the button is not known yet; the fixture is not testing what it claims")
		}
		if err := tr.Emit(b, "clicked"); err != nil {
			t.Errorf("Emit mid-mount = %v, want it queued", err)
		}
	}
	if err := tr.Mount(wiredSpec(t, tr, rec.recorder,
		`Flex { Button { id: b onClicked: save() } Text { id: a text: "x" } }`)); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if !raised {
		t.Fatal("nothing was raised during the mount; the fixture proves nothing")
	}
	if len(rootWhenRan) != 1 {
		t.Fatalf("the handler ran %d times, want once", len(rootWhenRan))
	}
	if rootWhenRan[0] == decl.NoNode {
		t.Error("the handler ran before the mount completed, against a tree with no root")
	}
}
