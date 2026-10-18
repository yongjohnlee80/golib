package widget

import "github.com/yongjohnlee80/golib/tui"

// THE TERMINAL EDITOR'S DOCUMENT VIEW (ADR 1792303119 §4.6) — the slot gui.Editor has: a Rendered
// view that draws the whole document as one widget (a TableDocument), shown in place of the text
// while Rendered is the view drawn and the host has turned it on for the document. Raw is always
// the text. The view is the editor's child, mounted through its context and laid out in its place;
// the focus moves into it as it shows and back to the editor as it hides, and a key it leaves (the
// Rendered toggle, however the keymap binds it) bubbles to the editor.
//
// The editor's Mode is its vim mode; Raw and Rendered are its View.

// DocumentView is a Rendered view that draws the whole document as one widget. The editor shows and
// hides it through SetVisible, and the framework reads Visible: a hidden view is not laid out,
// painted or focused. It takes the keyboard itself while shown (AcceptsFocus).
type DocumentView interface {
	tui.Component
	tui.Hideable
	tui.Focusable
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

// docSlot is the editor's document view and the views chosen and drawn.
type docSlot struct {
	view      DocumentView
	on        bool       // Rendered is the view's for this document (SetRenderedDocument)
	off       bool       // the Rendered view withheld for this document (SetRenderedEnabled)
	chosen    EditorView // the view the user chose (View)
	shown     bool       // the document view is shown now
	toggleSet bool       // the core's Rendered toggle is wired
}

// SetDocumentView gives the editor a Rendered view drawn in place of its text; nil removes it. The
// old view leaves (its focus, when it had it, back to the editor) and the new one joins hidden.
func (e *Editor) SetDocumentView(v DocumentView) {
	d := &e.doc
	if v == d.view {
		return
	}
	e.showDocument(false)
	if d.view != nil && e.Context() != nil {
		e.Context().Unmount(d.view)
	}
	d.view = v
	if v != nil {
		v.SetVisible(false)
		if e.Context() != nil {
			e.Context().Mount(v)
		}
	}
	e.wireToggle()
	e.applyView()
}

// DocumentView is the editor's document view, nil for none.
func (e *Editor) DocumentView() DocumentView { return e.doc.view }

// SetRenderedDocument turns the document view on for this document (true) or off (false): a host
// turns it on for a document its view reads (a CSV file) and off for any other.
func (e *Editor) SetRenderedDocument(on bool) {
	if e.doc.on == on {
		return
	}
	e.doc.on = on
	e.applyView()
}

// SetRenderedEnabled says whether this document can be Rendered. Off, Raw is drawn but the choice
// kept (View); on again, the choice is drawn.
func (e *Editor) SetRenderedEnabled(on bool) {
	if e.doc.off == !on {
		return
	}
	e.doc.off = !on
	e.applyView()
}

// SetView chooses Raw or Rendered; Rendered is drawn when the document can render (ShownView).
func (e *Editor) SetView(v EditorView) {
	e.doc.chosen = v
	e.applyView()
}

// View is the view chosen: Rendered stays the choice while a document that cannot render is drawn
// Raw.
func (e *Editor) View() EditorView { return e.doc.chosen }

// ShownView is the view drawn now.
func (e *Editor) ShownView() EditorView {
	if e.doc.chosen == ViewRendered && e.canRender() {
		return ViewRendered
	}
	return ViewRaw
}

// canRender reports whether the Rendered view is available: a document view turned on, and the
// Rendered view not withheld.
func (e *Editor) canRender() bool { return e.doc.view != nil && e.doc.on && !e.doc.off }

// wireToggle makes the core's Rendered toggle (ActToggleRendered: Ctrl+T, or what the keymap binds)
// switch the view while the document can render; otherwise the key is not consumed.
func (e *Editor) wireToggle() {
	if e.doc.toggleSet {
		return
	}
	e.doc.toggleSet = true
	e.core.SetToggleRendered(func() bool {
		if !e.canRender() {
			return false
		}
		if e.doc.chosen == ViewRendered {
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
	d := &e.doc
	if d.view == nil || d.shown == on {
		return
	}
	d.shown = on
	if on {
		d.view.ShowDocument([]byte(e.core.Value()))
	}
	ctx := e.Context()
	focused := ctx != nil && ctx.FocusWithin(e)
	d.view.SetVisible(on)
	if ctx != nil {
		ctx.RequestLayout()
		if focused {
			if on {
				ctx.FocusInto(d.view)
			} else {
				ctx.FocusInto(e)
			}
		}
	}
}

// layoutDocument lays the shown document view out over the editor's whole area.
func (e *Editor) layoutDocument(c tui.Constraints) tui.Size {
	ctx := e.Context()
	sz := ctx.LayoutChild(e.doc.view, c)
	ctx.PlaceChild(e.doc.view, tui.Rect{W: sz.W, H: sz.H})
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
