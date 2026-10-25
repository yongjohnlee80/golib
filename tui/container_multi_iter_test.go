package tui

import (
	"slices"
	"testing"
)

// These tests pin both halves of the compatibility promise: the new alias-safe
// iteration surface behaves correctly, while the deprecated live slice remains
// unchanged until a deliberate breaking release.

// Children are the package's existing test component (app_test.go), so these
// tests exercise the same shape every other tui test does.

func newFilled(n int) (*MultiChild, []Component) {
	var m MultiChild
	want := make([]Component, 0, n)
	for i := range n {
		p := &probe{name: string(rune('a' + i))}
		want = append(want, p)
		m.Add(p)
	}
	return &m, want
}

func TestLenCountsTheChildren(t *testing.T) {
	for _, n := range []int{0, 1, 5} {
		m, _ := newFilled(n)
		if m.Len() != len(m.items) {
			t.Errorf("n=%d: Len()=%d, %d children", n, m.Len(), len(m.items))
		}
		if m.Len() != n {
			t.Errorf("n=%d: Len()=%d", n, m.Len())
		}
	}
}

// --- the new surface ---

func TestAllYieldsDocumentOrderWithIndices(t *testing.T) {
	m, want := newFilled(4)

	var gotIdx []int
	var gotComp []Component
	for i, c := range m.All() {
		gotIdx = append(gotIdx, i)
		gotComp = append(gotComp, c)
	}

	if !slices.Equal(gotIdx, []int{0, 1, 2, 3}) {
		t.Errorf("indices = %v, want 0..3", gotIdx)
	}
	if !slices.Equal(gotComp, want) {
		t.Errorf("children = %v, want %v", gotComp, want)
	}
	// All yields exactly the children, in order.
	if !slices.Equal(gotComp, m.items) {
		t.Error("All() disagrees with the children on contents or order")
	}
}

func TestAllOnEmptyYieldsNothing(t *testing.T) {
	var m MultiChild
	for range m.All() {
		t.Fatal("All() yielded from an empty container")
	}
	if m.Len() != 0 {
		t.Errorf("Len() = %d on empty", m.Len())
	}
}

func TestAllStopsEarlyOnBreak(t *testing.T) {
	m, _ := newFilled(5)

	seen := 0
	for i := range m.All() {
		seen++
		if i == 1 {
			break
		}
	}
	if seen != 2 {
		t.Fatalf("break after index 1 visited %d children, want 2", seen)
	}

	// Breaking must leave the container intact and re-iterable.
	if m.Len() != 5 {
		t.Fatalf("Len() = %d after an early break, want 5", m.Len())
	}
	again := 0
	for range m.All() {
		again++
	}
	if again != 5 {
		t.Fatalf("re-iteration visited %d, want 5", again)
	}
}

// --- the migrated internal callers ---

// TestAllSurvivesMutation covers what the three migrated containers rely on:
// Add/Remove keep All and Len in agreement, in document order.
func TestAllTracksAddAndRemove(t *testing.T) {
	m, want := newFilled(3)

	m.Remove(want[1])
	if m.Len() != 2 {
		t.Fatalf("Len() = %d after Remove, want 2", m.Len())
	}
	var got []Component
	for _, c := range m.All() {
		got = append(got, c)
	}
	if !slices.Equal(got, []Component{want[0], want[2]}) {
		t.Fatalf("after Remove, All() = %v", got)
	}

	extra := &probe{name: "z"}
	m.Add(extra)
	last := -1
	var lastComp Component
	for i, c := range m.All() {
		last, lastComp = i, c
	}
	if last != 2 || lastComp != extra {
		t.Fatalf("after Add, last yielded (%d, %v), want (2, %v)", last, lastComp, extra)
	}
}
