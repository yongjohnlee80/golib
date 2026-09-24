package decl_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// The list screen. The Flex is a real tui.Container and splices its children;
// the Split around it is not one at all and cannot. Having both on the same
// screen is the point: one reload has to do the right and different thing with
// each.
const listScreen = `Split {
    id: root
    orientation: tui.Horizontal
    Flex {
        id: list
        direction: tui.Vertical
        Text { id: a text: "alpha" }
        Text { id: b text: "bravo" }
        Text { id: c text: "charlie" }
    }
    Text { id: side text: "right pane" }
}`

// reordered moves charlie to the front. Nothing else about the file changes.
const listReordered = `Split {
    id: root
    orientation: tui.Horizontal
    Flex {
        id: list
        direction: tui.Vertical
        Text { id: c text: "charlie" }
        Text { id: a text: "alpha" }
        Text { id: b text: "bravo" }
    }
    Text { id: side text: "right pane" }
}`

// TestAReloadReordersARealScreenWithoutRebuilding is the phase's acceptance
// case, run against real widgets in a running App.
//
// The assertions are deliberately layered, because each one alone can be
// satisfied by something wrong:
//
//   - the same Go POINTERS survive — the engine did not construct replacements;
//   - the same tui.NodeIDs survive — the framework never unmounted them, which
//     is what scroll offset, focus and in-flight work actually hang on;
//   - the PAINTED screen changed — the reorder reached the display rather than
//     just the bookkeeping.
//
// A test asserting only the first two would pass against a reconcile that
// updated its own records and never told the container anything.
func TestAReloadReordersARealScreenWithoutRebuilding(t *testing.T) {
	tr, a := mount(t, listScreen, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })

	be, app := startApp(t, mustRoot(t, tr, a))
	waitFor(t, func() bool { return strings.Contains(be.String(), "charlie") })

	list := nodeNamed(t, tr, "list")
	before := childComponents(t, tr, a, list)
	beforeIDs := nodeIDsOf(t, before)
	if got := painted(be, "alpha", "bravo", "charlie"); !equalStrings(got, []string{"alpha", "bravo", "charlie"}) {
		t.Fatalf("the screen did not start in document order: %v", got)
	}

	// The reload runs ON THE LOOP GOROUTINE. Every component the reconcile
	// touches is loop-owned, so a watcher's goroutine must hand the work over
	// through App.Update rather than reaching into the tree itself.
	var res decl.Result
	var err error
	onLoop(t, app, func() { res, err = tr.Reload([]byte(listReordered)) })
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if len(res.Rebuilt) != 0 {
		t.Fatalf("a pure reorder rebuilt %v", res.Rebuilt)
	}
	if res.Created != 0 || res.Destroyed != 0 {
		t.Fatalf("a pure reorder created %d and destroyed %d", res.Created, res.Destroyed)
	}
	if res.RootReplaced {
		t.Fatal("RootReplaced on a reorder: the Split did not change and must not have been rebuilt")
	}

	after := childComponents(t, tr, a, list)
	want := []tui.Component{before[2], before[0], before[1]}
	for i := range want {
		if after[i] != want[i] {
			t.Fatalf("child %d is a different component: the reorder rebuilt instead of moving", i)
		}
	}
	// Same widgets is not the same claim as same MOUNTS. A remove-and-re-add
	// would hand back the same pointers with fresh NodeIDs, and everything the
	// framework hung on the old ones would be gone.
	afterIDs := nodeIDsOf(t, after)
	for i, c := range after {
		if afterIDs[i] != beforeIDs[indexOfComponent(before, c)] {
			t.Errorf("child %d was remounted: NodeID changed", i)
		}
	}

	waitFor(t, func() bool {
		return equalStrings(painted(be, "alpha", "bravo", "charlie"),
			[]string{"charlie", "alpha", "bravo"})
	})
}

// TestAReloadInsertsANewChildInTheMiddle covers the placement half of an
// insert, which appending alone satisfies at the END of a list and nowhere
// else.
//
// tui.Container has no insert-at-index — Add appends — so the adapter appends
// and then moves. A version that skipped the move passed every other test here,
// because every other reload only ever adds to the end.
func TestAReloadInsertsANewChildInTheMiddle(t *testing.T) {
	const withDelta = `Split {
    id: root
    orientation: tui.Horizontal
    Flex {
        id: list
        direction: tui.Vertical
        Text { id: a text: "alpha" }
        Text { id: d text: "delta" }
        Text { id: b text: "bravo" }
        Text { id: c text: "charlie" }
    }
    Text { id: side text: "right pane" }
}`
	tr, a := mount(t, listScreen, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })
	be, app := startApp(t, mustRoot(t, tr, a))
	waitFor(t, func() bool { return strings.Contains(be.String(), "charlie") })

	list := nodeNamed(t, tr, "list")
	before := childComponents(t, tr, a, list)

	var res decl.Result
	var err error
	onLoop(t, app, func() { res, err = tr.Reload([]byte(withDelta)) })
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if res.Created != 1 || res.Destroyed != 0 || len(res.Rebuilt) != 0 {
		t.Fatalf("inserting one child should create exactly one node: %+v", res)
	}

	// The three originals keep their identity; the newcomer sits between them.
	after := childComponents(t, tr, a, list)
	if len(after) != 4 {
		t.Fatalf("children = %d, want 4", len(after))
	}
	if after[0] != before[0] || after[2] != before[1] || after[3] != before[2] {
		t.Error("the surviving children did not keep their identity around the insert")
	}

	waitFor(t, func() bool {
		return equalStrings(painted(be, "alpha", "bravo", "charlie", "delta"),
			[]string{"alpha", "delta", "bravo", "charlie"})
	})
}

