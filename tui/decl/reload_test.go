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
	opts := append(tuidecl.StdSetters(),
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
