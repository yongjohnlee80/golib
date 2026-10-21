package widget

import (
	"slices"
	"testing"
)

// The cached visible rows are a fresh walk's after every kind of change: new roots, an expand (a
// request, its children, a reopen), a collapse, a load error and a reset. A write that forgot to
// drop the cache would leave flatten answering the old rows.
func TestATreesCachedRowsAreAFreshWalkAfterEveryChange(t *testing.T) {
	tr := NewTree()
	check := func(step string) {
		t.Helper()
		_ = tr.flatten() // fill the cache before the next change
		if got, want := tr.flatten(), tr.walkRows(); !slices.Equal(got, want) {
			t.Fatalf("after %s: cached rows %v, a fresh walk %v", step, got, want)
		}
	}
	a, b := NewTreeNode("a", "a"), NewTreeNode("b", "b")
	tr.SetRoots(a, b)
	check("SetRoots")

	tr.ToggleExpanded(a) // unloaded: a request, the spinner row
	check("an expand that requests")
	a.SetChildren(a.gen, []*TreeNode{NewTreeNode("a1", "a1", WithLeaf()), NewTreeNode("a2", "a2")})
	check("the children arriving")

	tr.ToggleExpanded(a)
	check("a collapse")
	tr.ToggleExpanded(a) // loaded: open at once
	check("an expand of loaded children")

	a2 := a.children[1]
	tr.ToggleExpanded(a2)
	a2.SetLoadError(a2.gen, "no access")
	check("a load error")

	a.Reset()
	check("a reset")

	tr.SetRoots(NewTreeNode("c", "c"))
	check("new roots again")
}