// TestAReloadKeepsFocusAndInFlightWork is the claim a reorder exists to make
// good on, asserted on the things a user would actually lose.
//
// A focused button and a running task both hang off the mount, so this is the
// test that fails if a reconcile quietly re-mounts something.
func TestAReloadKeepsFocusAndInFlightWork(t *testing.T) {
	reg := tuidecl.StdRegistry()
	tuidecl.Register(reg, "Tracker", buildTracker)
	opts := append(tuidecl.StdProperties(),
		tuidecl.WithHostFuncs(tuidecl.HostFuncs{}),
		tuidecl.WithErrorSink(func(err error) { t.Errorf("unexpected handler error: %v", err) }),
	)
	ad := tuidecl.New(reg, opts...)
	tr := decl.New(ad)

	const before = `Flex {
    id: list
    direction: tui.Vertical
    Button { id: btn label: "Save" enabled: true }
    Tracker { id: work }
    Text { id: tail text: "tail" }
}`
	const after = `Flex {
    id: list
    direction: tui.Vertical
    Text { id: tail text: "tail" }
    Tracker { id: work }
    Button { id: btn label: "Save" enabled: true }
}`
	spec, perr := parse.QML{}.Parse([]byte(before))
	if perr != nil {
		t.Fatalf("schema does not parse: %v", perr)
	}
	if err := tr.Mount(spec); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	_, app := startApp(t, mustRoot(t, tr, ad))
	btn := componentOf(t, tr, ad, "btn").(*widget.Button)
	work := componentOf(t, tr, ad, "work").(*tracker)

	// Focus the button and start a task that is still running when the reload
	// happens. The task blocks until the test releases it, so "in flight" is a
	// fact about the schedule rather than a hopeful sleep.
	release := make(chan struct{})
	var taskID tui.TaskID
	onLoop(t, app, func() {
		btn.Context().RequestFocus()
		taskID = work.Context().Go(func(context.Context) (any, error) {
			<-release
			return "finished", nil
		})
	})
	onLoop(t, app, func() {
		if !btn.Context().Focused() {
			t.Fatal("the button did not take focus, so this test could not observe losing it")
		}
	})
	beforeBtnNode, beforeWorkNode := btn.NodeID(), work.NodeID()

	var res decl.Result
	var err error
	onLoop(t, app, func() { res, err = tr.Reload([]byte(after)) })
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if len(res.Rebuilt) != 0 {
		t.Fatalf("the reorder rebuilt %v, so this test cannot say anything about preservation", res.Rebuilt)
	}

	// Focus survived the move.
	var stillFocused bool
	onLoop(t, app, func() { stillFocused = btn.Context().Focused() })
	if !stillFocused {
		t.Error("focus was lost across the reload")
	}
	if btn.NodeID() != beforeBtnNode || work.NodeID() != beforeWorkNode {
		t.Errorf("a component was remounted: NodeIDs %d/%d -> %d/%d",
			beforeBtnNode, beforeWorkNode, btn.NodeID(), work.NodeID())
	}

	// And the task that was running across the reload still delivers, to the
	// same node. A remount would have made its result stale and dropped it.
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for {
		var got []tui.TaskResult
		onLoop(t, app, func() { got = append(got, work.results...) })
		if len(got) > 0 {
			if got[0].ID != taskID {
				t.Errorf("delivered task %d, want %d", got[0].ID, taskID)
			}
			if got[0].Value != "finished" || got[0].Err != nil {
				t.Errorf("task result = %v / %v", got[0].Value, got[0].Err)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the in-flight task never delivered its result across the reload")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestASplitIsRebuiltBecauseTheToolkitCannotRestructureIt is the degradation
// path, on the real widget that forces it.
//
// widget.Split takes both children as required constructor arguments and
// implements no Add, Remove or Move. Swapping its children is therefore not an
// edit that can be made in place, and the engine must say so rather than
// attempt it.
func TestASplitIsRebuiltBecauseTheToolkitCannotRestructureIt(t *testing.T) {
	const swapped = `Split {
    id: root
    orientation: tui.Horizontal
    Text { id: side text: "right pane" }
    Flex {
        id: list
        direction: tui.Vertical
        Text { id: a text: "alpha" }
        Text { id: b text: "bravo" }
        Text { id: c text: "charlie" }
    }
}`
	tr, a := mount(t, listScreen, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })
	_, app := startApp(t, mustRoot(t, tr, a))

	var res decl.Result
	var err error
	onLoop(t, app, func() { res, err = tr.Reload([]byte(swapped)) })
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if len(res.Rebuilt) != 1 || res.Rebuilt[0].Type != "Split" {
		t.Fatalf("Rebuilt = %v, want exactly the Split", res.Rebuilt)
	}
	if !strings.Contains(res.Rebuilt[0].Reason, "cannot be restructured") {
		t.Errorf("the reason does not name the cause: %q", res.Rebuilt[0].Reason)
	}
	if !res.RootReplaced {
		t.Error("the root Split was rebuilt but RootReplaced is false, " +
			"so a host would keep rendering the component that is no longer the tree")
	}
}

// TestCanRestructureAnswersFromTheToolkit pins the capability to what the
// widgets actually implement, rather than to a table this package maintains.
func TestCanRestructureAnswersFromTheToolkit(t *testing.T) {
	tr, a := mount(t, listScreen, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })

	cases := []struct {
		name string
		want bool
	}{
		{"list", true},  // tui.Flex is a Container
		{"root", false}, // widget.Split implements no Add, Remove or Move
		{"side", false}, // a leaf has no children to restructure
	}
	for _, c := range cases {
		id := nodeNamed(t, tr, c.name)
		if got := a.CanRestructure(id); got != c.want {
			comp, _ := a.Component(id)
			t.Errorf("CanRestructure(%s, %T) = %v, want %v", c.name, comp, got, c.want)
		}
	}
	if a.CanRestructure(decl.NodeID(9999)) {
		t.Error("an unknown node claimed it could be restructured")
	}
}

// -------------------------------------------------------------- the tracker

// tracker is a host-registered widget that records the task results addressed
// to it. It exists because TaskResult is an addressed EVENT: the only way to
// observe that a task survived a reload is to be the component it was sent to.
type tracker struct {
	widget.Base
	results []tui.TaskResult
}

func (x *tracker) Layout(c tui.Constraints) tui.Size {
	return tui.Size{W: c.MinW, H: c.MinH}
}

func (x *tracker) Render(tui.Surface) {}

func (x *tracker) HandleEvent(ev tui.Event) bool {
	if r, ok := ev.(tui.TaskResult); ok {
		x.results = append(x.results, r)
		return true
	}
	return false
}

func buildTracker(tuidecl.Build) (tui.Component, []string, error) {
	return &tracker{}, nil, nil
}

// ------------------------------------------------------------------ helpers

// nodeNamed finds the node a schema id refers to.
func nodeNamed(t *testing.T, tr *decl.Tree, name string) decl.NodeID {
	t.Helper()
	for id := decl.NodeID(1); id <= decl.NodeID(tr.Len()+8); id++ {
		if got, ok := tr.SchemaID(id); ok && got == name {
			return id
		}
	}
	t.Fatalf("no node declares id %q", name)
	return decl.NoNode
}

func componentOf(t *testing.T, tr *decl.Tree, a *tuidecl.Adapter, name string) tui.Component {
	t.Helper()
	c, ok := a.Component(nodeNamed(t, tr, name))
	if !ok {
		t.Fatalf("node %q built no component", name)
	}
	return c
}

func childComponents(t *testing.T, tr *decl.Tree, a *tuidecl.Adapter, parent decl.NodeID) []tui.Component {
	t.Helper()
	var out []tui.Component
	for _, id := range tr.Children(parent) {
		c, ok := a.Component(id)
		if !ok {
			t.Fatalf("child node %d has no component", id)
		}
		out = append(out, c)
	}
	return out
}

func nodeIDsOf(t *testing.T, comps []tui.Component) []tui.NodeID {
	t.Helper()
	out := make([]tui.NodeID, len(comps))
	for i, c := range comps {
		n, ok := c.(interface{ NodeID() tui.NodeID })
		if !ok {
			t.Fatalf("%T does not report a NodeID", c)
		}
		if n.NodeID() == 0 {
			t.Fatalf("%T is not mounted, so its NodeID proves nothing", c)
		}
		out[i] = n.NodeID()
	}
	return out
}

func indexOfComponent(in []tui.Component, c tui.Component) int {
	for i, v := range in {
		if v == c {
			return i
		}
	}
	return -1
}

// painted reports the given words in the order they appear on screen, which is
// the only order a user can see.
func painted(be *tui.TestBackend, words ...string) []string {
	screen := be.String()
	type hit struct {
		at   int
		word string
	}
	var hits []hit
	for _, w := range words {
		if i := strings.Index(screen, w); i >= 0 {
			hits = append(hits, hit{i, w})
		}
	}
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].at < hits[j-1].at; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.word
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------- removal and the guards

// TestAReloadRemovesAChildFromARealContainer exercises RemoveChild against the
// actual toolkit, which nothing else here did.
//
// The engine's own tests delete nodes through a fake. That proves the engine
// asks correctly; it says nothing about what tui.Container.Remove does, which
// is an unmount CASCADE over the child's whole subtree. This is the path where
// detaching before releasing has to be right against a live container rather
// than against a recording one.
func TestAReloadRemovesAChildFromARealContainer(t *testing.T) {
	const withoutBravo = `Split {
    id: root
    orientation: tui.Horizontal
    Flex {
        id: list
        direction: tui.Vertical
        Text { id: a text: "alpha" }
        Text { id: c text: "charlie" }
    }
    Text { id: side text: "right pane" }
}`
	tr, a := mount(t, listScreen, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })
	be, app := startApp(t, mustRoot(t, tr, a))
	waitFor(t, func() bool { return strings.Contains(be.String(), "bravo") })

	list := nodeNamed(t, tr, "list")
	before := childComponents(t, tr, a, list)
	gone := before[1]

	var res decl.Result
	var err error
	onLoop(t, app, func() { res, err = tr.Reload([]byte(withoutBravo)) })
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if res.Destroyed != 1 || res.Created != 0 || len(res.Rebuilt) != 0 {
		t.Fatalf("removing one child should destroy exactly one node: %+v", res)
	}

	after := childComponents(t, tr, a, list)
	if len(after) != 2 || after[0] != before[0] || after[1] != before[2] {
		t.Error("the survivors did not keep their identity across the removal")
	}
	// The removed widget must be UNMOUNTED, not merely forgotten by the engine.
	//
	// Context.Mounted is the instrument, and its own documentation says why a
	// retained pointer is not: it "answers a different question and answers it
	// wrongly for every component that has ever been mounted". NodeID is one of
	// those retained answers — Base never clears its context on unmount, so a
	// removed widget goes on reporting the id it had.
	var stillMounted bool
	onLoop(t, app, func() {
		if b, ok := gone.(interface{ Context() *tui.Context }); ok && b.Context() != nil {
			stillMounted = b.Context().Mounted()
		}
	})
	if stillMounted {
		t.Error("the removed child is still mounted, so the container kept it")
	}
	waitFor(t, func() bool { return !strings.Contains(be.String(), "bravo") })
	if painted := be.String(); !strings.Contains(painted, "alpha") ||
		!strings.Contains(painted, "charlie") {
		t.Errorf("removing bravo disturbed its siblings:\n%s", painted)
	}
}

// TestTheStructuralGuardsRefuseRatherThanPanic covers the paths that exist
// because the toolkit is unforgiving.
//
// tui.Container.Move PANICS on an out-of-range index (errs.Fatal), and Remove
// is a SILENT NO-OP for a child it does not hold. Neither is a shape this seam
// can pass on: a panic takes the process down instead of surfacing as the
// refusal the engine knows how to report, and silence hides a lost child. These
// are the guards, asserted directly because no correct reconcile reaches them.
func TestTheStructuralGuardsRefuseRatherThanPanic(t *testing.T) {
	tr, a := mount(t, listScreen, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })

	list := nodeNamed(t, tr, "list")
	side := nodeNamed(t, tr, "side")
	kids := tr.Children(list)
	const missing = decl.NodeID(9999)

	cases := []struct {
		name string
		call func() error
		want string
	}{
		{"move past the end", func() error { return a.MoveChild(list, kids[0], 99) }, "out of range"},
		{"move to a negative index", func() error { return a.MoveChild(list, kids[0], -1) }, "out of range"},
		{"move within a non-container", func() error { return a.MoveChild(side, kids[0], 0) }, "not a container"},
		{"move an unknown child", func() error { return a.MoveChild(list, missing, 0) }, "has no component"},
		{"move within an unknown parent", func() error { return a.MoveChild(missing, kids[0], 0) }, "has no component"},
		{"remove from a non-container", func() error { return a.RemoveChild(side, kids[0]) }, "not a container"},
		{"remove an unknown child", func() error { return a.RemoveChild(list, missing) }, "has no component"},
		{"insert into a non-container", func() error { return a.InsertChild(side, kids[0], 0) }, "not a container"},
		{"insert past the end", func() error { return a.InsertChild(list, kids[0], 99) }, "out of range"},
		{"insert at a negative index", func() error { return a.InsertChild(list, kids[0], -1) }, "out of range"},
		{"insert an unknown child", func() error { return a.InsertChild(list, missing, 0) }, "has no component"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			if err == nil {
				t.Fatalf("%s was accepted; it must be refused", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not say %q", err, c.want)
			}
		})
	}
}

