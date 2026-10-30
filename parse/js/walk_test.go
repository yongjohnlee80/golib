package js_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/parse/js"
)

// recursiveWalk is Walk as it was written before it took a stack: the order and the rewriting it
// promises, held up against the iterative one.
func recursiveWalk(e *js.Expr, fn func(*js.Expr) bool) {
	if e == nil || !fn(e) {
		return
	}
	recursiveWalk(e.Left, fn)
	recursiveWalk(e.Right, fn)
	recursiveWalk(e.Alt, fn)
	for i := range e.Args {
		recursiveWalk(&e.Args[i], fn)
	}
	for i := range e.Props {
		recursiveWalk(e.Props[i].KeyExpr, fn)
		recursiveWalk(&e.Props[i].Value, fn)
	}
}

func shown(e *js.Expr) string { return fmt.Sprintf("%d:%s%s", e.Kind, e.Raw, e.Name) }

// TestWalkVisitsAsTheRecursiveWalkDid: every node, parents first, in the recursive walk's order,
// and a node fn refuses is not walked into.
func TestWalkVisitsAsTheRecursiveWalkDid(t *testing.T) {
	for _, src := range []string{
		"a ? f(b, c.d)[e] : {k: v, [m]: n, o}",
		"`x${a + b}y${[c, d, e]}`",
		"-a.b(c)(d) || !e && (f ?? g)",
		"{a: {b: [c, {d: e}]}, [f(g)]: h ? i : j}",
	} {
		for _, prune := range []js.ExprKind{js.ExprInvalid, js.ExprMember, js.ExprCall, js.ExprObject} {
			e, err := js.NewExpression().Parse([]byte(src))
			if err != nil {
				t.Fatalf("%s: %v", src, err)
			}
			var got, want []string
			e.Walk(func(n *js.Expr) bool { got = append(got, shown(n)); return n.Kind != prune })
			recursiveWalk(&e, func(n *js.Expr) bool { want = append(want, shown(n)); return n.Kind != prune })
			if !slices.Equal(got, want) {
				t.Errorf("%s, not into kind %d:\n got %v\nwant %v", src, prune, got, want)
			}
		}
	}
}

// TestWalkIsWalkedIntoWhatAVisitorRewrote: Walk is for rewriting too, so a node's later children
// are read when the walk reaches them: a visitor at a that replaces a later sibling (a Right, or the
// Args or Props rebound to a fresh slice, or an element of them rewritten in place) is walked into
// the replacement c, never the node it replaced, as the recursive walk was.
func TestWalkIsWalkedIntoWhatAVisitorRewrote(t *testing.T) {
	id := func(s string) js.Expr { return js.Expr{Kind: js.ExprIdent, Raw: s} }
	prop := func(k string, v js.Expr) js.ExprProperty { return js.ExprProperty{Key: k, Value: v} }
	cases := []struct {
		name  string
		build func() (*js.Expr, func(*js.Expr))
	}{
		{"a Right replaced", func() (*js.Expr, func(*js.Expr)) {
			a, b, c := id("a"), id("b"), id("c")
			root := &js.Expr{Kind: js.ExprBinary, Left: &a, Right: &b}
			return root, func(*js.Expr) { root.Right = &c }
		}},
		{"the Args rebound", func() (*js.Expr, func(*js.Expr)) {
			root := &js.Expr{Kind: js.ExprArray, Args: []js.Expr{id("a"), id("b")}}
			return root, func(*js.Expr) { root.Args = []js.Expr{id("a"), id("c")} }
		}},
		{"an Arg rewritten in place", func() (*js.Expr, func(*js.Expr)) {
			root := &js.Expr{Kind: js.ExprArray, Args: []js.Expr{id("a"), id("b")}}
			return root, func(*js.Expr) { root.Args[1] = id("c") }
		}},
		{"the Props rebound", func() (*js.Expr, func(*js.Expr)) {
			root := &js.Expr{Kind: js.ExprObject, Props: []js.ExprProperty{prop("x", id("a")), prop("y", id("b"))}}
			return root, func(*js.Expr) { root.Props = []js.ExprProperty{prop("x", id("a")), prop("y", id("c"))} }
		}},
		{"a Prop's value rewritten in place", func() (*js.Expr, func(*js.Expr)) {
			root := &js.Expr{Kind: js.ExprObject, Props: []js.ExprProperty{prop("x", id("a")), prop("y", id("b"))}}
			return root, func(*js.Expr) { root.Props[1].Value = id("c") }
		}},
	}
	walkers := []struct {
		name string
		walk func(*js.Expr, func(*js.Expr) bool)
	}{
		{"Walk", func(e *js.Expr, fn func(*js.Expr) bool) { e.Walk(fn) }},
		{"recursive", recursiveWalk},
	}
	for _, c := range cases {
		for _, w := range walkers {
			t.Run(c.name+"/"+w.name, func(t *testing.T) {
				root, rewrite := c.build()
				var raws []string
				w.walk(root, func(n *js.Expr) bool {
					if n.Raw == "a" {
						rewrite(n)
					}
					if n.Raw != "" {
						raws = append(raws, n.Raw)
					}
					return true
				})
				if want := []string{"a", "c"}; !slices.Equal(raws, want) {
					t.Errorf("visited %v, want %v", raws, want)
				}
			})
		}
	}
}
