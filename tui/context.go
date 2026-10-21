package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/style"
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
func (c *Context) FocusWithin(comp Component) bool { return c.app.FocusWithin(comp) }

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

// FocusInto moves focus INTO comp — Qt's forceActiveFocus(): comp itself when
// it takes focus, else its focus child. The focus child is the one comp names as
// an InitialFocusProvider, validated as a focus repair validates it (inside
// comp, live), else the first component in comp's subtree, in document order,
// that takes focus now — so a pane, a frame or a form is focused by naming it,
// whatever its focusable part is. A hidden subtree, or one a focus trap keeps
// out, takes none. A subtree shown in this same turn, not laid out yet, takes
// it after the next layout — unless focus moves elsewhere first — as Qt's
// forceActiveFocus takes an item being shown. Returns whether focus is now
// within comp. Loop goroutine only.
func (c *Context) FocusInto(comp Component) bool { return c.app.FocusInto(comp) }

// HoldsFocusable reports whether comp, or anything under it, takes focus BY
// DESIGN — implements Focusable — whatever it accepts right now. It is the
// question to ask before FocusInto when the answer "no" means a mistake: a
// Text, a status line or a gauge is not focusable by design, and asking to
// focus one is an error, where a disabled button or a hidden pane is only not
// focusable NOW. False when comp is not mounted.
func (c *Context) HoldsFocusable(comp Component) bool {
	holds, _ := c.app.HoldsFocusable(comp)
	return holds
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

// Children returns comp's mounted children, in document order — the order Tab
// walks them. nil for a comp that is not mounted or has none. It is how a
// walk over components reaches the children of a composite that mounts them
// itself without listing them (a declarative adapter's node, say): the tree
// is where they are, whatever the composite exposes. Live tree state; ask
// again rather than retaining it.
func (c *Context) Children(comp Component) []Component {
	n := c.app.byComp[comp]
	if n == nil || !n.mounted {
		return nil
	}
	out := make([]Component, 0, len(n.children))
	for _, ch := range n.children {
		out = append(out, ch.comp)
	}
	return out
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

// App returns the owning runtime: the handle for what is the App's alone (its options, Update from
// another goroutine). A widget's on-loop questions are the Context's own (Keymap, Capabilities,
// CellPixels, Update).
func (c *Context) App() *App { return c.app }

// Keymap is the App's key map; nil for a nil Context (a widget not mounted) or an App without one.
func (c *Context) Keymap() *keymap.Map {
	if c == nil || c.app == nil {
		return nil
	}
	return c.app.Keymap()
}

// KeyResolver is the Context's key question alone (KeyCommand), for a key helper to take rather
// than the whole Context. *Context is one, a nil one included.
type KeyResolver interface {
	KeyCommand(ev KeyEvent, cmds ...keymap.Command) (keymap.Command, bool)
}

var _ KeyResolver = (*Context)(nil)

// Translate is m's text in the App's language ([App.SetLanguage]): the language itself, then
// its base ("es_MX" → "es"), then English, and the id itself when no catalog has it. Its
// mnemonic is kept on English's letter, so a catalog cannot move a shortcut; see
// [ParseMnemonic] for reading the result.
//
// A widget calls it as it is laid out, never as it renders: a language change always lays
// the tree out again, so text resolved in Layout is current when it is painted.
func (c *Context) Translate(m Message) string { return c.app.Translate(m) }

// Theme is the App's theme as frames are drawn with it: WithTheme's (the default theme when none
// was given), or the one SetTheme last applied. A component reads it where it needs the theme
// outside Render (a Theme getter, say) instead of keeping the surface's. nil on a nil Context, so a
// component asks whether or not it is mounted. Loop goroutine only, as the theme changes there.
func (c *Context) Theme() *style.Theme {
	if c == nil || c.app == nil {
		return nil
	}
	return c.app.cfg.theme
}

// KeyCommand reports which of cmds ev is: the event's chord (key.Of), then each of its candidates in
// order (key.Candidates), the first candidate any of cmds holds winning — by the App's key map, or a
// command's declared keys where the map has none. Within one candidate the first of cmds wins, so a
// widget lists its own commands in the order it wants ties broken.
//
// A release never matches; a repeat does, so a held arrow keeps moving. An event holding Hyper or
// Meta matches nothing. This is the one question a widget asks instead of switching on raw keys.
//
// A nil Context (a widget not mounted, as a unit test may drive one) matches each command's declared
// keys.
func (c *Context) KeyCommand(ev KeyEvent, cmds ...keymap.Command) (keymap.Command, bool) {
	var m *keymap.Map
	if c != nil && c.app != nil {
		m = c.app.cfg.keymap
	}
	return keyCommand(m, ev, cmds)
}

// keyCommand is KeyCommand against m, for the App's own keys (globalKey) as well as a widget's.
func keyCommand(m *keymap.Map, ev KeyEvent, cmds []keymap.Command) (keymap.Command, bool) {
	if ev.Kind == KeyRelease {
		return keymap.Command{}, false
	}
	chord, ok := key.Of(ev.Code, ev.Mods, ev.Text)
	if !ok {
		return keymap.Command{}, false
	}
	for _, cand := range key.Candidates(chord) {
		for _, cmd := range cmds {
			if m.Has(cmd, cand) {
				return cmd, true
			}
		}
	}
	return keymap.Command{}, false
}

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
//     program queue (see program_queue.go). Program-lane events are never dropped due
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

// Update enqueues fn for a later loop turn while this originating mount remains
// alive. It never runs inline and adds no persistent unmount hook. Safe to enqueue
// from any goroutine; the mounted check and fn both run on the application loop.
func (c *Context) Update(fn func()) {
	if fn == nil {
		return
	}
	c.app.Update(func() {
		if c.node.mounted {
			fn()
		}
	})
}

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
