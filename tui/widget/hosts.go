package widget

import "github.com/yongjohnlee80/golib/tui"

// OVERLAY HOST CAPABILITIES.
//
// A widget that owns a layer finds its host as the nearest enclosing ancestor with what it needs
// (hostFor), and names that need as an interface rather than the concrete *OverlayHost, so the
// lookup says exactly what it requires. Their methods are unexported where the layers they mount
// are this package's own types, so only this package's OverlayHost can hold one.

// popupHost is what a widget needs from the nearest OverlayHost to open a popup at a point.
type popupHost interface {
	openPopupAt(id LayerID, owner ContextOwner, panel tui.Component, at tui.Point, onClosed func(DismissReason)) (*popupLayer, error)
	CloseAnchored(id LayerID, reason DismissReason)
}

// modalHost is what a Modal opens on and is dismissed through: the host performs the transition,
// since dismissing a dialog that is not on top closes the ones above it first. The CommandLine
// opens its prompt dialog on it.
type modalHost interface {
	openModal(m *Modal) error
	dismissModal(m *Modal, reason DismissReason)
	restyleScrim(owner *Modal, s *ModalStyle)
}

// layerRaiser is what a Float raises itself through: to the top of the ordinary layers, or above
// the open dialogs when it is a shown modal one.
type layerRaiser interface {
	Raise(f *Float)
}

// identified is a component that knows its mount's node: everything embedding Base.
type identified interface {
	NodeID() tui.NodeID
}

var (
	_ popupHost   = (*OverlayHost)(nil)
	_ modalHost   = (*OverlayHost)(nil)
	_ layerRaiser = (*OverlayHost)(nil)
)
