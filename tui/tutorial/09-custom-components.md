# 9 — Custom Components: Building and Wrapping Widgets

While `golib/tui/widget` provides a rich set of primitives (Box, Table, List,
Editor, Tabs, Split, Dock), real-world applications frequently require custom
composite controls:
- A labeled text input with inline validation errors.
- An editor decorated with a line-number gutter and word-count footer.
- A searchable table with an embedded quick-filter input.
- A status badge or interactive progress bar.

This chapter covers the component protocol, the best-practice wrapper pattern,
and how to handle layout and event interception correctly.

---

## 1. The Core Component Interface

Every UI element in `golib/tui` implements `tui.Component`:

```go
type Component interface {
    Init(ctx *Context)
    Layout(c Constraints) Size
    Render(s Surface)
    HandleEvent(ev Event) bool
}
```

### Lifecycle & Constraints Rules

1. **`Init(ctx *Context)`**:
   - Invoked once when the component is mounted into the active tree.
   - Store `ctx` on your struct for subsequent layout, focus, and async calls.
   - Mount any child components via `ctx.Mount(child)`.
   - Register bus subscriptions with `tui.SubscribeScoped(ctx, ...)`.
   - Must be re-entrant: if a component is unmounted and remounted, `Init` runs again.
2. **`Layout(c Constraints) Size`**:
   - Implements the Flutter-style **constraints-down, sizes-up** contract.
   - The parent passes `c` (min/max width and height); the component returns the `tui.Size` it intends to occupy.
   - **This is the ONLY method where calling `ctx.LayoutChild` and `ctx.PlaceChild` is legal.** Calling them outside `Layout` will panic.
3. **`Render(s Surface)`**:
   - Draws local chrome into `s` (which is translated and clipped to your placed rectangle).
   - **Children draw themselves**: you do NOT call `child.Render()`. The runtime renders mounted, placed children automatically.
4. **`HandleEvent(ev Event) bool`**:
   - Executes strictly on the event loop goroutine.
   - Return `true` if your component handled and consumed the event.
   - Return `false` to let the event bubble up to your parent controller.

---

## 2. The Wrapper Pattern: Wrapping an Existing Widget

When augmenting an existing widget (such as adding a title header, a footer badge, or line numbers), compose the widget inside a wrapper component rather than subclassing or forking it.

Below is a complete, production-ready custom component: `SearchableList`, which wraps a `widget.List` and a `widget.TextInput` into a unified searchable control.

```go
package myapp

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// SearchableList is a custom component wrapping a TextInput filter and a List.
type SearchableList[T any] struct {
	ctx   *tui.Context
	input *widget.TextInput
	list  *widget.List[T]

	allItems []T
	filterFn func(item T, query string) bool
}

func NewSearchableList[T any](
	items []T,
	renderItem func(item T) string,
	filterFn func(item T, query string) bool,
) *SearchableList[T] {
	return &SearchableList[T]{
		allItems: items,
		filterFn: filterFn,
		input: widget.NewTextInput(
			widget.WithPlaceholder("Search... (/ to focus, Esc to clear)"),
		),
		list: widget.NewList(
			widget.WithItems(items, renderItem),
		),
	}
}

// 1. Init: Mount children and listen for text changes
func (sl *SearchableList[T]) Init(ctx *tui.Context) {
	sl.ctx = ctx
	ctx.Mount(sl.input)
	ctx.Mount(sl.list)

	// Subscribe to filter input text changes with automatic mount-bound cleanup:
	tui.SubscribeScoped(ctx, func(ev widget.ChangeEvent) {
		if ev.Owner == sl.input.NodeID() {
			sl.applyFilter(ev.Value)
		}
	})
}

// 2. Layout: Position input at top, list takes the remainder
func (sl *SearchableList[T]) Layout(c tui.Constraints) tui.Size {
	// Layout input: fixed 1 row height
	inputConst := tui.Constraints{
		MinW: c.MinW,
		MaxW: c.MaxW,
		MinH: 1,
		MaxH: 1,
	}
	inputSz := sl.ctx.LayoutChild(sl.input, inputConst)
	sl.ctx.PlaceChild(sl.input, tui.Rect{X: 0, Y: 0, W: inputSz.W, H: inputSz.H})

	// Layout list: remainder of height
	listH := max(c.MaxH-inputSz.H, 0)
	listConst := tui.Constraints{
		MinW: c.MinW,
		MaxW: c.MaxW,
		MinH: 0,
		MaxH: listH,
	}
	listSz := sl.ctx.LayoutChild(sl.list, listConst)
	sl.ctx.PlaceChild(sl.list, tui.Rect{X: 0, Y: inputSz.H, W: listSz.W, H: listSz.H})

	return tui.Size{
		W: max(inputSz.W, listSz.W),
		H: inputSz.H + listSz.H,
	}
}

// 3. Render: Children render themselves; wrapper renders a divider line
func (sl *SearchableList[T]) Render(s tui.Surface) {
	// Optional: paint a divider or border between input and list if desired
}

// 4. HandleEvent: Event interception and focus swapping
func (sl *SearchableList[T]) HandleEvent(ev tui.Event) bool {
	ke, ok := ev.(tui.KeyEvent)
	if !ok || ke.Kind != tui.KeyPress {
		return false
	}

	switch ke.Code {
	case '/': // Shortcut: jump from list to search input
		sl.ctx.FocusComponent(sl.input)
		return true

	case tui.KeyEscape: // Clear filter and return focus to list
		if sl.input.Value() != "" {
			sl.input.SetValue("")
			sl.applyFilter("")
			sl.ctx.FocusComponent(sl.list)
			return true
		}

	case tui.KeyDown, tui.KeyUp:
		// Transfer focus. The next navigation key reaches the List through the
		// runtime; do not call another component's HandleEvent directly.
		sl.ctx.FocusComponent(sl.list)
		return true
	}

	return false
}

// applyFilter updates the list items matching the query.
func (sl *SearchableList[T]) applyFilter(query string) {
	if query == "" {
		sl.list.SetItems(sl.allItems)
		return
	}
	var matched []T
	for _, it := range sl.allItems {
		if sl.filterFn(it, query) {
			matched = append(matched, it)
		}
	}
	sl.list.SetItems(matched)
}

// 5. Focus delegation: the wrapper omits tui.Focusable, so it is not a tab stop.
func (sl *SearchableList[T]) FocusTarget() tui.Component { return sl.list }
```

