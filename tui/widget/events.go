package widget

import "github.com/yongjohnlee80/golib/tui"

// Package widget provides a decoupled, type-safe event model built on top of [tui.Bus].
//
// # Bus Events vs Tree Events
//
// There are two distinct event mechanisms in golib/tui:
//  1. Tree Events ([tui.Event], [tui.KeyEvent], [tui.MouseEvent]): Handled by [tui.Component.HandleEvent].
//     These route down the focused branch or bubble up from hit-tested leaf components.
//  2. Bus Events (the struct types defined here and in sibling files): Broadcast domain notifications
//     published via [tui.Context.Bus] and received by exact dynamic type through [tui.Subscribe]
//     or [tui.SubscribeScoped].
//
// Bus events NEVER route or bubble through the component tree. They allow decoupled communication
// between parent views, sibling widgets, and state controllers without requiring direct struct
// pointer wiring or callback spaghetti.
//
// # The Owner Invariant
//
// Every public bus event carries Owner ([tui.NodeID]) as its first field. Because bus subscriptions
// are typed globally or per-scope, multiple instances of the same widget type (e.g. three [TextInput]
// fields in one dialog) publish the same event type. Subscribers inspect ev.Owner to match against
// the specific widget they care about:
//
//	tui.SubscribeScoped(ctx, func(ev widget.SubmitEvent) {
//		if ev.Owner == usernameInput.NodeID() {
//			passwordInput.RequestFocus()
//		}
//	})
//
// # Publication & Concurrency: Input Lane vs Program Lane
//
// Publication onto the bus is strictly enqueue-only onto the application loop goroutine.
// The runtime processes events in two alternating lanes:
//   - Input Lane: Dispatches raw keyboard and mouse events from the terminal driver.
//   - Program Lane: Delivers enqueued bus events and asynchronous [tui.TaskResult] payloads.
//
// Because the runtime alternates between lanes, publishing a bus event introduces an asynchronous
// queue hop. If an action must execute immediately before the next pending keypress is dispatched
// (such as moving focus to prevent pasted characters from leaking into a vacated field), use
// synchronous hooks—such as [WithOnSubmit] on [TextInput]—rather than relying solely on [SubmitEvent].
//
// # Complete Event Inventory Across Package Widget
//
// In addition to the events declared below, sibling files declare specialized domain events:
//   - [SplitZoomEvent] (split.go): Published when a [Split] pane enters or exits full-screen zoom.
//   - [ExpandRequestEvent] (tree.go): Published when an unloaded [TreeNode] requests async children.
//   - [CollapseEvent] (tree.go): Published when a [TreeNode] collapses.
//   - [ModeChangedEvent] (editor_keymap.go): Published on Vim [EditorMode] transitions.

// SubmitEvent is emitted by TextInput on Enter when validation passes.
type SubmitEvent struct {
	Owner tui.NodeID
	Value string
}

// YankEvent is emitted by Editor when an EXPLICIT yank runs — visual `y` or
// `yy` — and reports whether the text reached the system clipboard.
//
// OUTCOME ONLY. It deliberately carries no text, unlike SubmitEvent, whose
// Value IS the point of a form field. A yank's payload may be a secret: the
// widget is used read-only to display credentials, and an event is exactly the
// kind of thing that ends up in a log line or a debug dump. A parent already
// knows what it rendered; what it cannot otherwise learn is whether the copy
// landed, because CopyToClipboard's bool is consumed inside the widget and
// HandleEvent returns only "handled".
//
// ClipboardDelivered is false — truthfully — when the backend implements no
// ClipboardWriter. That keeps the optional-capability contract (the widget
// does not error on a backend that cannot copy) while still letting a parent
// tell the user their copy did not leave the application.
//
// DELETES DO NOT EMIT THIS. `x`, `D`, `dd` and visual `d` fill the same
// register, correctly, but exporting them would publish text nobody asked to
// share.
type YankEvent struct {
	Owner              tui.NodeID
	ClipboardDelivered bool
}

// MacroStoppedEvent is emitted by an Editor when a macro (@{reg}) is stopped before its end, as
// Vim ends a macro on an error: Err says why (ErrMacroTooLong). Register is the one played. The
// keys still queued are dropped; what the macro changed stays, each change its own undo step. A
// host shows Err where it shows the editor's messages.
type MacroStoppedEvent struct {
	Owner    tui.NodeID
	Register rune
	Err      error
}

// ChangeEvent is emitted by TextInput, TextArea and EditorCore whenever the
// value changes through user input — coalesced per input event (one event per
// paste, not per rune).
//
// It is published only while the bus has a ChangeEvent subscriber
// ([tui.HasSubscribers]): Value is the widget's whole text, which an editor
// would otherwise join on every keystroke for no one. So when an edit finds no
// subscriber at all, its publish is skipped, and a subscription made after it,
// before the program lane drains, does not receive that edit's event. When a
// subscriber already existed, the edit's event is queued, and a later
// subscriber still receives it at delivery. Subscribe before the edits you
// follow.
type ChangeEvent struct {
	Owner tui.NodeID
	Value string
}

// SelectionChangedEvent is emitted by Select on commit, by List on cursor
// movement in single-select mode, and by Tree when the node under its cursor
// changes (Index is then the row among the Tree's VisibleRows).
type SelectionChangedEvent struct {
	Owner tui.NodeID
	Index int
	Label string
}

// OpenedEvent is emitted by Select when its option overlay opens.
type OpenedEvent struct{ Owner tui.NodeID }

// ClosedEvent is emitted by Select when its option overlay closes,
// with or without a commit.
type ClosedEvent struct{ Owner tui.NodeID }

// ActivateEvent is emitted by List on Enter or double-click.
type ActivateEvent struct {
	Owner tui.NodeID
	Index int
}

// ContextMenuEvent is emitted by List, when its context menu is on (List.SetContextMenu), for a
// right press or the context-menu key (Shift+F10, Menu): the row it is for, the cursor already
// moved there (-1 for a press below the rows), and where to open the menu, in the list's own cells.
type ContextMenuEvent struct {
	Owner tui.NodeID
	Index int
	At    tui.Point
}

// FollowTailChangedEvent is emitted by BufferView when follow-tail
// disengages (manual scroll-up) or re-engages (End / scroll to bottom).
type FollowTailChangedEvent struct {
	Owner     tui.NodeID
	Following bool
}

// TabChangedEvent is emitted by Tabs when the active tab changes.
type TabChangedEvent struct {
	Owner tui.NodeID
	Index int
	Label string
}

// SplitResizedEvent is emitted by Split when the divider moves (keyboard or
// mouse drag).
type SplitResizedEvent struct {
	Owner tui.NodeID
	// Ratio is the EFFECTIVE division — what is on screen after the min sizes
	// and the available cells have had their say, not what was asked for.
	Ratio float64
	// ACells and BCells are that same answer in integers, which is what a
	// consumer persisting or reporting a layout actually needs: a ratio has to
	// be re-derived against an extent, and two readers dividing it differently
	// is how a restored split lands one cell off.
	ACells, BCells int
}