func countKids(t *testing.T, a *tuidecl.Adapter, id decl.NodeID) int {
	t.Helper()
	return len(containerKids(t, a, id))
}

func containerKids(t *testing.T, a *tuidecl.Adapter, id decl.NodeID) []tui.Component {
	t.Helper()
	c, ok := a.Component(id)
	if !ok {
		t.Fatalf("node %d has no component", id)
	}
	cont, ok := c.(tui.Container)
	if !ok {
		t.Fatalf("%T is not a container", c)
	}
	var out []tui.Component
	for child := range cont.Children() {
		out = append(out, child)
	}
	return out
}

// TestAddingAConstructorOnlyPropertyRebuilds covers the blind spot in inferring
// "has no setter" from what a builder reported consuming.
//
// A builder reports a property consumed only when the schema DECLARED it. Mount
// a Split with no orientation and it takes the default and consumes nothing, so
// adding `orientation` in a later reload looked like an ordinary runtime
// property — and failed the whole reload against a widget that has no such
// setter. The adapter owns the setter table and now answers the question
// directly.
func TestAddingAConstructorOnlyPropertyRebuilds(t *testing.T) {
	cases := []struct{ name, before, after, typ, prop string }{
		{"Split.orientation",
			`Split { id: root Text { id: a text: "l" } Text { id: b text: "r" } }`,
			`Split { id: root orientation: tui.Vertical Text { id: a text: "l" } Text { id: b text: "r" } }`,
			"Split", "orientation"},
		{"Flex.direction",
			`Flex { id: root Text { id: a text: "l" } }`,
			`Flex { id: root direction: tui.Horizontal Text { id: a text: "l" } }`,
			"Flex", "direction"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr, _ := mount(t, c.before, tuidecl.HostFuncs{},
				func(err error) { t.Errorf("unexpected handler error: %v", err) })

			res, err := tr.Reload([]byte(c.after))
			if err != nil {
				t.Fatalf("adding a constructor-only property must rebuild, not fail: %v", err)
			}
			if len(res.Rebuilt) != 1 || res.Rebuilt[0].Type != c.typ {
				t.Fatalf("Rebuilt = %v, want the %s", res.Rebuilt, c.typ)
			}
			if !strings.Contains(res.Rebuilt[0].Reason, c.prop) {
				t.Errorf("the reason does not name %q: %q", c.prop, res.Rebuilt[0].Reason)
			}
		})
	}
}

