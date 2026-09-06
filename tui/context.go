package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/yongjohnlee80/golib/errs"
)

// Context is a mounted component's identity and its ONLY sanctioned channel
// to the runtime. There are no globals to reach it by, deliberately: a
// component that could find the App without being handed it could also be
// used outside one, and the compiler would not say so. One *Context per
// mounted node,
// created at mount, invalidated at unmount.
//
// Methods are legal only on the loop goroutine except Post and Go, which are
// safe from any goroutine, because they only hand work to the App rather than
// touching the tree.
type Context struct {
	app  *App
	node *node
}

// ID returns the node's identity: stable for this mount; 0 is never
// assigned; never reused for the App's lifetime.
func (c *Context) ID() NodeID { return c.node.id }

// Ctx returns the node's lifetime context, cancelled when this node
// unmounts. It is the async-lifetime anchor: task contexts derive from it,
// so unmount kills in-flight work with zero bookkeeping in the component.
func (c *Context) Ctx() context.Context { return c.node.cctx }

// MarkDirty requests a repaint of the subtree, leaving geometry unchanged.
//
// It only SCHEDULES a frame; rendering never happens synchronously inside a
// handler. That is what lets a handler mark dirty as many times as it likes,
// and what stops a component from observing a half-updated tree mid-handler.
func (c *Context) MarkDirty() {
	c.app.renderDirty = true
	c.app.queue.wakeUp()
}

// RequestLayout signals the node's size may have changed: the next frame
// runs one full layout pass from the root, then repaints. Use it when a
// change affects SIZE; MarkDirty is enough when only appearance changed.
func (c *Context) RequestLayout() {
	c.app.layoutDirty = true
	c.app.queue.wakeUp()
}

// RequestFocus asks the focus manager to focus this node. Ignored unless
// the component implements Focusable and accepts focus.
func (c *Context) RequestFocus() { c.app.requestFocus(c.node) }

// FocusWithin reports whether the currently focused node is comp or one of
// comp's descendants — what a container asks before it hides a subtree, so
// focus can be moved out rather than stranded somewhere invisible. False when
// nothing is focused or comp is not mounted.
func (c *Context) FocusWithin(comp Component) bool {
	n := c.app.nodes[c.app.focused]
	for ; n != nil; n = n.parent {
		if n.comp == comp {
			return true
		}
	}
	return false
}

// FocusComponent moves focus to another already-mounted component — the
// cross-node analogue of RequestFocus, which can only focus the calling node.
// A controller uses it to hand focus to a specific child (a form field, a
// content panel). Returns false if comp exposes no node identity, is not
// mounted, or is not focusable. Loop goroutine only.
func (c *Context) FocusComponent(comp Component) bool {
	if comp == nil {
		return false
	}
	// Fast path: widgets embed Base, which exposes the mount's node id.
	if ider, ok := comp.(interface{ NodeID() NodeID }); ok {
		if id := ider.NodeID(); id != 0 {
			return c.app.requestFocusByID(id)
		}
	}
	// Fallback: reverse-lookup the node by identity so any Component works.
	for id, n := range c.app.nodes {
		if n.comp == comp {
			return c.app.requestFocusByID(id)
		}
	}
	return false
}

// Focused reports whether this node currently holds focus.
func (c *Context) Focused() bool { return c.app.focused == c.node.id }

// OnUnmount registers a cleanup hook; hooks run LIFO at unmount.
// SubscribeScoped and After/Every register their cancels through it.
// Registering on an already-unmounted context runs fn immediately.
func (c *Context) OnUnmount(fn func()) {
	if fn == nil {
		return
	}
	if !c.node.mounted {
		fn()
		return
	}
	c.node.hooks = append(c.node.hooks, fn)
}

// Mount mounts child under this node. Loop goroutine only; illegal inside
// Layout/Render.
func (c *Context) Mount(child Component) {
	if !c.node.mounted {
		panic(errs.Fatal{Op: "tui", Rule: fmt.Sprintf("Context.Mount on unmounted node %d", c.node.id)})
	}
	c.app.mount(c.node, child)
}

// Unmount runs the unmount cascade for child. Loop goroutine only; illegal
// inside Layout/Render.
func (c *Context) Unmount(child Component) {
	n := c.app.byComp[child]
	if n == nil {
		panic(errs.Fatal{Op: "tui: Context.Unmount", Rule: fmt.Sprintf("component %T is not mounted", child)})
	}
	c.app.unmountTree(n)
}

