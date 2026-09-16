package widget

import "github.com/yongjohnlee80/golib/tui"

// DISMISSAL AND PLACEMENT VOCABULARY.
//
// An overlay closes for reasons that are not interchangeable: the user accepted
// it, the user escaped it, the program closed it, or the thing it was anchored
// to went away. A caller that only learns "it closed" has to reconstruct which
// from context, and reconstructs it wrongly the first time two paths coincide.

// DismissReason says why an overlay closed.
//
// The built-in automatic paths emit only these values. An UNKNOWN value is
// deliberately permitted for a consumer dismissing programmatically with a
// reason of its own — the reason travels through the toolkit untouched, and
// renders as "unknown" rather than being rejected, so an application can carry
// its own vocabulary without the toolkit needing to know it.
type DismissReason uint8

const (
	// DismissProgrammatic: the program called Dismiss.
	DismissProgrammatic DismissReason = iota
	// DismissEscape: the user asked to leave without choosing — physically
	// Escape, or a host-configured escape-equivalent key.
	//
	// BROADENED RATHER THAN JOINED BY A SIBLING. A Modal may accept extra
	// dismiss keys (see WithModalDismissKeys), and `q` there means exactly what
	// Escape means: close this, having decided nothing. A separate reason would
	// force every listener to learn a second constant for one intention, and
	// the ones that did not would treat a `q` dismissal as an unrecognised
	// event. What the operator MEANT is the same; only the key differs.
	DismissEscape
	// DismissAccept: an affirmative control was activated.
	DismissAccept
	// DismissCancel: a cancelling control was activated.
	DismissCancel
	// DismissAnchorLost: what the overlay was anchored to is gone.
	DismissAnchorLost
	// DismissReplaced: another overlay took this one's place.
	DismissReplaced
)

// String names the reason for traces, events and test failures.
func (r DismissReason) String() string {
	switch r {
	case DismissProgrammatic:
		return "programmatic"
	case DismissEscape:
		return "escape"
	case DismissAccept:
		return "accept"
	case DismissCancel:
		return "cancel"
	case DismissAnchorLost:
		return "anchor-lost"
	case DismissReplaced:
		return "replaced"
	}
	return "unknown"
}

// LayerID names a keyed overlay layer. It is empty for an overlay opened
// without a key, which is the ordinary case for a modal dialog.
type LayerID string

// OverlayDismissedEvent reports that an overlay closed, and why.
//
// Exactly one is published per lifecycle transition: a dismissal that finds the
// overlay already closed publishes nothing, so a listener counting these is
// counting closures rather than attempts.
type OverlayDismissedEvent struct {
	// Owner is the overlay's node — for a Modal, the Modal itself rather than
	// its card, since the card is an unfocusable layout child.
	Owner tui.NodeID
	// Layer is the key the overlay was opened under, empty for an unkeyed one.
	Layer LayerID
	// Reason is why it closed.
	Reason DismissReason
}

// ModalPlacement says where a Modal's card sits within the host.
type ModalPlacement uint8

const (
	// PlacementCenter is the default, and what a dialog almost always wants.
	PlacementCenter ModalPlacement = iota
	PlacementTopLeft
	PlacementTopRight
	PlacementBottomLeft
	PlacementBottomRight
)

// String names the placement for traces and test failures.
func (p ModalPlacement) String() string {
	switch p {
	case PlacementCenter:
		return "center"
	case PlacementTopLeft:
		return "top-left"
	case PlacementTopRight:
		return "top-right"
	case PlacementBottomLeft:
		return "bottom-left"
	case PlacementBottomRight:
		return "bottom-right"
	}
	return "unknown"
}

// Valid reports whether p is one of the declared placements. Exported for the
// same reason PointerPolicy.Valid is: a construction option has to reject an
// invalid value before storing it, and duplicating the bound is how two checks
// come to disagree.
func (p ModalPlacement) Valid() bool { return p <= PlacementBottomRight }

// PlacementSide is the side of an anchor a popup prefers.
type PlacementSide uint8

const (
	// PlacementBelow is the default: a menu opens downward.
	PlacementBelow PlacementSide = iota
	PlacementAbove
	PlacementRight
	PlacementLeft
)

// String names the side.
func (s PlacementSide) String() string {
	switch s {
	case PlacementBelow:
		return "below"
	case PlacementAbove:
		return "above"
	case PlacementRight:
		return "right"
	case PlacementLeft:
		return "left"
	}
	return "unknown"
}

// Valid reports whether s is one of the declared sides.
func (s PlacementSide) Valid() bool { return s <= PlacementLeft }

// PlacementAlign is how a popup lines up along the anchor's other axis.
type PlacementAlign uint8

const (
	// PlacementAlignStart is the default: left-aligned below, top-aligned beside.
	PlacementAlignStart PlacementAlign = iota
	PlacementAlignCenter
	PlacementAlignEnd
)

// String names the alignment.
func (a PlacementAlign) String() string {
	switch a {
	case PlacementAlignStart:
		return "start"
	case PlacementAlignCenter:
		return "center"
	case PlacementAlignEnd:
		return "end"
	}
	return "unknown"
}

// Valid reports whether a is one of the declared alignments.
func (a PlacementAlign) Valid() bool { return a <= PlacementAlignEnd }

// Placement is a popup's PREFERRED position relative to its anchor. It is a
// preference rather than an instruction: the anchor policy flips or clips it
// when the preferred position would not fit on screen.
//
// Its zero value is the common case — below the anchor, aligned to its start,
// with no offset — so a caller that wants ordinary menu behaviour supplies
// nothing.
type Placement struct {
	Side   PlacementSide
	Align  PlacementAlign
	Offset tui.Point
}

// Valid reports whether both closed enums hold declared values. The offset is
// unconstrained: any point is a legitimate nudge.
func (p Placement) Valid() bool { return p.Side.Valid() && p.Align.Valid() }
