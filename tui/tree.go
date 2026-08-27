package tui

import (
	"context"
	"fmt"
	"reflect"
	"slices"
)

// node is the runtime's per-mount bookkeeping record (ADR-0004 §2.4). The
// authoritative table is App.nodes (map[NodeID]*node — every internal path
// is ID-keyed); App.byComp is the Component-keyed identity index serving the
// public Component-keyed API (Context.Unmount, Container.Remove,
// LayoutChild/PlaceChild). All fields are loop-goroutine-owned
// (ADR-0005 §2.3).
type node struct {
	id       NodeID
	comp     Component
	parent   *node
	children []*node // append order == document order == focus/paint order

	ctx    *Context
	cctx   context.Context    // derived from the parent node's context at mount
	cancel context.CancelFunc // unmount cancels; cascades to descendants for free
	hooks  []func()           // OnUnmount hooks, run LIFO at unmount

	mounted bool

	// Layout state (ADR-0004 §2.7). measured/placed reset each pass; a
	// node is visible this frame only when both are set with a non-empty
	// rect.
	rect     Rect // parent-relative, set by PlaceChild
	absRect  Rect // absolute, derived after the pass
	size     Size // clamped Layout return
	measured bool // Layout ran this pass
	placed   bool // PlaceChild ran this pass
}

// visible reports whether n was laid out in the current frame with a
// non-empty Rect — the render/hit-test/tab-stop condition (ADR-0004 §2.6.2).
func (n *node) visible() bool { return n.measured && n.placed && !n.rect.Empty() }

// mount allocates a NodeID, links the node under parent (nil = root),
// derives its context from the parent's, calls Init, and marks layout dirty
// (ADR-0004 §2.4 mount cascade). Loop goroutine only.
func (a *App) mount(parent *node, comp Component) *node {
	if comp == nil {
		panic("tui: Mount: nil component")
	}
	if a.inLayout || a.inRender {
		panic("tui: tree mutation (Mount) inside Layout/Render is illegal (ADR-0004 §2.1)")
	}
	// Identity contract (ADR-0004 §2.4 rev 1, Lector must-fix #4): the
	// Component-keyed index requires a comparable dynamic type; verify
	// eagerly with a targeted panic (precedent: server.NewScaffold's
	// nil-arg panic, server/scaffold.go:87-90).
	if !reflect.TypeOf(comp).Comparable() {
		panic(fmt.Sprintf("tui: component type %T is not comparable; use a pointer component", comp))
	}
	if _, dup := a.byComp[comp]; dup {
		panic(fmt.Sprintf("tui: component %T is already mounted; a component value mounts at most once (ADR-0004 §2.4)", comp))
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

	// Register the async-visible task context (ADR-0005 §2.8: App.Go is
	// callable from any goroutine, so this lookup table has its own lock).
	a.async.mu.Lock()
	a.async.ctxs[id] = cctx
	a.async.mu.Unlock()

	// Init may itself Mount children — the cascade is depth-first and
	// re-entrant (ADR-0004 §2.4 step 3).
	comp.Init(n.ctx)

	a.layoutDirty = true // a new child means geometry may change (step 4)
	a.queue.wakeUp()
	return n
}

// moveWithin (ADR-0011 §2.3) repositions child — a mounted DIRECT child
// of parent — to
// index to in the parent's children slice. A splice only: unlike
// unmountTree + mount it preserves everything the node owns — NodeID
// (addressed deliveries stay routable, ADR-0005 §2.8), the derived
// context (in-flight tasks keep running), OnUnmount hooks (none fire),
// and focus/scope-stack membership; Init is NOT re-run. Focus needs no
// repair: the node stays registered in a.nodes, so a dangling focus ID
// is impossible by construction. Loop goroutine only.
func (a *App) moveWithin(parent *node, child Component, to int) {
	if a.inLayout || a.inRender {
		panic("tui: tree mutation (Move) inside Layout/Render is illegal (ADR-0004 §2.1)")
	}
	n := a.byComp[child]
	if n == nil {
		panic(fmt.Sprintf("tui: Move: component %T is not mounted", child))
	}
	if n.parent != parent {
		panic("tui: Move: child belongs to a different container; cross-container moves are not supported")
	}
	cs := parent.children
	if to < 0 || to >= len(cs) {
		panic(fmt.Sprintf("tui: Move: index %d out of range [0,%d)", to, len(cs)))
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
// parent, and performs focus repair / scope restore so no frame ever renders
// with a dangling focus ID (ADR-0004 §2.4, §2.6.4).
func (a *App) unmountTree(n *node) {
	if a.inLayout || a.inRender {
		panic("tui: tree mutation (Unmount) inside Layout/Render is illegal (ADR-0004 §2.1)")
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
	a.layoutDirty = true // ADR-0004 §2.4 step 5
	a.queue.wakeUp()

	// Scope restore + focus repair (ADR-0004 §2.6.3/§2.6.4). Pop scope
	// entries whose trapping scope died; the innermost pop supplies the
	// restore target.
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
// depth-first, in reverse document order (ADR-0004 §2.4): cancel the node
// context (in-flight tasks die), run OnUnmount hooks LIFO (mirroring the
// deliberate defer stacking of server/ws/ws.go:230-232), then remove from
// the node tables — from that instant addressed deliveries dead-letter
// (ADR-0005 §2.8).
func (a *App) unmountNode(n *node) {
	a.trace(TraceEvent{Kind: TraceUnmount, Node: n.id})
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
