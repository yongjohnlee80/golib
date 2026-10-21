package tui

import (
	"fmt"
	"slices"

	"github.com/yongjohnlee80/golib/errs"
)

// Reconcile makes next c's children, in next's order, keeping the mount of each child c holds
// whose key is in next: that child is moved, not remounted, so its NodeID, lifetime Context,
// tasks, timers and focus survive and Init does not run again. The held child is kept and next's
// value with the same key is dropped. A child of next with a new key is added, and a held child
// whose key is not in next is removed. Every change goes through c's own Add, Remove and Move, so
// a container's per-child records follow (a Dock adds center children).
//
// key gives a child's identity: comparable, and unique within next. A nil key is the child value
// itself. A key repeated in next is a programming error and panics before c changes. Loop
// goroutine only, like the Container calls it makes.
func Reconcile(c Container, next []Component, key func(Component) any) {
	if key == nil {
		key = func(child Component) any { return child }
	}
	want := make(map[any]int, len(next))
	for i, n := range next {
		if n == nil {
			panic(errs.Fatal{Op: "tui: Reconcile", Rule: fmt.Sprintf("next[%d] is nil", i)})
		}
		k := key(n)
		if j, dup := want[k]; dup {
			panic(errs.Fatal{Op: "tui: Reconcile", Rule: fmt.Sprintf("next[%d] and next[%d] share the key %v", j, i, k)})
		}
		want[k] = i
	}
	held := make(map[any]Component)
	for _, h := range slices.Collect(c.Children()) {
		k := key(h)
		if _, keep := want[k]; keep && held[k] == nil {
			held[k] = h
			continue
		}
		c.Remove(h)
	}
	for i, n := range next {
		child, ok := held[key(n)]
		if !ok {
			child = n
			c.Add(child)
		}
		c.Move(child, i)
	}
}
