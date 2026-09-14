package tui_test

import (
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// External-boundary coverage for ADR golib-tui-0011 L0 (verification row 25,
// and the §8.1 rule that extension probes run from a package importing tui as
// an ordinary consumer). Everything here uses only exported API — if any of it
// stopped compiling, an external caller's code would have broken too.

type extProbe struct{ id int }

func (p *extProbe) Init(*tui.Context)                 {}
func (p *extProbe) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{W: 1, H: 1}) }
func (p *extProbe) Render(tui.Surface)                {}
func (p *extProbe) HandleEvent(tui.Event) bool        { return false }

func fill(m *tui.MultiChild, n int) []tui.Component {
	want := make([]tui.Component, 0, n)
	for i := range n {
		p := &extProbe{id: i}
		want = append(want, p)
		m.Add(p)
	}
	return want
}

// TestItemsStillCompilesAndBehavesExternally is the compatibility guarantee
// ledger 19 makes: Items is preserved unchanged, so code written against the
// pre-L0 API keeps working. This test exists to fail if Items is ever removed
// or its signature altered without the deliberate break that was promised.
func TestItemsStillCompilesAndBehavesExternally(t *testing.T) {
	var m tui.MultiChild
	want := fill(&m, 3)

	got := m.Items() // []tui.Component — the pre-L0 shape, unchanged
	if !slices.Equal(got, want) {
		t.Fatalf("Items() = %v, want %v", got, want)
	}
}

func TestLenAndAllFromOutsideThePackage(t *testing.T) {
	var m tui.MultiChild
	want := fill(&m, 3)

	if m.Len() != len(want) {
		t.Fatalf("Len() = %d, want %d", m.Len(), len(want))
	}

	var got []tui.Component
	for i, c := range m.All() {
		if i != len(got) {
			t.Fatalf("index %d out of document order (expected %d)", i, len(got))
		}
		got = append(got, c)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("All() = %v, want %v", got, want)
	}
}

// TestAllHandsOutNoSlice is the aliasing guarantee stated positively: All's
// yielded value is a single Component, never a collection, so there is nothing
// an external caller could write through to reach the backing store. The
// assertion is carried by the type — this compiles only because c is a
// Component — with a behavioural check that iteration is unaffected by
// whatever the caller does with its own copy.
func TestAllHandsOutNoSlice(t *testing.T) {
	var m tui.MultiChild
	want := fill(&m, 3)

	var collected []tui.Component
	for _, c := range m.All() {
		var single tui.Component = c // no slice is ever in the caller's hands
		collected = append(collected, single)
	}

	// Reordering the caller's OWN copy must not disturb the container.
	slices.Reverse(collected)

	var after []tui.Component
	for _, c := range m.All() {
		after = append(after, c)
	}
	if !slices.Equal(after, want) {
		t.Fatalf("container order changed via a caller's copy: %v", after)
	}
}
