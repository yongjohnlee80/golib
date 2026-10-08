// Package decl is gui's decl style: the native look of tui/decl's QML vocabulary. A Program that
// runs on gui's backend takes it with tuidecl.WithStyle(Native()), and a document writing
// `Editor { }` gets gui's Editor, which draws in pixels and has a Rendered view. The same document
// runs with no style on a terminal and gets tui's Editor: the two read a declaration the same way
// (tuidecl.ReadEditor) and offer the same QML contract (decltest.CheckStyle).
package decl

import (
	"github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// NativeOption sets up the native style.
type NativeOption func(*native)

type native struct {
	renderers map[string]func(tuidecl.RendererSpec) widget.Renderer
	fallback  widget.Diagrammer // what native Mermaid declines; nil: drawn as code
	diagrams  widget.Diagrammer // the style's one chain and cache, made on first use
}

// WithDiagramFallback draws what golib's native Mermaid declines (a diagram type it does not
// draw, one over a limit) with d: an application's own renderer, such as one through a headless
// browser. Without it, such a fence stays code.
func WithDiagramFallback(d widget.Diagrammer) NativeOption {
	return func(n *native) { n.fallback = d }
}

// diagramChain is the style's Diagrams: one chain, so one cache, shared by every renderer the
// style builds, which is every editor in its window.
func (n *native) diagramChain() widget.Diagrammer {
	if n.diagrams == nil {
		n.diagrams = widget.Diagrams(n.fallback)
	}
	return n.diagrams
}

// WithRendererFor makes the renderer of a spec of kind (its RendererKind) with f: a consumer's
// own renderer, declared through its own spec type and tuidecl.RendererNode. "markdown" is built
// in; f for it replaces golib's. A spec whose kind has no renderer leaves the Editor Raw only.
func WithRendererFor(kind string, f func(tuidecl.RendererSpec) widget.Renderer) NativeOption {
	return func(n *native) { n.renderers[kind] = f }
}

// Native is gui's style. It replaces Editor, and lays HTMLView out in pixels (the same tui
// widget, so a host reaches it by id either way); every other type stays tui's.
func Native(opts ...NativeOption) tuidecl.Style {
	n := &native{renderers: map[string]func(tuidecl.RendererSpec) widget.Renderer{}}
	n.renderers["markdown"] = n.markdownRenderer
	for _, o := range opts {
		o(n)
	}
	ed, _ := tuidecl.StandardType("Editor")
	ed.Build = n.buildEditor
	ed.Setters = editorSetters()
	ed.Restyle = restyleEditor
	hv, _ := tuidecl.StandardType("HTMLView")
	build := hv.Build
	hv.Build = func(b tuidecl.Build) (tui.Component, []string, error) {
		c, consumed, err := build(b)
		if v, ok := c.(*tuiwidget.HTMLView); ok && err == nil {
			widget.BindHTML(v) // laid out in pixels and drawn natively, the same widget to the host
		}
		return c, consumed, err
	}
	return tuidecl.Style{Name: "native", Types: []tuidecl.Type{ed, hv}}
}

// markdownRenderer is golib's renderer for a MarkdownRenderer declaration. It reads the spec's
// headingScale, and its mermaid flag: a mermaid fence is drawn as its diagram, natively (a
// flowchart) or by the style's fallback, through the style's one chain.
func (n *native) markdownRenderer(s tuidecl.RendererSpec) widget.Renderer {
	var opts []widget.MarkdownOption
	if m, ok := s.(tuidecl.MarkdownSpec); ok {
		if m.HeadingScale > 0 {
			opts = append(opts, widget.HeadingScale(m.HeadingScale))
		}
		if m.Mermaid {
			opts = append(opts, widget.WithDiagrams(n.diagramChain()))
		}
	}
	return widget.NewMarkdownRenderer(opts...)
}

// buildEditor builds gui's Editor from the declaration tui's Editor reads: the same text, the
// same listeners on its core, the same highlighter attached, and a renderer from its spec.
func (n *native) buildEditor(b tuidecl.Build) (tui.Component, []string, error) {
	d, err := tuidecl.ReadEditor(b)
	if err != nil {
		return nil, nil, err
	}
	opts := []widget.EditorOption{widget.WithCore(d.CoreOptions()...), widget.WithWrap(wrapMode(d.Wrap))}
	if d.Renderer != nil {
		if f, ok := n.renderers[d.Renderer.RendererKind()]; ok {
			if r := f(d.Renderer); r != nil {
				opts = append(opts, widget.WithRenderer(r))
			}
		}
	}
	e := widget.NewEditor(opts...)
	d.Attach(e.Core())
	return e, d.Consumed, nil
}

// wrapMode is the QML wrap property as a WrapMode: true wraps long lines at the editor's width,
// false keeps them on one row and scrolls sideways (the default, as tui's Editor's).
func wrapMode(wrap bool) tuiwidget.WrapMode {
	if wrap {
		return tuiwidget.WrapSoft
	}
	return tuiwidget.WrapNone
}

// editorSetters are the Editor's runtime properties on gui's Editor: the behaviour ones shared
// with tui's, and its own view ones, which mean what they mean on tui's Editor. The contract is
// tui's Editor's, property for property.
func editorSetters() map[string]tuidecl.Setter {
	s := tuidecl.EditorSetters()
	s["contextMenu"] = tuidecl.BoolSetter((*widget.Editor).SetContextMenu)
	s["cursorColor"] = tuidecl.ColorSetter((*widget.Editor).SetCursorColor)
	s["view"] = tuidecl.EnumSetter(tuidecl.EditorViews, func(e *widget.Editor, v string) {
		if v == "Editor.Rendered" || v == "Rendered" {
			e.SetMode(widget.Rendered)
		} else {
			e.SetMode(widget.Raw)
		}
	})
	s["renderedEnabled"] = tuidecl.BoolSetter((*widget.Editor).SetRenderedEnabled)
	s["wrap"] = tuidecl.BoolSetter(func(e *widget.Editor, v bool) { e.SetWrap(wrapMode(v)) })
	s["lineNumbers"] = tuidecl.BoolSetter((*widget.Editor).SetLineNumbers)
	s["lineNumberColor"] = tuidecl.ColorSetter((*widget.Editor).SetLineNumberColor)
	// the Raw view's guide, at this column (1-based); 0 for none
	s["ruler"] = tuidecl.NumberSetter(func(e *widget.Editor, v float64) { e.SetRuler(int(v)) })
	return s
}

// restyleEditor puts the palette's text on base under the Editor: the cells its view takes its
// colours from. An empty palette is the terminal theme's default, golib's own look.
func restyleEditor(c tui.Component, p tuidecl.Palette) {
	if e, ok := c.(*widget.Editor); ok {
		e.SetPageStyle(p.Look(tuidecl.RoleBase, tuidecl.RoleText))
	}
}