---

## 3. Best Practices for Custom Components

### A. Focus Delegation & The Terminal Cursor

1. **Transparent containers omit `tui.Focusable`**: Do not add an
   `AcceptsFocus() bool { return false }` method merely to say the wrapper is not
   a stop; absence of the optional capability already says that structurally.
   It is also what makes a component **not focusable by design**, which
   `App.HoldsFocusable` reads: `AcceptsFocus` answers what a component accepts
   *now* (a disabled button, false), and a constant false claims the
   capability while denying it. A type whose focusability is a per-instance
   choice made at construction (a `Box` built `WithFocusable`) implements
   `tui.FocusDesigner` as well, and answers `FocusableByDesign()`.
2. **Delegate to the child with a cursor**: If a child draws a terminal cursor (like `Editor` or `TextInput`), that child **must hold actual focus**. If the wrapper steals focus, the runtime cursor reporter cannot find the inner widget, and the cursor disappears.
3. Provide a `FocusTarget() tui.Component` method so parent controllers can easily focus the active child:
   ```go
   ctx.FocusComponent(customWidget.FocusTarget())
   ```
4. **Or focus the wrapper itself with `ctx.FocusInto(wrapper)`** — Qt's
   `forceActiveFocus()`: the wrapper when it takes focus, else its
   `tui.InitialFocusProvider` nominee, else its first focusable descendant in
   document order. A hidden wrapper, one under a hidden ancestor, or one a
   focus trap keeps out takes none. Ask `ctx.HoldsFocusable(comp)` first when
   "nothing here takes focus" is a mistake rather than a state: it reports
   whether anything under comp implements `tui.Focusable` at all — by design,
   not whether it accepts focus now. A caller holding the App rather than a
   mounted Context (a declarative layer over the tree) has the same two as
   `App.FocusInto` and `App.HoldsFocusable`; the latter also says whether
   comp is mounted.

### B. Event Forwarding vs Bubbling

- **Target-then-bubble**: Events reach the focused child first. If the child does not consume the event (`return false`), it bubbles up to the parent wrapper automatically.
- **Do not call another component's `HandleEvent` directly**: doing so bypasses
  runtime pointer policy, semantic resolution, provenance, tracing, and capture.
  Move focus, let normal bubbling work, expose a typed method, or dispatch a
  semantic action with `Context.DoAction`.
- **Selective Interception**: Only return `true` if you took meaningful action. Returning `true` indiscriminately swallows application-wide shortcuts (`Ctrl+C`, `q`, modal `Esc`).

### C. Optional interaction capabilities

- Implement `tui.ActionHandler` when the component owns a typed intent such as
  menu movement or resizing. Keep physical input in resolvers so keyboard,
  pointer, and consumer-defined input share the same state transition.
- Implement `tui.Activatable` for a leaf control with one trigger operation.
- Implement `tui.FocusScope` only when the subtree genuinely traps traversal,
  such as an open modal.
- Acquire pointer capture only inside the component's own `HandleEvent` or
  `HandleAction`. End the gesture on release and on
  `tui.PointerCaptureLostEvent`; captured coordinates may be outside the widget.

### D. State Mutation Discipline

- Mutate UI state on the **loop goroutine**, but keep `Layout` and `Render`
  pure. They may measure, place, and paint; they must not publish, mount,
  unmount, or store geometry-derived state.
- When a state change depends on final geometry, register it from `Layout` with
  `ctx.AfterLayout(key, fn)`. The keyed commit runs after layout and before
  rendering, coalescing repeated registrations by the same owner and key.
- When external data updates arrive, post them onto the loop with `App.Update` or via `TaskResult` from `ctx.Go`.
- Call `ctx.MarkDirty()` whenever visual state changes so the runtime schedules a coalesced frame render.

Next: [Architectural Observations & Discussion](10-architectural-observations.md).
