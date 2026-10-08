package widget

import (
	"context"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// docEditor is an Editor with Markdown's renderer and an HTMLView document view, its pixel layout
// bound, showing text.
func docEditor(t *testing.T, text string, opts ...EditorOption) (*edHarness, *tuiwidget.HTMLView) {
	t.Helper()
	v := tuiwidget.NewHTMLView()
	BindHTML(v)
	h := startEditor(t, 60, 20, append([]EditorOption{WithRenderer(NewMarkdownRenderer()), WithDocumentView(v),
		WithCore(tuiwidget.CoreInitialText(text))}, opts...)...)
	return h, v
}

// state is what the cells read on the loop: the mode, which child shows, and where the focus is.
type docState struct {
	mode            EditorMode
	docShown        bool
	bodyVis, docVis bool
	focusDoc, focus bool // focus in the view; focus in the body
	source          string
	listEditing     bool
}

func (h *edHarness) docState(v *tuiwidget.HTMLView) (s docState) {
	h.onLoop(func() {
		s = docState{mode: h.e.Mode(), docShown: h.e.stack.docShown, bodyVis: h.e.body.Visible(), docVis: v.Visible(),
			focusDoc: h.app.FocusWithin(v), focus: h.app.FocusWithin(h.e.body), source: string(v.Source()),
			listEditing: h.e.core.ListEditing()}
	})
	return s
}

// TestTheDocumentViewIsRenderedForADocumentItReads: with the document view turned on, Rendered
// shows it in place of the body with the text and the focus; Ctrl+T from it returns to Raw with
// the focus on the body, a press and its release toggling once; an edit in Raw reaches it on the
// way back; SetValue while it is shown reaches it at once.
func TestTheDocumentViewIsRenderedForADocumentItReads(t *testing.T) {
	h, v := docEditor(t, "<p>one</p>")
	h.onLoop(func() { h.e.SetRenderedDocument(true) })
	h.keys(ctrl('t'))
	s := h.docState(v)
	if s.mode != Rendered || !s.docShown || s.bodyVis || !s.docVis || s.source != "<p>one</p>" || s.listEditing {
		t.Fatalf("Rendered with the view on: %+v", s)
	}
	h.until("the focus in the view", func() bool { return h.docState(v).focusDoc })

	h.keys(ctrl('t'), tui.KeyEvent{Code: 't', Mods: tui.ModCtrl, Kind: tui.KeyRelease})
	s = h.docState(v)
	if s.mode != Raw || s.docShown || !s.bodyVis || s.docVis {
		t.Fatalf("Ctrl+T (pressed, then released) from the view: %+v, want Raw on the body", s)
	}
	h.until("the focus back in the body", func() bool { return h.docState(v).focus })

	h.keys(tui.KeyEvent{Code: 'i', Text: "i"}, tui.KeyEvent{Code: 'x', Text: "x"}, tui.KeyEvent{Code: tui.KeyEscape})
	h.keys(ctrl('t'))
	if s = h.docState(v); !s.docShown || s.source != "x<p>one</p>" {
		t.Fatalf("Rendered after an edit in Raw: %+v, want the edited text", s)
	}
	h.onLoop(func() { h.e.SetValue("<p>two</p>") })
	if s = h.docState(v); s.source != "<p>two</p>" {
		t.Errorf("SetValue while shown: the view has %q", s.source)
	}
}

// TestTheDocumentViewTurnedOffWhileRendered: SetRenderedDocument(false) while Rendered shows the
// body again, the focus moving with it, and stays Rendered through the renderer (list editing on),
// though SetMode's mode is unchanged; with no renderer, Rendered falls back to Raw.
func TestTheDocumentViewTurnedOffWhileRendered(t *testing.T) {
	h, v := docEditor(t, "- item")
	h.onLoop(func() { h.e.SetRenderedDocument(true); h.e.SetMode(Rendered) })
	h.until("the focus in the view", func() bool { return h.docState(v).focusDoc })
	h.onLoop(func() { h.e.SetRenderedDocument(false) })
	s := h.docState(v)
	if s.mode != Rendered || s.docShown || !s.bodyVis || s.docVis || !s.listEditing {
		t.Fatalf("turned off while Rendered: %+v, want Rendered Markdown on the body", s)
	}
	h.until("the focus back in the body", func() bool { return h.docState(v).focus })

	v2 := tuiwidget.NewHTMLView()
	BindHTML(v2)
	h2 := startEditor(t, 60, 20, WithDocumentView(v2), WithCore(tuiwidget.CoreInitialText("<p>x</p>")))
	h2.onLoop(func() { h2.e.SetRenderedDocument(true); h2.e.SetMode(Rendered) })
	if s := h2.docState(v2); s.mode != Rendered || !s.docShown {
		t.Fatalf("a document view alone gives Rendered: %+v", s)
	}
	h2.onLoop(func() { h2.e.SetRenderedDocument(false) })
	if s := h2.docState(v2); s.mode != Raw || s.docShown || !s.bodyVis {
		t.Errorf("turned off with no renderer: %+v, want Raw", s)
	}
}

// TestTheDocumentViewLeavesOtherKeys: in the view, j scrolls it rather than reaching the hidden
// body's core, and a key the keymap leaves unbound bubbles.
func TestTheDocumentViewLeavesOtherKeys(t *testing.T) {
	h, v := docEditor(t, "<p>a</p>")
	h.onLoop(func() { h.e.SetRenderedDocument(true); h.e.SetMode(Rendered) })
	h.until("the focus in the view", func() bool { return h.docState(v).focusDoc })
	var ln int
	h.onLoop(func() { ln, _ = h.e.core.Line() })
	h.keys(tui.KeyEvent{Code: 'j', Text: "j"}, tui.KeyEvent{Code: 'x', Text: "x"})
	var after string
	var ln2 int
	h.onLoop(func() { after = h.e.core.Value(); ln2, _ = h.e.core.Line() })
	if after != "<p>a</p>" || ln2 != ln {
		t.Errorf("keys in the view reached the body's core: text %q, line %d", after, ln2)
	}
}

// TestCtrlTPressAndReleaseFromRawTogglesOnce: from Raw, Ctrl+T's press shows the view and moves
// the focus into it, so its release lands on the view and bubbles to the stack: it must not toggle
// back.
func TestCtrlTPressAndReleaseFromRawTogglesOnce(t *testing.T) {
	h, v := docEditor(t, "<p>one</p>")
	h.onLoop(func() { h.e.SetRenderedDocument(true) })
	h.keys(ctrl('t'))
	h.until("the focus in the view", func() bool { return h.docState(v).focusDoc })
	h.keys(tui.KeyEvent{Code: 't', Mods: tui.ModCtrl, Kind: tui.KeyRelease})
	if s := h.docState(v); s.mode != Rendered || !s.docShown {
		t.Errorf("Ctrl+T's release in the view toggled again: %+v", s)
	}
}

// TestTheFocusMovesWithTheShownChild: with a focusable widget before the editor, a toggle keeps the
// focus in the editor, on the child shown: the focus repair alone would hand it to the first
// focusable in the window.
func TestTheFocusMovesWithTheShownChild(t *testing.T) {
	v := tuiwidget.NewHTMLView()
	BindHTML(v)
	e := NewEditor(WithRenderer(NewMarkdownRenderer()), WithDocumentView(v), WithCore(tuiwidget.CoreInitialText("<p>x</p>")))
	col := tui.NewFlex(tui.Vertical)
	before := tuiwidget.NewButton("before")
	col.Add(before)
	col.AddWeighted(e, 1)
	tb := tui.NewTestBackend(60, 20, tui.WithTestCapabilities(tui.Capabilities{NativeViews: true}))
	sh := &shell{child: col}
	app := tui.NewApp(sh, tui.WithBackend(&nativeTB{TestBackend: tb}))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go app.Run(ctx)
	h := &edHarness{t: t, app: app, tb: tb, sh: sh, e: e, cell: gui.Size{W: cellW, H: cellH}}
	h.until("a frame", func() bool { return tb.Flushes() > 0 })
	h.onLoop(func() { app.FocusInto(e) })
	h.barrier()
	h.onLoop(func() { e.SetRenderedDocument(true) })
	h.keys(ctrl('t'))
	h.until("the focus in the view", func() bool { return h.docState(v).focusDoc })
	h.keys(ctrl('t'))
	h.until("the focus in the body", func() bool { return h.docState(v).focus })
	var onBefore bool
	h.onLoop(func() { onBefore = app.FocusWithin(before) })
	if onBefore {
		t.Error("the focus left the editor for the widget before it")
	}
}
