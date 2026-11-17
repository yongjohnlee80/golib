package decl_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
)

// SPLICE VETTER (decl.SpliceVetter): a node that can restructure may still refuse a particular
// splice, told the types of the children the reload adds, drops, replaces or moves among those it
// keeps; refused, the node is rebuilt.

// vetSplicer is a splicer that is also a SpliceVetter: it records what each CanSplice is told and
// refuses a splice touching a type in refuse.
type vetSplicer struct {
	*splicer
	refuse map[string]bool
	told   [][]string
}

func newVetSplicer(refuse ...string) *vetSplicer {
	v := &vetSplicer{splicer: newSplicer(), refuse: map[string]bool{}}
	for _, r := range refuse {
		v.refuse[r] = true
	}
	return v
}

func (v *vetSplicer) CanSplice(_ decl.NodeID, types []string) bool {
	got := slices.Clone(types)
	slices.Sort(got)
	v.told = append(v.told, got)
	return !slices.ContainsFunc(types, func(t string) bool { return v.refuse[t] })
}

const vetBefore = `Outer { Flex {
	Text { id: a }
	Button { id: b }
	Text { id: c }
} }`

// b leaves, x arrives at the front: a and c shift, keeping their order among the kept
const vetAfter = `Outer { Flex {
	Label { id: x }
	Text { id: a }
	Text { id: c }
} }`

// The vetter is told the types the reload adds and drops, and not those of the children that only
// shift; consenting, the Flex is spliced, not rebuilt. The mutant, named here: the kept children
// that only shift reported as moved (Text told).
func TestASpliceVetterIsToldWhatTheReloadSplices(t *testing.T) {
	v := newVetSplicer()
	tr := mounted(t, v, v.recorder, vetBefore)
	res := reconcile(t, tr, vetAfter)
	if len(res.Rebuilt) != 0 {
		t.Fatalf("Rebuilt = %v, want a splice", res.Rebuilt)
	}
	if want := [][]string{{"Button", "Label"}}; !slices.EqualFunc(v.told, want, slices.Equal[[]string]) {
		t.Fatalf("CanSplice told %v, want %v", v.told, want)
	}
}

// A reorder among the kept children is told as the types of those that moved.
func TestASpliceVetterIsToldOfAReorder(t *testing.T) {
	v := newVetSplicer()
	tr := mounted(t, v, v.recorder, `Outer { Flex { Text { id: a } Button { id: b } } }`)
	reconcile(t, tr, `Outer { Flex { Button { id: b } Text { id: a } } }`)
	if want := [][]string{{"Button", "Text"}}; !slices.EqualFunc(v.told, want, slices.Equal[[]string]) {
		t.Fatalf("CanSplice told %v, want %v", v.told, want)
	}
}

// Refused, the node is rebuilt, as one that cannot restructure is, with the cause named; nothing is
// spliced into it first. The mutant, named here: canRestructure not asking the vetter (the Flex is
// spliced).
func TestARefusedSpliceRebuildsTheNode(t *testing.T) {
	v := newVetSplicer("Label")
	tr := mounted(t, v, v.recorder, vetBefore)
	flex := tr.Children(tr.Root())[0]
	res := reconcile(t, tr, vetAfter)
	if len(res.Rebuilt) != 1 || res.Rebuilt[0].Type != "Flex" {
		t.Fatalf("Rebuilt = %v, want the Flex", res.Rebuilt)
	}
	if !strings.Contains(res.Rebuilt[0].Reason, "cannot be restructured") {
		t.Errorf("reason does not name the cause: %q", res.Rebuilt[0].Reason)
	}
	for _, line := range v.trace {
		if strings.HasSuffix(line, fmt.Sprintf(" into %d", flex)) || strings.Contains(line, fmt.Sprintf(" into %d at", flex)) ||
			strings.HasSuffix(line, fmt.Sprintf(" from %d", flex)) || strings.Contains(line, fmt.Sprintf(" in %d to", flex)) {
			t.Errorf("a refused splice touched the old Flex's children: %q", line)
		}
	}
}
