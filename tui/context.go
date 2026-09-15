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
// mounted node, created at mount, invalidated at unmount.
//
// Context is deliberately a concrete struct rather than an interface: it encapsulates
// unexported runtime pointers (*App, *node) and is passed directly by pointer to
// Component.Init(*Context). It does NOT implement context.Context; rather, it exposes
// the standard async lifetime context via Ctx().
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

// Mounted reports whether this context's node is still in the tree.
//
// A widget REMEMBERS its Context after the runtime has forgotten the node —
// widget.Base keeps the pointer so that MarkDirty and friends stay safe to call
// at any time — so "has a Context" is emphatically not "is mounted". Anything
// that needs the difference has to ask, and before this there was nothing to
// ask: a container checking a child's mount state could only test the retained
// pointer, which answers a different question and answers it wrongly for every
// component that has ever been mounted.
func (c *Context) Mounted() bool { return c.node.mounted }

// ParentIs reports whether this node's direct parent is comp.
//
// It answers the one question a container has to settle before adopting a
// component it did not create: is this already mounted somewhere ELSE? A
// container can tell "mounted" from "not mounted" on its own, but not "mine"
// from "someone else's" — and the difference decides between an ordinary
// reorder of a child it already owns and a mount that will panic partway
// through a reconcile.
//
// The obvious substitute is for the container to consult its own list of
// children, which is a proxy for parentage rather than parentage: the two agree
// until the list and the tree disagree, which is exactly the situation such a
// check exists to detect.
//
// Reports false for an unmounted node, for the root, and for a nil comp.
func (c *Context) ParentIs(comp Component) bool {
	if comp == nil || !c.node.mounted || c.node.parent == nil {
		return false
	}
	return c.node.parent.comp == comp
}

