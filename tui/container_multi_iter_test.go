package tui

import (
	"slices"
	"testing"
)

// L0 of ADR golib-tui-0011: MultiChild gains Len and All; Items is preserved
// byte-identically because no repository search can prove there is no external
// caller (ledger 19). These tests pin BOTH halves of that promise — the new
// surface behaves, and the old one did not move.

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

// --- compatibility: Items must be byte-identical to its pre-L0 behaviour ---

func TestItemsUnchangedByL0(t *testing.T) {
	m, want := newFilled(4)

	got := m.Items()
	if !slices.Equal(got, want) {
		t.Fatalf("Items() = %v, want %v", got, want)
	}

	// Pre-L0 Items returned the LIVE backing slice, and callers may depend on
	// that. Prove it still does, by writing through one returned slice and
	// observing the change through a separately obtained one — which only
	// holds while both share the backing array.
	m.Items()[0] = want[1]
	if m.Items()[0] != want[1] {
		t.Fatal("Items() no longer returns the live backing slice; " +
			"L0 was supposed to leave it byte-identical")
	}
	m.Items()[0] = want[0]
}

func TestLenAgreesWithItems(t *testing.T) {
	for _, n := range []int{0, 1, 5} {
		m, _ := newFilled(n)
		if m.Len() != len(m.Items()) {
			t.Errorf("n=%d: Len()=%d, len(Items())=%d", n, m.Len(), len(m.Items()))
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
	// All and Items must never disagree while both exist.
	if !slices.Equal(gotComp, m.Items()) {
		t.Error("All() and Items() disagree on contents or order")
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

// TestAllCannotAliasBackingStorage is the point of the whole change.
// Items hands out the live slice, so a caller can reorder it behind the
// framework's back; All hands out no slice at all. This asserts the second
// half — and that a mutation through the deprecated path is still visible,
// which is what makes it a real hazard rather than a theoretical one.
func TestAllCannotAliasBackingStorage(t *testing.T) {
	m, want := newFilled(3)

	// Via the deprecated surface, a caller CAN corrupt document order.
	m.Items()[0], m.Items()[2] = m.Items()[2], m.Items()[0]

	var got []Component
	for _, c := range m.All() {
		got = append(got, c)
	}
	if slices.Equal(got, want) {
		t.Fatal("Items() aliasing no longer reaches the backing store; " +
			"the deprecation rationale needs revisiting")
	}

	// Restore, then confirm All exposes nothing a caller could have written
	// through: its yielded values are the components themselves, never a slice.
	m.Items()[0], m.Items()[2] = m.Items()[2], m.Items()[0]
	for i, c := range m.All() {
		if c != want[i] {
			t.Fatalf("index %d = %v, want %v", i, c, want[i])
		}
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
