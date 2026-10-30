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
// are read when the walk reaches them: a visitor that replaces a later sibling (a Right, an Arg, a
// Prop's value) is walked into the replacement, never the node it replaced.
func TestWalkIsWalkedIntoWhatAVisitorRewrote(t *testing.T) {
	id := func(s string) js.Expr { return js.Expr{Kind: js.ExprIdent, Raw: s} }
	for name, build := range map[string]func() (*js.Expr, func(*js.Expr)){
		"a Right": func() (*js.Expr, func(*js.Expr)) {
			a, b := id("a"), id("b")
			root := &js.Expr{Kind: js.ExprBinary, Left: &a, Right: &b}
			c := id("c")
			return root, func(n *js.Expr) {
				if n.Raw == "a" {
					root.Right = &c
				}
			}
		},
		"an Arg": func() (*js.Expr, func(*js.Expr)) {
			root := &js.Expr{Kind: js.ExprArray, Args: []js.Expr{id("a"), id("b")}}
			return root, func(n *js.Expr) {
				if n.Raw == "a" {
					root.Args[1] = id("c")
				}
			}
		},
		"a Prop's value": func() (*js.Expr, func(*js.Expr)) {
			root := &js.Expr{Kind: js.ExprObject, Props: []js.ExprProperty{{Key: "x", Value: id("a")}, {Key: "y", Value: id("b")}}}
			return root, func(n *js.Expr) {
				if n.Raw == "a" {
					root.Props[1].Value = id("c")
				}
			}
		},
	} {
		for walker, walk := range map[string]func(*js.Expr, func(*js.Expr) bool){
			"Walk": func(e *js.Expr, fn func(*js.Expr) bool) { e.Walk(fn) }, "the recursive walk": recursiveWalk,
		} {
			root, rewrite := build()
			var raws []string
			walk(root, func(n *js.Expr) bool {
				rewrite(n)
				if n.Raw != "" {
					raws = append(raws, n.Raw)
				}
				return true
			})
			if want := []string{"a", "c"}; !slices.Equal(raws, want) {
				t.Errorf("%s rewritten, %s visited %v, want %v", name, walker, raws, want)
			}
		}
	}
}
