package decl_test

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
)

// rootvet_test.go covers the optional RootVetter capability: an adapter
// refuses a type as the document's root — one that means something only
// inside another.

// rootVetter is a reactor that refuses a Text as the root.
type rootVetter struct {
	*reactor
	types map[decl.NodeID]string
	asked []decl.NodeID
}

func newRootVetter() *rootVetter {
	return &rootVetter{reactor: newReactor(), types: map[decl.NodeID]string{}}
}

func (r *rootVetter) Create(c decl.Construction) ([]string, error) {
	r.types[c.Node] = c.Type
	return r.reactor.Create(c)
}

var errTextRoot = errors.New("a Text cannot be the root")

func (r *rootVetter) VetRoot(n decl.NodeID) error {
	r.asked = append(r.asked, n)
	if r.types[n] == "Text" {
		return errTextRoot
	}
	return nil
}

var _ decl.RootVetter = (*rootVetter)(nil)

func TestARootTheAdapterRefusesFailsTheMount(t *testing.T) {
	a := newRootVetter()
	tr := decl.New(a)
	err := tr.Mount(mustSpec(t, `Text { text: "alone" }`))
	if !errors.Is(err, errTextRoot) || !errors.Is(err, decl.ErrAdapter) {
		t.Fatalf("Mount: err = %v, want the adapter's refusal", err)
	}
	if !tr.Failed() {
		t.Error("a refused root left the tree usable; it is a partial tree, for Destroy")
	}
	if err := tr.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if tr.Len() != 0 {
		t.Errorf("Destroy left %d nodes", tr.Len())
	}
}

func TestOnlyTheRootIsVetted(t *testing.T) {
	a := newRootVetter()
	tr := decl.New(a)
	if err := tr.Mount(mustSpec(t, `Flex { Text { text: "inside" } }`)); err != nil {
		t.Fatalf("a Text inside a Flex was refused: %v", err)
	}
	if len(a.asked) != 1 || a.asked[0] != tr.Root() {
		t.Errorf("asked about %v, want only the root %d", a.asked, tr.Root())
	}
}

func TestAReloadToARefusedRootLeavesTheLiveTree(t *testing.T) {
	a := newRootVetter()
	tr := decl.New(a)
	if err := tr.Mount(mustSpec(t, `Flex { Text { text: "live" } }`)); err != nil {
		t.Fatal(err)
	}
	root, n := tr.Root(), tr.Len()
	_, err := tr.Reconcile(mustSpec(t, `Text { text: "alone" }`))
	if !errors.Is(err, errTextRoot) {
		t.Fatalf("Reconcile: err = %v, want the refusal", err)
	}
	if tr.Failed() || tr.Root() != root || tr.Len() != n {
		t.Errorf("the live tree changed: failed %v, root %d (was %d), %d nodes (was %d)",
			tr.Failed(), tr.Root(), root, tr.Len(), n)
	}
}
