package qml_test

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/js"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// TestALongMemberChainProjectsWithoutRecursion: a property's dotted path is read down the chain in
// a loop and joined once, so a 200,000-link a.b.c… projects under a small stack.
func TestALongMemberChainProjectsWithoutRecursion(t *testing.T) {
	defer debug.SetMaxStack(debug.SetMaxStack(8 << 20))
	const n = 200_000
	e, err := js.NewExpression().Parse([]byte("a" + strings.Repeat(".b", n)))
	if err != nil {
		t.Fatal(err)
	}
	v := qml.ProjectValue(&e)
	if v.Kind != qml.SpecValueRef || len(v.Path) != n+1 || v.Path[0] != "a" || !strings.HasPrefix(v.Raw, "a.b.b") || strings.Count(v.Raw, ".") != n {
		t.Errorf("the path projected as %v %.40s… (%d segments)", v.Kind, v.Raw, len(v.Path))
	}
}
