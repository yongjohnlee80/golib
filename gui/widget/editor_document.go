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

// DocumentView is a Rendered view that draws the whole document as one read-only widget.
type DocumentView interface {
	tui.Component
	// ShowDocument shows text: called as the view is shown, and on SetValue while it is.
	ShowDocument(text []byte)
	SetVisible(bool)
}

// WithDocumentView gives the Editor a document view, off until SetRenderedDocument turns it on.
func WithDocumentView(v DocumentView) EditorOption { return func(c *editorConfig) { c.doc = v } }

// DocumentView is the Editor's document view; nil for none.
func (e *Editor) DocumentView() DocumentView { return e.doc }

// SetRenderedDocument makes Rendered draw the document view (true) or the renderer (false): a host
// turns it on for a document its view reads (an HTML file) and off for any other. Without a
// document view it does nothing.
func (e *Editor) SetRenderedDocument(on bool) {
	if e.doc == nil || e.docOn == on {
		return
	}
	e.docOn = on
	// The mode is applied again even when it stays Rendered, which SetMode does not redo: the view
	// is shown or hidden here, and Rendered falls back to Raw without a renderer to draw it.
	m := e.mode
	if m == Rendered && !e.canRender() {
		m = Raw
	}
	e.showDocument(m == Rendered && e.docOn)
	e.SetMode(m)
	e.sw.MarkDirty()
}

// docActive reports whether Rendered is the document view's.
func (e *Editor) docActive() bool { return e.doc != nil && e.docOn }

// showDocument shows the document view in place of the body, or the body again, moving the focus
// with them when it was in either.
func (e *Editor) showDocument(on bool) {
	s := e.stack
	if e.doc == nil || s.docShown == on {
		return
	}
	s.docShown = on
	if on {
		e.doc.ShowDocument([]byte(e.core.Value()))
	}
	focused := s.ctx != nil && s.ctx.FocusWithin(s)
	e.doc.SetVisible(on)
	e.body.SetVisible(!on)
	if focused {
		var to tui.Component = e.body
		if on {
			to = e.doc
		}
		s.ctx.FocusInto(to)
	}
}

// editorStack is the Panel's content: the text body and the document view, one of them shown.
// Both stay mounted, so the body keeps its scroll and layout across a toggle.
type editorStack struct {
	tui.MultiChild
	e        *Editor
	ctx      *tui.Context
	docShown bool
}

func newEditorStack(e *Editor) *editorStack {
	s := &editorStack{e: e}
	s.Label("editorStack")
	s.Add(e.body)
	if e.doc != nil {
		e.doc.SetVisible(false)
		s.Add(e.doc)
	}
	return s
}

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

func (s *editorStack) Render(tui.Surface) {}

// HandleEvent takes, while the document view is shown, the key it leaves that the editor's keymap
// binds to the Rendered toggle: the view does not know the keymap, and a remapped or unbound
// toggle must read as it does on the body. A release is bound to nothing (ActionOf), so a press
// and its release toggle once.
func (s *editorStack) HandleEvent(ev tui.Event) bool {
	k, ok := ev.(tui.KeyEvent)
	if !ok || !s.docShown {
		return false
	}
	if act, ok := s.e.core.ActionOf(k); ok && act == tuiwidget.ActToggleRendered && s.e.canRender() {
		s.e.toggleMode()
		return true
	}
	return false
}