// Move repositions child — a mounted direct child of this node — to
// index to in document order WITHOUT unmounting it: NodeID, context,
// in-flight tasks, hooks, and focus survive; Init does not re-run (see
// App.moveWithin). Loop goroutine only; illegal inside Layout/Render.
func (c *Context) Move(child Component, to int) {
	if !c.node.mounted {
		panic(errs.Fatal{Op: "tui", Rule: fmt.Sprintf("Context.Move on unmounted node %d", c.node.id)})
	}
	c.app.moveWithin(c.node, child, to)
}

// LayoutChild lays out a mounted child under cc and returns its chosen
// (clamped) size. Legal ONLY inside this component's Layout call.
func (c *Context) LayoutChild(child Component, cc Constraints) Size {
	a := c.app
	if a.layingOut != c.node {
		panic(errs.Fatal{Op: "tui", Rule: "Context.LayoutChild is legal only inside this component's Layout (ADR-0004 §2.2)"})
	}
	cn := a.byComp[child]
	if cn == nil || cn.parent != c.node {
		panic(errs.Fatal{Op: "tui: LayoutChild", Rule: fmt.Sprintf("%T is not a mounted child of %T", child, c.node.comp)})
	}
	return a.layoutComponent(cn, cc)
}

// PlaceChild positions a laid-out child at the parent-relative Rect r.
// Legal ONLY inside this component's Layout call.
func (c *Context) PlaceChild(child Component, r Rect) {
	a := c.app
	if a.layingOut != c.node {
		panic(errs.Fatal{Op: "tui", Rule: "Context.PlaceChild is legal only inside this component's Layout (ADR-0004 §2.2)"})
	}
	cn := a.byComp[child]
	if cn == nil || cn.parent != c.node {
		panic(errs.Fatal{Op: "tui: PlaceChild", Rule: fmt.Sprintf("%T is not a mounted child of %T", child, c.node.comp)})
	}
	cn.rect = r
	cn.placed = true
}

// App returns the owning runtime.
func (c *Context) App() *App { return c.app }

// StringWidth measures s under the App's active width policy — the SAME
// policy Surface.StringWidth applies (WithWidthPolicy). It is the
// policy-aware measurement surface available OUTSIDE Render (Layout, event
// handlers, cursor/scroll/wrap/hit-test math), where there is no Surface.
// NORMATIVE: component layout and state math MUST measure through this (or
// Surface.StringWidth in Render), never the package-level tui.StringWidth
// default, so a per-App WidthPolicyAmbiguousWide stays consistent between
// paint and geometry.
func (c *Context) StringWidth(s string) int {
	return StringWidthPolicy(s, c.app.widthPolicy())
}

// Post enqueues ev on the program lane. Safe from any goroutine.
func (c *Context) Post(ev Event) { c.app.Post(ev) }

// Go schedules task on the App's bounded pool with this node as owner: the
// TaskResult is addressed to this node's HandleEvent, and the task context
// derives from Ctx() so unmount cancels it. Safe from any goroutine.
func (c *Context) Go(task Task, opts ...TaskOption) TaskID {
	return c.app.Go(c.node.id, task, opts...)
}

// Bus returns the App's broadcast bus.
func (c *Context) Bus() *Bus { return c.app.bus }

// After registers a one-shot timer: after d, a TickEvent addressed to this
// node is delivered. The returned cancel is idempotent; unmount cancels
// automatically.
func (c *Context) After(d time.Duration) (cancel func()) {
	cancel = c.app.addTimer(c.node.id, d, 0)
	c.OnUnmount(cancel)
	return cancel
}

// Every registers a repeating timer: a TickEvent addressed to this node
// every d, re-armed after delivery (fixed-delay, not fixed-rate — no burst
// catch-up after a stall, so a slow frame cannot be followed by a burst of
// backdated ticks). The returned cancel is idempotent;
// unmount cancels automatically.
func (c *Context) Every(d time.Duration) (cancel func()) {
	if d <= 0 {
		panic(errs.Fatal{Op: "tui: Context.Every", Rule: "non-positive interval"})
	}
	cancel = c.app.addTimer(c.node.id, d, d)
	c.OnUnmount(cancel)
	return cancel
}