// TestClassifyPropertyDistinguishesAllThreeKinds pins the capability to the
// distinction a boolean could not carry, and to the key that makes it usable.
//
// "Unknown" and "constructor-only" are both un-appliable, which is why one
// boolean looked sufficient. They call for opposite responses: a
// constructor-only change rebuilds the node, while a property the adapter does
// not have must leave the tree untouched, because the rebuilt node would refuse
// it too.
//
// It takes a TYPE NAME, and no node needs to exist. That is what lets a reload
// vet a node it is about to build.
func TestClassifyPropertyDistinguishesAllThreeKinds(t *testing.T) {
	_, a := mount(t, listScreen, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })

	cases := []struct {
		name string
		typ  string
		prop string
		want decl.PropertyKind
	}{
		{"Text.text has a setter", "Text", "text", decl.PropRuntime},
		{"Split.orientation is a constructor argument", "Split", "orientation", decl.PropConstructorOnly},
		{"Flex.direction is a constructor argument", "Flex", "direction", decl.PropConstructorOnly},
		{"a misspelling is not constructor-only", "Text", "nosuchprop", decl.PropUnknown},
		{"a property of a DIFFERENT type is unknown here", "Text", "orientation", decl.PropUnknown},
		{"a type nothing has been mounted as still answers", "Button", "label", decl.PropRuntime},
		{"an unregistered type", "NoSuchWidget", "text", decl.PropUnknown},
	}
	for _, c := range cases {
		if got := a.ClassifyProperty(c.typ, c.prop); got != c.want {
			t.Errorf("%s: ClassifyProperty(%q, %q) = %s, want %s", c.name, c.typ, c.prop, got, c.want)
		}
	}
}

