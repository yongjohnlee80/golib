package tui

// VISIBILITY — Qt's Item.visible.
//
// A hidden component takes no space in its parent's layout — its Layout is not
// called, and it measures as the least its constraints allow — is not painted
// or hit-tested, is not a tab stop, and hides everything under it. Focus inside
// it is repaired as for any node that stops being eligible.
//
// It is the COMPONENT's state, not its node's, so it can be set before the
// component is mounted: a declarative screen decides visibility while building
// the tree, before any runtime node exists.

// Hideable is a component that can be hidden. A component that does not
// implement it is always shown.
type Hideable interface {
	Visible() bool
}

// Visibility is the state a component embeds to be Hideable; widget.Base and
// MultiChild embed it. The zero value is visible.
type Visibility struct{ hidden bool }

// Visible reports whether the component is shown.
func (v *Visibility) Visible() bool { return !v.hidden }

// Show sets whether the component is shown, and reports whether that changed.
// The embedding component requests a layout when it did and it is mounted.
func (v *Visibility) Show(visible bool) (changed bool) {
	if v.hidden == !visible {
		return false
	}
	v.hidden = !visible
	return true
}

// hidden reports whether c is a Hideable that is hidden.
func hidden(c Component) bool {
	h, ok := c.(Hideable)
	return ok && !h.Visible()
}
