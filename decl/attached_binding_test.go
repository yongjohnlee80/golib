package decl_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
)

// slotAttached is an Attacher's judgement for these fixtures: Slot.side is attached and admitted
// under any parent, everything else is not attached.
func slotAttached(_, _, prop string) (bool, error) { return strings.HasPrefix(prop, "Slot."), nil }

// attacher reads Slot.side off its children, and is no Classifier.
type attacher struct{ *splicer }

func (a *attacher) CheckAttached(parent, child, prop string) (bool, error) {
	return slotAttached(parent, child, prop)
}

// classifyingAttacher reads Slot.side too, and classifies it as its reactor's tables say.
type classifyingAttacher struct{ *reactor }

func (a *classifyingAttacher) CheckAttached(parent, child, prop string) (bool, error) {
	return slotAttached(parent, child, prop)
}

const slotDoc = `Flex { Text { id: a Slot.side: side text: "x" } }`

// A bound attached property is taken only where the adapter applies a change of it to the built
// node: refused through an Attacher that is no Classifier, and through one that calls it
// constructor-only; taken where it is PropRuntime, and a change of its source reaches Apply.
func TestABoundAttachedPropertyIsTakenOnlyWhereItsChangeIsApplied(t *testing.T) {
	mount := func(a decl.Adapter, rec *recorder) error {
		tr := decl.New(a)
		if err := tr.DeclareSource("side", sv("left")); err != nil {
			t.Fatal(err)
		}
		return tr.Mount(wiredSpec(t, tr, rec, slotDoc))
	}
	plain := &attacher{newSplicer()}
	if err := mount(plain, plain.recorder); !errors.Is(err, decl.ErrAttached) {
		t.Errorf("bound through an Attacher that is no Classifier: %v, want ErrAttached", err)
	}
	ctor := &classifyingAttacher{newReactor()}
	ctor.ctor["Text"] = map[string]bool{"Slot.side": true}
	if err := mount(ctor, ctor.recorder); !errors.Is(err, decl.ErrAttached) {
		t.Errorf("bound where it is constructor-only: %v, want ErrAttached", err)
	}

	live := &classifyingAttacher{newReactor()}
	live.runtime["Text"]["Slot.side"] = true
	tr := treeWith(t, live, live.recorder, slotDoc, map[string]string{"side": "left"}, nil)
	if _, err := tr.SetSource("side", sv("right")); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(live.trace, func(l string) bool { return strings.Contains(l, "Slot.side=") && strings.Contains(l, "(right)") }) {
		t.Errorf("a change of a live attached property's source never reached Apply: %q", live.trace)
	}
}