// TestAnUnknownPropertyLeavesTheTreeUntouched is the case a boolean capability
// could not express.
//
// A misspelled property is not a constructor-only property. Both are
// un-appliable, so one flag answered "false" to each — and the reconciler then
// demolished a working widget to build one that refused the same property, while
// the diagnostic blamed construction for a name the adapter had simply never
// heard of. Nothing may be touched: the rebuild cannot help, so it must not
// happen.
func TestAnUnknownPropertyLeavesTheTreeUntouched(t *testing.T) {
	const before = `Flex { id: list direction: tui.Vertical Text { id: a text: "one" } }`
	const typo = `Flex { id: list direction: tui.Vertical Text { id: a text: "one" nosuch: "x" } }`
	tr, a := mount(t, before, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })
	be, app := startApp(t, mustRoot(t, tr, a))
	waitFor(t, func() bool { return strings.Contains(be.String(), "one") })

	list := nodeNamed(t, tr, "list")
	was := childComponents(t, tr, a, list)
	wasIDs := nodeIDsOf(t, was)

	var res decl.Result
	var err error
	onLoop(t, app, func() { res, err = tr.Reload([]byte(typo)) })

	if err == nil {
		t.Fatal("a property the adapter does not have must be refused")
	}
	if !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("the error does not name the property: %v", err)
	}
	// It must NOT be reported as a constructor-only problem: that was the
	// misdiagnosis, and it is the half a reader would act on.
	if strings.Contains(err.Error(), "construction") {
		t.Errorf("an unknown property was blamed on construction: %v", err)
	}
	if res.Created != 0 || res.Destroyed != 0 || len(res.Rebuilt) != 0 {
		t.Fatalf("the tree was mutated for a property that can never apply: %+v", res)
	}

	// Same widgets, same mounts, still painting.
	now := childComponents(t, tr, a, list)
	if len(now) != len(was) {
		t.Fatalf("children = %d, want %d", len(now), len(was))
	}
	for i := range was {
		if now[i] != was[i] {
			t.Errorf("child %d was replaced", i)
		}
	}
	for i, got := range nodeIDsOf(t, now) {
		if got != wasIDs[i] {
			t.Errorf("child %d was remounted: NodeID %d -> %d", i, wasIDs[i], got)
		}
	}
	if !strings.Contains(be.String(), "one") {
		t.Errorf("the screen lost its content:\n%s", be.String())
	}

	// And the tree is NOT latched: correcting the typo works.
	onLoop(t, app, func() {
		res, err = tr.Reload([]byte(`Flex { id: list direction: tui.Vertical Text { id: a text: "two" } }`))
	})
	if err != nil {
		t.Fatalf("the tree was latched by a property typo: %v", err)
	}
	if res.Applied != 1 {
		t.Errorf("Applied = %d, want 1", res.Applied)
	}
	waitFor(t, func() bool { return strings.Contains(be.String(), "two") })
}

