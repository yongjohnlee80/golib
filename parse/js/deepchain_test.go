package js_test

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/js"
)

// TestALongChainIsWalkedWithoutRecursion: a left-deep chain (a.b.c…, a+b+c…) is as deep as it is
// long, and the parser builds it in a loop, past the depth budget, so Walk must not recurse on it:
// under a small stack, a 200,000-link chain is walked whole, each node once (GS-19).
func TestALongChainIsWalkedWithoutRecursion(t *testing.T) {
	defer debug.SetMaxStack(debug.SetMaxStack(8 << 20)) // a recursive Walk overflows this, fatally
	const n = 200_000
	for _, src := range []string{"a" + strings.Repeat(".b", n), "a" + strings.Repeat("+b", n)} {
		e, err := js.NewExpression().Parse([]byte(src))
		if err != nil {
			t.Fatalf("%.10s…: %v", src, err)
		}
		nodes := 0
		e.Walk(func(*js.Expr) bool { nodes++; return true })
		if want := n + 1 + strings.Count(src, "+"); nodes != want {
			t.Errorf("%.10s…: walked %d nodes", src, nodes)
		}
	}
}
