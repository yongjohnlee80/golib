package decl

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// A Build's Eval evaluates through the adapter's tree, and an adapter with no tree yet (built before
// its tree, or alone, as a builder test makes one) says so rather than dereferencing nil.
func TestABuildsEvalWithoutATreeSaysSo(t *testing.T) {
	a := New(StdRegistry())
	b := a.newBuild(decl.Construction{Type: "TableView"}, nil, nil, childSet{}, map[string]bool{})
	if _, err := b.Eval(qml.SpecValue{}, nil); err == nil || !strings.Contains(err.Error(), "no tree evaluates") {
		t.Errorf("Eval with no tree: %v; want refused", err)
	}
}