// TestATypoInANodeTHATDOESNOTEXISTYETLeavesTheTreeUntouched is the case a
// node-keyed classifier structurally could not reach.
//
// Classification used to be asked about a MOUNTED node. The nodes a reload most
// needs vetted are the ones that do not exist yet — a node the schema adds, and
// the replacement for one whose type changed — so the check was unavailable
// exactly where being wrong destroys a working widget. Both paths are covered
// here because they reach the mount differently: one through a fresh step, the
// other through a type-change rebuild.
func TestATypoInANodeTHATDOESNOTEXISTYETLeavesTheTreeUntouched(t *testing.T) {
	const before = `Flex { id: list direction: tui.Vertical Text { id: a text: "one" } }`
	const fixed = `Flex { id: list direction: tui.Vertical Text { id: a text: "two" } }`
	cases := map[string]string{
		"retyped":  `Flex { id: list direction: tui.Vertical Button { id: a label: "go" nosuch: "x" } }`,
		"inserted": `Flex { id: list direction: tui.Vertical Text { id: a text: "one" } Text { id: b nosuch: "x" } }`,
		"inserted deeper": `Flex { id: list direction: tui.Vertical Text { id: a text: "one" } ` +
			`Flex { id: sub direction: tui.Vertical Text { id: c nosuch: "x" } } }`,
	}
	for name, typo := range cases {
		t.Run(name, func(t *testing.T) {
			tr, a := mount(t, before, tuidecl.HostFuncs{},
				func(err error) { t.Errorf("unexpected handler error: %v", err) })
			be, app := startApp(t, mustRoot(t, tr, a))
			waitFor(t, func() bool { return strings.Contains(be.String(), "one") })

			list := nodeNamed(t, tr, "list")
			was := childComponents(t, tr, a, list)
			wasIDs := nodeIDsOf(t, was)
			wasKids := containerKids(t, a, list)

			var res decl.Result
			var err error
			onLoop(t, app, func() { res, err = tr.Reload([]byte(typo)) })

			if err == nil {
				t.Fatal("a property the adapter does not have must be refused")
			}
			if !strings.Contains(err.Error(), "nosuch") {
				t.Errorf("the error does not name the property: %v", err)
			}
			if res.Created != 0 || res.Destroyed != 0 || len(res.Rebuilt) != 0 {
				t.Fatalf("the tree was mutated for a property that can never apply: %+v", res)
			}

			// Nothing moved structurally either: the container holds exactly
			// what it held, in order.
			nowKids := containerKids(t, a, list)
			if len(nowKids) != len(wasKids) {
				t.Fatalf("the container's children changed: %d -> %d", len(wasKids), len(nowKids))
			}
			for i := range wasKids {
				if nowKids[i] != wasKids[i] {
					t.Errorf("container child %d changed", i)
				}
			}
			// Same components, same mounts, still painting.
			now := childComponents(t, tr, a, list)
			for i := range was {
				if now[i] != was[i] {
					t.Errorf("child %d was replaced", i)
				}
			}
			for i, got := range nodeIDsOf(t, now) {
				if got != wasIDs[i] {
					t.Errorf("child %d was remounted: NodeID %d -> %d", i, wasIDs[i], got)
				}
			}
			if !strings.Contains(be.String(), "one") {
				t.Errorf("the screen lost its content:\n%s", be.String())
			}

			// Not latched: correcting the typo works.
			onLoop(t, app, func() { res, err = tr.Reload([]byte(fixed)) })
			if err != nil {
				t.Fatalf("the tree was latched by a property typo: %v", err)
			}
			if res.Applied != 1 {
				t.Errorf("Applied = %d, want 1", res.Applied)
			}
			waitFor(t, func() bool { return strings.Contains(be.String(), "two") })
		})
	}
}

