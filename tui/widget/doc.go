// Package widget is golib/tui's standard widget set v1: the
// Base embedding contract, the Box titled-window container, and the eleven
// widgets sufficient to build sqlit- and lazygit-shaped applications out of
// the box.
//
// Inventory:
//
//	Widget       Kind           Focusable   Emits (Bus)
//	TextInput    input          yes         SubmitEvent, ChangeEvent
//	TextArea     input          yes         ChangeEvent
//	Select[T]    input          yes         SelectionChangedEvent, OpenedEvent/ClosedEvent
//	List[T]      input          yes         SelectionChangedEvent, ActivateEvent
//	BufferView   display/input  yes(scroll) FollowTailChangedEvent
//	Tabs         chrome         yes         TabChangedEvent
//	Split        container      no          SplitResizedEvent
//	Float        container      children    DismissEvent
//	StatusBar    chrome         no          —
//	ProgressBar  chrome         no          —
//	Text         display        no          —
//
// All events carry Owner (the emitting widget's NodeID) as their first
// field; publication is enqueue-only onto the App loop.
//
// Widgets are tui.Component implementations: retained, mutable,
// living on the loop goroutine. Every widget method — constructors aside —
// is loop-goroutine-only, with ONE sanctioned exception: the io.Writer
// handle returned by BufferView.Writer is safe from any goroutine (bounded
// pending bytes, ordered delivery, ErrClosed after unmount). The BufferView
// value itself stays loop-owned; it deliberately does not implement
// io.Writer.
//
// Overlays: wrap the application root in NewOverlayHost once. Select mounts
// its open option list there (found via an internal Bus handshake); Float
// attaches there explicitly (OverlayHost.Attach) or to any full-area
// tui.Stack layer.
//
// Construction is golib-conventional: widget.NewX(opts ...XOption)
// functional options; misconfiguration panics at construction; the package
// imports only the standard library, tui, and tui/style.
//
// Known v1 limits (deliberate):
//
//   - List's provided source (SliceSource) holds all items in memory;
//     100k+-row datasets must page at the data layer until a windowed
//
// ListSource ships (#2 — a new source implementation behind the existing
// seam, no List API change).
//   - TextArea's []string line buffer targets commit-message/query-editor
//     scale (kilobytes); very large single lines degrade.
//   - Text renders styled plain text — no markdown or syntax highlighting.
//   - Scroll indicators are per-widget chrome, not a standalone Scrollbar.
package widget
