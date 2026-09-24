package decl_test

import (
	"context"
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
    orientation: horizontal
    Flex {
        id: list
        direction: vertical
        Text { id: a text: "alpha" }
        Text { id: b text: "bravo" }
        Text { id: c text: "charlie" }
    }
    Text { id: side text: "right pane" }
}`

// reordered moves charlie to the front. Nothing else about the file changes.
const listReordered = `Split {
    id: root
    orientation: horizontal
    Flex {
        id: list
        direction: vertical
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
    orientation: horizontal
    Flex {
        id: list
        direction: vertical
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
    direction: vertical
    Button { id: btn label: "Save" enabled: true }
    Tracker { id: work }
    Text { id: tail text: "tail" }
}`
	const after = `Flex {
    id: list
    direction: vertical
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
    orientation: horizontal
    Text { id: side text: "right pane" }
    Flex {
        id: list
        direction: vertical
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
    orientation: horizontal
    Flex {
        id: list
        direction: vertical
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
			`Split { id: root orientation: vertical Text { id: a text: "l" } Text { id: b text: "r" } }`,
			"Split", "orientation"},
		{"Flex.direction",
			`Flex { id: root Text { id: a text: "l" } }`,
			`Flex { id: root direction: horizontal Text { id: a text: "l" } }`,
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
// distinction a boolean could not carry.
//
// "Unknown" and "constructor-only" are both un-appliable, which is why one
// boolean looked sufficient. They call for opposite responses: a
// constructor-only change rebuilds the node, while a property the adapter does
// not have must leave the tree untouched, because the rebuilt node would refuse
// it too.
func TestClassifyPropertyDistinguishesAllThreeKinds(t *testing.T) {
	tr, a := mount(t, listScreen, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })

	root := nodeNamed(t, tr, "root") // Split
	text := nodeNamed(t, tr, "side") // Text
	list := nodeNamed(t, tr, "list") // Flex
	cases := []struct {
		name string
		node decl.NodeID
		prop string
		want decl.PropertyKind
	}{
		{"Text.text has a setter", text, "text", decl.PropRuntime},
		{"Split.orientation is a constructor argument", root, "orientation", decl.PropConstructorOnly},
		{"Flex.direction is a constructor argument", list, "direction", decl.PropConstructorOnly},
		{"a misspelling is not constructor-only", text, "nosuchprop", decl.PropUnknown},
		{"a property of a DIFFERENT type is unknown here", text, "orientation", decl.PropUnknown},
		{"an unknown node", decl.NodeID(9999), "text", decl.PropUnknown},
	}
	for _, c := range cases {
		if got := a.ClassifyProperty(c.node, c.prop); got != c.want {
			t.Errorf("%s: ClassifyProperty = %s, want %s", c.name, got, c.want)
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
	const before = `Flex { id: list direction: vertical Text { id: a text: "one" } }`
	const typo = `Flex { id: list direction: vertical Text { id: a text: "one" nosuch: "x" } }`
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
		res, err = tr.Reload([]byte(`Flex { id: list direction: vertical Text { id: a text: "two" } }`))
	})
	if err != nil {
		t.Fatalf("the tree was latched by a property typo: %v", err)
	}
	if res.Applied != 1 {
		t.Errorf("Applied = %d, want 1", res.Applied)
	}
	waitFor(t, func() bool { return strings.Contains(be.String(), "two") })
}