// TestAConstructorRefusingAValueLeavesTheTreeStanding covers the one thing
// classification cannot settle.
//
// Classification proves a property EXISTS. Only the constructor can say whether
// it accepts this VALUE, and only by being called — a Split declared with one
// child names nothing wrong, and the constructor refuses it because Split takes
// exactly two. (An invalid ENUM no longer reaches the constructor at all: a
// qualified name is resolved at planning, so `tui.Diagonal` is refused there.
// This cell needs a failure the CONSTRUCTOR owns.) So the
// replacement is BUILT FIRST, while the live tree is still standing, and a
// refusal costs nothing but the half-built replacement.
//
// Three shapes, because they reach the mount by different routes: the ROOT
// (which has no parent to splice into), a CHILD whose type changed, and an
// INSERTED child.
func TestAConstructorRefusingAValueLeavesTheTreeStanding(t *testing.T) {
	const before = `Flex { id: root direction: tui.Vertical Text { id: a text: "one" } }`
	cases := map[string]string{
		"root constructor value": `Split { id: root orientation: tui.Horizontal Text { id: a text: "one" } }`,
		"child of an unknown type": `Flex { id: root direction: tui.Vertical ` +
			`NoSuchWidget { id: a } }`,
		"inserted child with a bad value": `Flex { id: root direction: tui.Vertical ` +
			`Text { id: a text: "one" } Split { id: b orientation: tui.Horizontal Text {} } }`,
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			tr, a := mount(t, before, tuidecl.HostFuncs{},
				func(err error) { t.Errorf("unexpected handler error: %v", err) })
			be, app := startApp(t, mustRoot(t, tr, a))
			waitFor(t, func() bool { return strings.Contains(be.String(), "one") })

			rootComp := mustRoot(t, tr, a)
			rootNode := tr.Root()
			sizeBefore := tr.Len()
			kids := containerKids(t, a, rootNode)
			text := componentOf(t, tr, a, "a")
			textID := nodeIDsOf(t, []tui.Component{text})[0]

			var res decl.Result
			var err error
			onLoop(t, app, func() { res, err = tr.Reload([]byte(bad)) })

			if err == nil {
				t.Fatal("the constructor refused nothing; this case proves nothing")
			}
			if res.Created != 0 || res.Destroyed != 0 {
				t.Fatalf("the live tree was disturbed: %+v", res)
			}
			// A rebuild that never happened must not be REPORTED as one:
			// Result is what a host logs.
			if len(res.Rebuilt) != 0 {
				t.Errorf("Result names a rebuild that did not happen: %v", res.Rebuilt)
			}
			if res.RootReplaced {
				t.Error("RootReplaced on a reconcile that built nothing")
			}
			// The half-built replacement must be DISCARDED, not merely
			// abandoned. Nodes left behind are invisible — they are attached to
			// nothing and painted nowhere — but they hold adapter components and
			// identities for the life of the tree, and every failed reload adds
			// more.
			if got := tr.Len(); got != sizeBefore {
				t.Errorf("the tree grew from %d to %d nodes: the partial replacement was leaked",
					sizeBefore, got)
			}

			// Same root, same children, same mount, still painting.
			if mustRoot(t, tr, a) != rootComp || tr.Root() != rootNode {
				t.Error("the root was replaced by a reconcile that failed to build one")
			}
			now := containerKids(t, a, rootNode)
			if len(now) != len(kids) {
				t.Fatalf("children = %d, want %d", len(now), len(kids))
			}
			for i := range kids {
				if now[i] != kids[i] {
					t.Errorf("child %d changed", i)
				}
			}
			if got := nodeIDsOf(t, []tui.Component{text})[0]; got != textID {
				t.Errorf("a surviving child was remounted: NodeID %d -> %d", textID, got)
			}
			if !strings.Contains(be.String(), "one") {
				t.Errorf("the screen lost its content:\n%s", be.String())
			}

			// And NOT latched: a corrected reload goes through.
			onLoop(t, app, func() {
				res, err = tr.Reload([]byte(`Flex { id: root direction: tui.Vertical Text { id: a text: "two" } }`))
			})
			if err != nil {
				t.Fatalf("the tree was latched by a refused constructor: %v", err)
			}
			waitFor(t, func() bool { return strings.Contains(be.String(), "two") })
		})
	}
}

