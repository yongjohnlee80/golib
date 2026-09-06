package widget

import "github.com/yongjohnlee80/golib/tui"

// Bus event inventory. Every event carries Owner — the
// emitting widget's NodeID — as its first field so subscribers filter by
// source. Publication is enqueue-only onto the App loop:
// widgets publish via Context.Bus(), handlers run on the loop goroutine.
//
// These are Bus values, not tui.Event tree events: they never route or
// bubble through the component tree; subscribers receive them by exact
// dynamic type (tui.Subscribe / tui.SubscribeScoped).

// SubmitEvent is emitted by TextInput on Enter when validation passes.
type SubmitEvent struct {
	Owner tui.NodeID
	Value string
}

// ChangeEvent is emitted by TextInput and TextArea whenever the value
// changes through user input — coalesced per input event (one event per
// paste, not per rune).
type ChangeEvent struct {
	Owner tui.NodeID
	Value string
}

// SelectionChangedEvent is emitted by Select on commit and by List on
// cursor movement in single-select mode.
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
	Ratio float64
}

// DismissEvent is emitted by Float when it hides (Esc on a modal, or
// Float.Hide).
type DismissEvent struct{ Owner tui.NodeID }

// --- internal overlay protocol (OverlayHost <-> Select) ---

// overlayOpenEvent asks the mounted OverlayHost to mount layer on its
// overlay Stack. Unexported: the protocol is package plumbing, not API.
type overlayOpenEvent struct{ layer tui.Component }

// overlayCloseEvent asks the mounted OverlayHost to unmount layer.
type overlayCloseEvent struct{ layer tui.Component }
