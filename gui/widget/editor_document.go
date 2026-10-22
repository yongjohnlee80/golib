package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// THE DOCUMENT VIEW — a second kind of Rendered view. A Renderer lays the text out line by line,
// each span on its source's clusters, so Rendered Markdown stays editable. A DocumentView draws
// the whole document as one read-only widget instead (an HTML page, tuiwidget.HTMLView), shown in
// place of the text body while the Editor is Rendered and the host has turned it on for the
// document (SetRenderedDocument). Raw is the text, editable, either way.
//
// # Document View Stack & Mode Toggle Architecture
//
// Below is the layout hierarchy and focus routing between the editable editor body and the
// read-only document view:
//
//	                 ┌─────────────────────────────┐
//	                 │       editorStack           │
//	                 │   (holds body + doc view)   │
//	                 └──────────────┬──────────────┘
//	                                │
//	                 ┌──────────────┴──────────────┐
//	                 │   Mode & Document Switch?   │
//	                 └──────┬───────────────┬──────┘
//	  Rendered && the view on               Raw || the view off
//	                 │                             │
//	                 ▼                             ▼
//	        ┌─────────────────┐           ┌─────────────────┐
//	        │  DocumentView   │           │   EditorBody    │
//	        │ - Visible: true │           │ - Visible: true │
//	        │ - Takes focus   │           │ - Takes focus   │
//	        │ - Read-only HTML│           │ - Full editing  │
//	        └─────────────────┘           └─────────────────┘

// DocumentView is a Rendered view that draws the whole document as one widget: a page
// (tuiwidget.HTMLView, read only) or a CSV table (tuiwidget.TableDocument, edited in place). It is
// the terminal Editor's, and both editors keep it in a tuiwidget.DocumentSlot. gui's Editor calls
// ShowDocument as the view is shown, and again on SetValue while it is. Shown, the focus goes into
// it (the view itself, or what it holds that takes the keys), so a key it leaves, the Rendered
// toggle among them, bubbles to the Editor.
type DocumentView = tuiwidget.DocumentView

var (
	_ DocumentView = (*tuiwidget.HTMLView)(nil)
	_ DocumentView = (*tuiwidget.TableDocument)(nil)
)

// WithDocumentView gives the Editor a document view, off until SetRenderedDocument turns it on.
func WithDocumentView(v DocumentView) EditorOption { return func(c *editorConfig) { c.doc = v } }

// DocumentView is the Editor's document view; nil for none (SetDocumentView).
func (e *Editor) DocumentView() DocumentView { return e.doc.Document() }

// DocumentComponent is the document view as a component, nil for none: for a host that names no
// gui type and reaches the view by its own (an HTMLView, for its links and images).
func (e *Editor) DocumentComponent() tui.Component {
	return e.doc.Document() // a nil view is a nil component
}

// SetRenderedDocument makes Rendered draw the document view (true) or the renderer (false): a host
// turns it on for a document its view reads (an HTML file) and off for any other. Without a
// document view it does nothing.
func (e *Editor) SetRenderedDocument(on bool) {
	if e.doc.Document() == nil || !e.doc.SetOn(on) {
		return
	}
	e.applyShown() // the view shown or hidden, and Raw drawn when nothing can render
	e.sw.MarkDirty()
}

// SetDocumentView swaps the Editor's document view at run time, nil removing it: the old view
// leaves the stack (its focus, when it had it, moved to the body first) and the new one joins it
// hidden, shown when Rendered is drawn and SetRenderedDocument has turned it on. A host with one
// editor and several kinds of document (an HTML page, a table) gives each buffer its view.
func (e *Editor) SetDocumentView(v DocumentView) {
	if v == e.doc.Document() {
		return
	}
	e.showDocument(false)
	if old := e.doc.PutDocument(v); old != nil {
		e.stack.Remove(old)
	}
	if v != nil {
		e.stack.Add(v)
	}
	e.applyShown()
	e.sw.MarkDirty()
}

// showDocument shows the document view in place of the body, or the body again, moving the focus
// with them when it was in either.
func (e *Editor) showDocument(on bool) {
	s := e.stack
	if e.doc.Document() == nil || e.doc.Shown() == on {
		return
	}
	focused := s.ctx != nil && s.ctx.FocusWithin(s)
	e.doc.Show(on, func() []byte { return []byte(e.core.Value()) })
	e.body.SetVisible(!on)
	if focused {
		var to tui.Component = e.body
		if on {
			to = e.doc.Document()
		}
		s.ctx.FocusInto(to)
	}
}

// editorStack is the Panel's content: the text body and the document view, one of them shown.
// Both stay mounted, so the body keeps its scroll and layout across a toggle.
type editorStack struct {
	tui.MultiChild
	e   *Editor
	ctx *tui.Context
}

// newEditorStack builds the stack component containing the editor body and optional document view.
func newEditorStack(e *Editor) *editorStack {
	s := &editorStack{e: e}
	s.Label("editorStack")
	s.Add(e.body)
	if v := e.doc.Document(); v != nil { // hidden: the slot hid it
		s.Add(v)
	}
	return s
}

// Init initializes the editor stack with the widget context.
func (s *editorStack) Init(ctx *tui.Context) {
	s.ctx = ctx
	s.MultiChild.Init(ctx)
}

// Layout gives the shown child the whole area.
func (s *editorStack) Layout(c tui.Constraints) tui.Size {
	var sz tui.Size
	for _, k := range s.Items() {
		if h, ok := k.(tui.Hideable); ok && !h.Visible() {
			continue
		}
		sz = s.ctx.LayoutChild(k, c)
		s.ctx.PlaceChild(k, tui.Rect{W: sz.W, H: sz.H})
	}
	return sz
}

// Render paints nothing directly, delegating entirely to placed children.
func (s *editorStack) Render(tui.Surface) {}

// HandleEvent takes, while the document view is shown, the key it leaves that the editor's keymap
// binds to the Rendered toggle: the view does not know the keymap, and a remapped or unbound
// toggle must read as it does on the body. A release is bound to nothing (ActionOf), so a press
// and its release toggle once.
func (s *editorStack) HandleEvent(ev tui.Event) bool {
	k, ok := ev.(tui.KeyEvent)
	if !ok || !s.e.doc.Shown() {
		return false
	}
	if act, ok := s.e.core.ActionOf(k); ok && act == tuiwidget.ActToggleRendered && s.e.canRender() {
		s.e.toggleMode()
		return true
	}
	return false
}
