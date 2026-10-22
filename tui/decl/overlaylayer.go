package decl

import (
	"errors"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// ErrOutsideWindow is what opening an [OverlayLayer] returns when it is attached to no overlay: the
// type opens over a Window and is not in one. errors.Is matches it; the message names the type.
var ErrOutsideWindow = errors.New("tui/decl: an overlay layer opened outside a Window")

// OverlayLayer is the overlay attachment of a type that opens over the screen through a Float: a
// Drawer, controls' Popup. It attaches the Float to its Window's overlay host, detaches it when the
// node goes, and opens it there. Embed one, made by NewOverlayLayer, and the type implements
// [Overlaid]; its Destroyed hook calls Detach.
type OverlayLayer struct {
	float *widget.Float
	host  *widget.OverlayHost
	what  string // the type as a document names it, "a Popup": for ErrOutsideWindow's message
}

// NewOverlayLayer is the layer of float for a type a document calls what ("a Drawer").
func NewOverlayLayer(float *widget.Float, what string) OverlayLayer {
	return OverlayLayer{float: float, what: what}
}

// SetOverlay implements [Overlaid]: the Float becomes one of the host's layers, hidden until
// opened. Attached to another host first, it leaves that one.
func (l *OverlayLayer) SetOverlay(host *widget.OverlayHost, _ func()) {
	if l.host == host {
		return
	}
	detachLayer(l)
	l.host = host
	host.Attach(l.float)
}

// Detach takes the Float off its host, if it is on one.
func (l *OverlayLayer) Detach() { detachLayer(l) }

// detachLayer is Detach, for SetOverlay too: a free function, so neither method calls a sibling on
// its own receiver, which an embedder's Detach would not override (the promotion self-call guard).
func detachLayer(l *OverlayLayer) {
	if l.host != nil {
		l.host.Detach(l.float)
		l.host = nil
	}
}

// Open shows the Float over its Window. opened is false when it was already shown, so the type
// raises its opened signal once. Attached to no overlay, it is ErrOutsideWindow.
func (l *OverlayLayer) Open() (opened bool, err error) {
	if l.host == nil {
		return false, outsideWindow{l.what}
	}
	if l.float.Shown() {
		return false, nil
	}
	l.float.Show()
	return true, nil
}

// outsideWindow is ErrOutsideWindow for one type, worded for a document's author.
type outsideWindow struct{ what string }

func (e outsideWindow) Error() string {
	return e.what + " opens over a Window, and this one is not in one; inside a Go program, give the adapter WithOverlay(host)"
}

func (e outsideWindow) Is(target error) bool { return target == ErrOutsideWindow }
