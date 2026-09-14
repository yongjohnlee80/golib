package tui

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/yongjohnlee80/golib/errs"
)

// mount allocates a NodeID, links the node under parent (nil = root),
// derives its context from the parent's, calls Init, and marks layout dirty
// (the mount cascade). Loop goroutine only.
func (a *App) mount(parent *node, comp Component) *node {
	if comp == nil {
		panic(errs.Fatal{Op: "tui: Mount", Rule: "nil component"})
	}
	if a.inLayout || a.inRender {
		panic("tui: tree mutation (Mount) inside Layout/Render is illegal")
	}
	// The Component-keyed index requires a COMPARABLE dynamic type, so verify
	// it eagerly with a targeted panic. Deferring the check means the failure
	// surfaces as a map-assignment panic somewhere else entirely, naming
	// neither the component nor the rule it broke.
	// REFERENCE: server/scaffold.go
	if !reflect.TypeOf(comp).Comparable() {
		panic(errs.Fatal{Op: "tui", Rule: fmt.Sprintf("component type %T is not comparable; use a pointer component", comp)})
	}
	if _, dup := a.byComp[comp]; dup {
		panic(errs.Fatal{Op: "tui", Rule: fmt.Sprintf("component %T is already mounted; a component value mounts at most once", comp)})
	}

	a.nextNodeID++ // monotonic, starts at 1; 0 reserved as "no node"; never reused
	id := NodeID(a.nextNodeID)

	pctx := a.runCtx
	if parent != nil {
		pctx = parent.cctx
	}
	if pctx == nil {
		pctx = context.Background()
	}
	cctx, cancel := context.WithCancel(pctx)

	n := &node{id: id, comp: comp, parent: parent, cctx: cctx, cancel: cancel, mounted: true}
	n.ctx = &Context{app: a, node: n}
	a.nodes[id] = n
	a.byComp[comp] = n
	if parent != nil {
		parent.children = append(parent.children, n)
	} else {
		a.rootNode = n
	}

	// Register the async-visible task context. App.Go is callable from any
	// goroutine, so this lookup table has its own lock — it is the one piece
	// of node state that is NOT loop-goroutine-owned.
	a.async.mu.Lock()
	a.async.ctxs[id] = cctx
	a.async.mu.Unlock()

	// Init may itself Mount children — the cascade is depth-first and
	// re-entrant. The marker is saved and restored rather than cleared for
	// exactly that reason: a child's Init runs inside its parent's, and
	// clearing on the inner return would leave the parent's remaining Init
	// wrongly classified as no longer initialising.
	prevInit := a.initNode
	a.initNode = n.id
	comp.Init(n.ctx)
	a.initNode = prevInit

	a.layoutDirty = true // a new child means geometry may change (step 4)
	a.queue.wakeUp()
	return n
}

// moveWithin repositions child — a mounted DIRECT child of parent — to
// index to in the parent's children slice. A splice only: unlike
// unmountTree + mount it preserves everything the node owns — NodeID
// (addressed deliveries stay routable), the derived context (in-flight
// tasks keep running), OnUnmount hooks (none fire), and focus/scope-stack
// membership; Init is NOT re-run. Focus needs no repair: the node stays
// registered in a.nodes, so a dangling focus ID is impossible by
// construction. Loop goroutine only.
func (a *App) moveWithin(parent *node, child Component, to int) {
	if a.inLayout || a.inRender {
		panic(errs.Fatal{Op: "tui", Rule: "tree mutation (Move) inside Layout/Render is illegal"})
	}
	n := a.byComp[child]
	if n == nil {
		panic(errs.Fatal{Op: "tui: Move", Rule: fmt.Sprintf("component %T is not mounted", child)})
	}
	if n.parent != parent {
		panic(errs.Fatal{Op: "tui: Move", Rule: "child belongs to a different container; cross-container moves are not supported"})
	}
	cs := parent.children
	if to < 0 || to >= len(cs) {
		panic(errs.Fatal{Op: "tui: Move", Rule: fmt.Sprintf("index %d out of range [0,%d)", to, len(cs))})
	}
	if from := slices.Index(cs, n); from != to {
		// Post-move-index semantics: remove, then insert at to.
		cs = slices.Insert(slices.Delete(cs, from, from+1), to, n)
		parent.children = cs
	}

	a.layoutDirty = true // document order changed: paint + tab order may change
	a.queue.wakeUp()
}

// unmountTree is the top-level unmount entry (Context.Unmount,
// Container.Remove, App teardown): it runs the cascade, detaches from the
// parent, and performs focus repair / scope restore so no frame ever
// renders with a dangling focus ID.
func (a *App) unmountTree(n *node) {
	if a.inLayout || a.inRender {
		panic(errs.Fatal{Op: "tui", Rule: "tree mutation (Unmount) inside Layout/Render is illegal"})
	}
	focusedBefore := a.focused
	parent := n.parent

	a.unmountNode(n)

	if parent != nil {
		for i, ch := range parent.children {
			if ch == n {
				parent.children = append(parent.children[:i], parent.children[i+1:]...)
				break
			}
		}
	} else if a.rootNode == n {
		a.rootNode = nil
	}
	a.layoutDirty = true
	a.queue.wakeUp()

	// Scope restore + focus repair. Pop scope entries whose trapping scope
	// died; the innermost pop supplies the restore target.
	restore := NodeID(0)
	hadDeadScope := false
	for len(a.scopeStack) > 0 {
		top := a.scopeStack[len(a.scopeStack)-1]
		if _, alive := a.nodes[top.scope]; alive {
			break
		}
		restore = top.restore
		hadDeadScope = true
		a.scopeStack = a.scopeStack[:len(a.scopeStack)-1]
	}

	if focusedBefore == 0 {
		return // nothing was focused; unmount cannot steal focus
	}
	if _, alive := a.nodes[focusedBefore]; alive {
		return // focus survived the unmount
	}
	// The focused node died with the subtree. Try the scope-stack restore
	// target first (open-modal → close-modal → focus-where-you-were),
	// then repair within the innermost surviving scope.
	a.focused = 0
	if hadDeadScope && restore != 0 {
		if rn := a.nodes[restore]; rn != nil {
			if f, ok := rn.comp.(Focusable); ok && f.AcceptsFocus() {
				a.setFocus(restore)
				return
			}
		}
	}
	a.repairFocus()
}

// unmountNode runs the unmount cascade for n's subtree — children first,
// depth-first, in reverse document order: cancel the node context
// (in-flight tasks die), run OnUnmount hooks LIFO (mirroring the deliberate
// defer stacking of server/ws/ws.go:230-232), then remove from the node
// tables — from that instant addressed deliveries dead-letter.
func (a *App) unmountNode(n *node) {
	a.trace(TraceEvent{Kind: TraceUnmount, Node: n.id})
	// Tell a capture owner it has lost the pointer while it is still mounted
	// and still legally callable. Everything below this line is the teardown
	// past which no component method may be invoked, so a notification issued
	// any later would be a call into an unmounted component.
	a.captureLostOnUnmount(n)
	for i := len(n.children) - 1; i >= 0; i-- {
		a.unmountNode(n.children[i])
	}
	n.children = nil

	n.cancel()
	for i := len(n.hooks) - 1; i >= 0; i-- {
		h := n.hooks[i]
		n.hooks[i] = nil
		h()
	}
	n.hooks = nil
	n.mounted = false

	delete(a.nodes, n.id)
	delete(a.byComp, n.comp)
	a.async.mu.Lock()
	delete(a.async.ctxs, n.id)
	a.async.mu.Unlock()
}