// Ancestor returns the nearest ancestor component for which match reports true,
// or nil when no ancestor matches.
//
// WHY THE RUNTIME OWNS THIS. A widget frequently needs the one container that
// will act on its behalf — the overlay host that must mount its popup, the
// scroller that must bring it into view. The alternatives are all worse. A
// broadcast on the bus has no addressee, so EVERY host in the application
// answers a request that belongs to one of them: the containing host succeeds
// and the rest refuse a request that was never theirs, which turns a correct
// operation into a stream of false failures. Making the collaborator a
// constructor parameter pushes a wiring detail the tree already knows into every
// call site, and it goes stale the moment the widget is re-parented.
//
// It walks PARENTS only, never children, so a match is an enclosing scope by
// construction and the answer cannot depend on document order among siblings.
// The nearest one wins, which is what makes nested hosts work: an inner host
// serves the widgets inside it without the outer one ever hearing about them.
//
// The search starts at this node's PARENT: a component is not its own ancestor,
// so a container asking for the nearest container of its own kind gets the one
// enclosing it rather than itself.
//
// Returns nil for an unmounted node and for a nil match. The result is live tree
// state and must not be retained across mutations — ask again rather than
// caching, because the cheap walk is what keeps the answer true.
func (c *Context) Ancestor(match func(Component) bool) Component {
	if match == nil || !c.node.mounted {
		return nil
	}
	for n := c.node.parent; n != nil; n = n.parent {
		if n.comp != nil && match(n.comp) {
			return n.comp
		}
	}
	return nil
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

// BatchTreeMutation runs fn as one tree mutation, deferring focus repair and
// focusability revalidation until it returns and then performing exactly one.
//
// WHY A COMPOSITION NEEDS THIS. Replacing a dialog's three buttons with two is
// one change to the caller and five to the runtime: three unmounts and two
// mounts, each of which repairs focus against a list that is half of neither the
// old set nor the new one. Focus lands wherever the last intermediate state
// happened to put it, which is not a state anybody asked for — and a widget
// cannot fix that itself, because the repair is the runtime's and runs inside
// each Unmount. Sequencing the calls differently does not help; the only fix is
// a boundary, and one runtime primitive gives it to every composition rather
// than to one widget.
//
// WHAT IS DEFERRED, precisely: focus repair and focusability revalidation, and
// nothing else. Capture loss, OnUnmount hooks and lifetime-context cancellation
// still happen as they always did, synchronously, inside each Unmount. They are
// safety-critical — a deferred capture release strands the pointer on a dead
// node — so the guarantee here is deliberately narrower than "no event can
// observe a half-applied tree", which would have been easier to state and
// impossible to keep.
//
// WHAT A CALLER MAY RELY ON: one run-loop mutation with no input dispatch and no
// render interleaved, and exactly one focus repair at the end.
//
// Nested calls are counted and only the outermost is a boundary. A panic inside
// fn cannot strand the runtime in batching mode: the depth is restored and the
// pending repair runs on the way out, before the panic continues to propagate.
func (c *Context) BatchTreeMutation(fn func()) {
	if fn == nil {
		return
	}
	a := c.app
	if a.inLayout || a.inRender {
		panic(errs.Fatal{
			Op:   "tui: Context.BatchTreeMutation",
			Rule: "tree mutation inside Layout/Render is illegal",
		})
	}
	a.batchDepth++
	defer func() {
		a.batchDepth--
		if a.batchDepth > 0 {
			return // an inner call is not a boundary
		}
		if !a.batchRepair {
			return
		}
		a.batchRepair = false
		// The same revalidation the immediate path runs, so deferring cannot
		// change what "repaired" means. Running it here rather than after fn()
		// is what makes the boundary hold on the panic path too.
		a.revalidateFocus()
	}()
	fn()
}

// LayoutChild lays out a mounted child under cc and returns its chosen
// (clamped) size. Legal ONLY inside this component's Layout call.
func (c *Context) LayoutChild(child Component, cc Constraints) Size {
	a := c.app
	if a.layingOut != c.node {
		panic(errs.Fatal{Op: "tui", Rule: "Context.LayoutChild is legal only inside this component's Layout"})
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
		panic(errs.Fatal{Op: "tui", Rule: "Context.PlaceChild is legal only inside this component's Layout"})
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

// Post enqueues ev for delivery on Lane B (the Program Lane).
//
// # Lane & Dispatch Semantics
//
//   - Lane B (Program Queue): ev enters the non-blocking, unbounded-by-default
//     program queue (see queue.go). Program-lane events are never dropped due
//     to input overflow and never block the sender.
//   - Concurrency: Safe to call from ANY goroutine — including external watchers,
//     background network loops, and component handlers on the loop itself.
//   - Delivery Target: ev is delivered on the loop goroutine through standard
//     event routing, bubbling or targeting the active component tree.
//
// # Example Usage
//
//	// In a background goroutine, file watcher, or network listener:
//	go func() {
//	    data, err := loadData()
//	    if err != nil {
//	        ctx.Post(ErrorEvent{Err: err})
//	        return
//	    }
//	    ctx.Post(DataLoadedEvent{Payload: data})
//	}()
//
//	// Handled in the component's HandleEvent:
//	func (c *MyComponent) HandleEvent(ev tui.Event) bool {
//	    switch e := ev.(type) {
//	    case DataLoadedEvent:
//	        c.data = e.Payload
//	        c.ctx.MarkDirty()
//	        return true
//	    case ErrorEvent:
//	        c.err = e.Err
//	        c.ctx.MarkDirty()
//	        return true
//	    }
//	    return false
//	}
func (c *Context) Post(ev Event) { c.app.Post(ev) }

// Go schedules task on the App's background task runner, binding this
// component node as the task owner.
//
// # Lane & Lifecycle Semantics
//
//   - Concurrency & Execution: Each submission starts a separate goroutine, while
//     a semaphore limits concurrently executing task functions (default 16,
//     configurable via WithTaskPoolSize).
//   - Context & Cancellation: The context.Context passed to task derives from
//     c.Ctx() (the node's lifetime context) and the App's run context. If the
//     component unmounts before the task completes, ctx is cancelled immediately.
//   - Lane B Completion (TaskResult): When task returns or panics, the runtime
//     synthesizes a TaskResult{ID, Value, Err} and enqueues it into Lane B (the
//     Program Lane).
//   - Direct Node Addressing: The resulting TaskResult is addressed strictly to
//     this component node's HandleEvent, bypassing normal bubbling and leaf-focus
//     routing. If the node was unmounted while the task was running, delivery is
//     safely skipped.
//   - Concurrency: Safe to call from any goroutine, though most commonly initiated
//     inside Component.Init or HandleEvent.
//
// # Options
//
//   - Exclusive(group): Cancels any in-flight task on this node sharing the same
//     group name before launching the new one (e.g. preempting stale search queries).
//
// # Example Usage
//
//	// Trigger an asynchronous fetch on button click or query change:
//	taskID := ctx.Go(func(tctx context.Context) (any, error) {
//	    req, err := http.NewRequestWithContext(tctx, "GET", "https://api.example.com/items", nil)
//	    if err != nil {
//	        return nil, err
//	    }
//	    resp, err := http.DefaultClient.Do(req)
//	    if err != nil {
//	        return nil, err
//	    }
//	    defer resp.Body.Close()
//	    return io.ReadAll(resp.Body)
//	}, tui.Exclusive("search"))
//
//	// Handled on the loop goroutine in HandleEvent:
//	func (c *MyComponent) HandleEvent(ev tui.Event) bool {
//	    if res, ok := ev.(tui.TaskResult); ok {
//	        if res.Err != nil {
//	            c.err = res.Err
//	        } else {
//	            c.items = parseItems(res.Value.([]byte))
//	        }
//	        c.ctx.MarkDirty()
//	        return true
//	    }
//	    return false
//	}
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
