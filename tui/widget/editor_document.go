package widget

import "github.com/yongjohnlee80/golib/tui"

// THE TERMINAL EDITOR'S DOCUMENT VIEW — the slot gui.Editor has: a Rendered
// view that draws the whole document as one widget (a TableDocument), shown in place of the text
// while Rendered is the view drawn and the host has turned it on for the document. Raw is always
// the text. The view is the editor's child, mounted through its context and laid out in its place;
// the focus moves into it as it shows and back to the editor as it hides, and a key it leaves (the
// Rendered toggle, however the keymap binds it) bubbles to the editor.
//
// The editor's Mode is its vim mode; Raw and Rendered are its View.

// DocumentView is a Rendered view that draws the whole document as one widget. The editor shows and
// hides it through SetVisible, and the framework reads Visible: a hidden view is not laid out,
// painted or focused. While shown it takes the keyboard: the editor moves the focus into it
// (tui.Context.FocusInto), to the view itself or to the focusable part inside it.
type DocumentView interface {
	tui.Component
	tui.Hideable
	// ShowDocument shows text: called as the view is shown.
	ShowDocument(text []byte)
	SetVisible(bool)
}

// EditorView is the view an editor draws: its text (Raw) or its Rendered view.
type EditorView uint8

const (
	ViewRaw EditorView = iota
	ViewRendered
)

// DocumentSlot is an editor's Rendered state: its document view, the view chosen (Raw or
// Rendered), and whether the document can render and is drawn Rendered. Both golib editors keep one,
// the cell Editor and gui's; each does the rest itself: it mounts the view, lays it out, and moves
// the focus when Show reports the view shown or hidden. An editor that renders text itself as well
// (gui's, through a Renderer) says so to CanRender and Drawn.
type DocumentSlot struct {
	view   DocumentView
	on     bool       // Rendered is the view's for this document (SetOn)
	off    bool       // the Rendered view withheld for this document (SetEnabled)
	chosen EditorView // the view chosen (Chosen)
	shown  bool       // the document view is shown now
}

// Document is the slot's document view, nil for none.
func (d *DocumentSlot) Document() DocumentView { return d.view }

// PutDocument makes v the slot's document view, hidden, and returns the one it replaces. The editor
// hides the old one first (Show(false, …)), unmounts it, and mounts v.
func (d *DocumentSlot) PutDocument(v DocumentView) (old DocumentView) {
	old, d.view = d.view, v
	if v != nil {
		v.SetVisible(false)
	}
	return old
}

// SetOn turns the document view on for this document (a host turns it on for a document its view
// reads) or off, reporting whether that changed.
func (d *DocumentSlot) SetOn(on bool) bool {
	if d.on == on {
		return false
	}
	d.on = on
	return true
}

// SetEnabled says whether this document can be Rendered at all, reporting whether that changed. Off,
// Raw is drawn but the choice kept.
func (d *DocumentSlot) SetEnabled(on bool) bool {
	if d.off == !on {
		return false
	}
	d.off = !on
	return true
}

// Enabled reports whether the Rendered view is not withheld (SetEnabled).
func (d *DocumentSlot) Enabled() bool { return !d.off }

// Choose chooses Raw or Rendered; Chosen is the choice, kept while a document that cannot render is
// drawn Raw.
func (d *DocumentSlot) Choose(v EditorView) { d.chosen = v }

// Chosen is the view chosen.
func (d *DocumentSlot) Chosen() EditorView { return d.chosen }

// Active reports whether Rendered is the document view's: a view, turned on for this document.
func (d *DocumentSlot) Active() bool { return d.view != nil && d.on }

// CanRender reports whether the Rendered view is available: the document view active, or the editor
// rendering the text itself (own), and the Rendered view not withheld.
func (d *DocumentSlot) CanRender(own bool) bool { return (own || d.Active()) && !d.off }

// Drawn is the view drawn now: Rendered when it is chosen and the document can render.
func (d *DocumentSlot) Drawn(own bool) EditorView {
	if d.chosen == ViewRendered && d.CanRender(own) {
		return ViewRendered
	}
	return ViewRaw
}

// Shown reports whether the document view is shown now.
func (d *DocumentSlot) Shown() bool { return d.shown }