// TestAFailedSetterStillLatches is the negative control for the test above, and
// the reason this PR does not claim a reconcile is atomic.
//
// Building replacements first removes the CONSTRUCTOR from the destructive
// paths. It does not remove the setter: once a property has been applied to a
// live widget, a later failure cannot take it back. The tree latches, and
// Destroy is the documented way out. Without this case the test above would
// read as a promise the engine does not make.
func TestAFailedSetterStillLatches(t *testing.T) {
	reg := tuidecl.StdRegistry()
	opts := append(tuidecl.StdProperties(),
		tuidecl.WithHostFuncs(tuidecl.HostFuncs{}),
		tuidecl.WithErrorSink(func(error) {}),
		// A setter that refuses every value, registered over the standard one.
		tuidecl.WithSetters("Text", map[string]tuidecl.Setter{
			"text": func(tui.Component, parse.SpecValue) error {
				return errTestSetter
			},
		}),
	)
	ad := tuidecl.New(reg, opts...)
	tr := decl.New(ad)
	spec, err := parse.QML{}.Parse([]byte(`Flex { id: root direction: tui.Vertical Text { id: a } }`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := tr.Mount(spec); err != nil {
		t.Fatalf("mount: %v", err)
	}

	if _, err := tr.Reload([]byte(`Flex { id: root direction: tui.Vertical Text { id: a text: "one" } }`)); err == nil {
		t.Fatal("the setter was supposed to refuse")
	}
	// A setter that failed HAS touched the widget, so the tree is partial.
	if _, err := tr.Reload([]byte(`Flex { id: root direction: tui.Vertical Text { id: a } }`)); !errors.Is(err, decl.ErrPhase) {
		t.Fatalf("a reconcile after a failed setter returned %v, want ErrPhase", err)
	}
	if err := tr.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if err := tr.Mount(spec); err != nil {
		t.Fatalf("Destroy did not clear the latch: %v", err)
	}
}

var errTestSetter = errors.New("this setter refuses everything")

// TestAnAbortedBatchReportsNothing covers the accounting, which is the part a
// host reads and cannot check.
//
// Two siblings are replaced; the first constructor succeeds and the second
// refuses. Every replacement is then discarded — but the earlier success had
// already been counted, so the Result named a rebuild that was thrown away.
// That is the same false-log defect as reporting a rebuild before it happened,
// one scope out: fixed for a single node, still live for a batch.
func TestAnAbortedBatchReportsNothing(t *testing.T) {
	const before = `Flex { id: root direction: tui.Vertical
	    Text { id: a text: "one" }
	    Text { id: b text: "two" } }`
	const bad = `Flex { id: root direction: tui.Vertical
	    Flex { id: a direction: tui.Horizontal }
	    Split { id: b orientation: tui.Horizontal Text {} } }`

	tr, a := mount(t, before, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })
	be, app := startApp(t, mustRoot(t, tr, a))
	waitFor(t, func() bool { return strings.Contains(be.String(), "one") })

	root := tr.Root()
	kids := containerKids(t, a, root)
	size := tr.Len()

	var res decl.Result
	var err error
	onLoop(t, app, func() { res, err = tr.Reload([]byte(bad)) })

	if err == nil {
		t.Fatal("the second constructor was supposed to refuse")
	}
	if res.Created != 0 {
		t.Errorf("Created = %d: the discarded replacements were counted", res.Created)
	}
	if len(res.Rebuilt) != 0 {
		t.Errorf("Rebuilt = %v: names a rebuild that was thrown away", res.Rebuilt)
	}
	if res.Destroyed != 0 || res.Moved != 0 || res.Applied != 0 {
		t.Errorf("Result reports work that never committed: %+v", res)
	}

	// And the tree really is untouched, so the Result above is not merely
	// consistent with itself.
	if tr.Len() != size {
		t.Errorf("tree size %d -> %d: the discarded batch was leaked", size, tr.Len())
	}
	now := containerKids(t, a, root)
	if len(now) != len(kids) {
		t.Fatalf("children = %d, want %d", len(now), len(kids))
	}
	for i := range kids {
		if now[i] != kids[i] {
			t.Errorf("child %d changed", i)
		}
	}
	if !strings.Contains(be.String(), "one") || !strings.Contains(be.String(), "two") {
		t.Errorf("the screen lost content:\n%s", be.String())
	}
	onLoop(t, app, func() { _, err = tr.Reload([]byte(before)) })
	if err != nil {
		t.Fatalf("an aborted batch latched the tree: %v", err)
	}
}

// TestTheNoLatchGuaranteeIsPerParentNotWholeTree is the honest limit of the
// prebuild, asserted rather than left in a review thread.
//
// Replacements are built before anything is released WITHIN ONE PARENT. But a
// node's own properties are applied before its children's replacements are
// constructed, so an Apply on one node followed by a refused constructor DEEPER
// in the tree leaves the applied value in place and the tree latched.
//
// This exists because the public contract said constructor refusal costs
// nothing, without the condition. A limit that lives only in a review thread
// while the documentation promises the opposite is not a limit, it is a bug
// with a witness.
func TestTheNoLatchGuaranteeIsPerParentNotWholeTree(t *testing.T) {
	const before = `Flex { id: root direction: tui.Vertical
	    Text { id: a text: "one" }
	    Flex { id: mid direction: tui.Vertical Text { id: c text: "three" } } }`
	const bad = `Flex { id: root direction: tui.Vertical
	    Text { id: a text: "CHANGED" }
	    Flex { id: mid direction: tui.Vertical Split { id: c orientation: tui.Horizontal Text {} } } }`

	tr, a := mount(t, before, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })
	_, app := startApp(t, mustRoot(t, tr, a))

	var res decl.Result
	var err error
	onLoop(t, app, func() { res, err = tr.Reload([]byte(bad)) })
	if err == nil {
		t.Fatal("the deeper constructor was supposed to refuse")
	}
	// The earlier Apply DID land. That is the point of the case.
	if res.Applied != 1 {
		t.Fatalf("Applied = %d, want 1 — this case only says something if the "+
			"property change reached the widget before the deeper refusal", res.Applied)
	}
	// So the tree is partial, and latches.
	if _, e := tr.Reload([]byte(before)); !errors.Is(e, decl.ErrPhase) {
		t.Fatalf("a mixed apply-then-refuse returned %v, want ErrPhase", e)
	}
	// Destroy is the documented way out.
	onLoop(t, app, func() { err = tr.Destroy() })
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	spec, perr := parse.QML{}.Parse([]byte(before))
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	if err := tr.Mount(spec); err != nil {
		t.Fatalf("Destroy did not clear the latch: %v", err)
	}
}