// Show shows the document view, handing it the text, or hides it, and reports whether that changed:
// the editor then lays out again and moves the focus with it. Without a view it changes nothing.
func (d *DocumentSlot) Show(on bool, text func() []byte) bool {
	if d.view == nil || d.shown == on {
		return false
	}
	d.shown = on
	if on {
		d.view.ShowDocument(text())
	}
	d.view.SetVisible(on)
	return true
}

// SetDocumentView gives the editor a Rendered view drawn in place of its text; nil removes it. The
// old view leaves (its focus, when it had it, back to the editor) and the new one joins hidden.
func (e *Editor) SetDocumentView(v DocumentView) {
	if v == e.doc.Document() {
		return
	}
	e.showDocument(false)
	old := e.doc.PutDocument(v)
	if ctx := e.Context(); ctx != nil {
		if old != nil {
			ctx.Unmount(old)
		}
		if v != nil {
			ctx.Mount(v)
		}
	}
	e.wireToggle()
	e.applyView()
}

// DocumentView is the editor's document view, nil for none.
func (e *Editor) DocumentView() DocumentView { return e.doc.Document() }

// SetRenderedDocument turns the document view on for this document (true) or off (false): a host
// turns it on for a document its view reads (a CSV file) and off for any other.
func (e *Editor) SetRenderedDocument(on bool) {
	if e.doc.SetOn(on) {
		e.applyView()
	}
}

// SetRenderedEnabled says whether this document can be Rendered. Off, Raw is drawn but the choice
// kept (View); on again, the choice is drawn.
func (e *Editor) SetRenderedEnabled(on bool) {
	if e.doc.SetEnabled(on) {
		e.applyView()
	}
}

// SetView chooses Raw or Rendered; Rendered is drawn when the document can render (ShownView).
func (e *Editor) SetView(v EditorView) {
	e.doc.Choose(v)
	e.applyView()
}

// View is the view chosen: Rendered stays the choice while a document that cannot render is drawn
// Raw.
func (e *Editor) View() EditorView { return e.doc.Chosen() }

// ShownView is the view drawn now.
func (e *Editor) ShownView() EditorView { return e.doc.Drawn(false) }

// canRender reports whether the Rendered view is available: a document view turned on, and the
// Rendered view not withheld. The cell Editor renders nothing itself.
func (e *Editor) canRender() bool { return e.doc.CanRender(false) }

// wireToggle makes the core's Rendered toggle (ActToggleRendered: Ctrl+T, or what the keymap binds)
// switch the view while the document can render; otherwise the key is not consumed.
func (e *Editor) wireToggle() {
	if e.docToggle {
		return
	}
	e.docToggle = true
	e.core.SetToggleRendered(func() bool {
		if !e.canRender() {
			return false
		}
		if e.doc.Chosen() == ViewRendered {
			e.SetView(ViewRaw)
		} else {
			e.SetView(ViewRendered)
		}
		return true
	})
}

// applyView shows the document view, or the text, as ShownView says.
func (e *Editor) applyView() {
	e.showDocument(e.ShownView() == ViewRendered)
	e.MarkDirty()
}

// showDocument shows the document view in place of the text, or the text again, moving the focus
// with them when it was in either.
func (e *Editor) showDocument(on bool) {
	ctx := e.Context()
	focused := ctx != nil && ctx.FocusWithin(e)
	if !e.doc.Show(on, func() []byte { return []byte(e.core.Value()) }) || ctx == nil {
		return
	}
	ctx.RequestLayout()
	if focused {
		if on {
			ctx.FocusInto(e.doc.Document())
		} else {
			ctx.FocusInto(e)
		}
	}
}

// layoutDocument lays the shown document view out over the editor's whole area.
func (e *Editor) layoutDocument(c tui.Constraints) tui.Size {
	ctx := e.Context()
	sz := ctx.LayoutChild(e.doc.Document(), c)
	ctx.PlaceChild(e.doc.Document(), tui.Rect{W: sz.W, H: sz.H})
	return sz
}

// documentKey is a key the shown document view left: the Rendered toggle switches back to the text,
// and nothing else reaches the hidden text.
func (e *Editor) documentKey(k tui.KeyEvent) bool {
	if act, ok := e.core.ActionOf(k); ok && act == ActToggleRendered && e.canRender() {
		e.SetView(ViewRaw)
		return true
	}
	return false
}
