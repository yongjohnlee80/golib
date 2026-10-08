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
	"github.com/yongjohnlee80/golib/tui/style"
)

// NativeOption sets up the native style.
type NativeOption func(*native)

type native struct {
	renderers map[string]func(tuidecl.RendererSpec) widget.Renderer
}

// WithRendererFor makes the renderer of a spec of kind (its RendererKind) with f: a consumer's
// own renderer, declared through its own spec type and tuidecl.RendererNode. "markdown" is built
// in; f for it replaces golib's. A spec whose kind has no renderer leaves the Editor Raw only.
func WithRendererFor(kind string, f func(tuidecl.RendererSpec) widget.Renderer) NativeOption {
	return func(n *native) { n.renderers[kind] = f }
}

// Native is gui's style. It replaces Editor; every other type stays tui's.
func Native(opts ...NativeOption) tuidecl.Style {
	n := &native{renderers: map[string]func(tuidecl.RendererSpec) widget.Renderer{
		"markdown": markdownRenderer,
	}}
	for _, o := range opts {
		o(n)
	}
	ed, _ := tuidecl.StandardType("Editor")
	ed.Build = n.buildEditor
	ed.Setters = editorSetters()
	ed.Restyle = restyleEditor
	return tuidecl.Style{Name: "native", Types: []tuidecl.Type{ed}}
}

// markdownRenderer is golib's renderer for a MarkdownRenderer declaration.
func markdownRenderer(s tuidecl.RendererSpec) widget.Renderer {
	var opts []widget.MarkdownOption
	if m, ok := s.(tuidecl.MarkdownSpec); ok && m.HeadingScale > 0 {
		opts = append(opts, widget.HeadingScale(m.HeadingScale))
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
	opts := []widget.EditorOption{widget.WithCore(d.CoreOptions()...)}
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

// editorSetters are the Editor's runtime properties on gui's Editor: the behaviour ones shared
// with tui's, and its own view ones. The contract is tui's Editor's, property for property; a
// view property gui's Editor does not draw yet is accepted and changes nothing, so a document
// runs unchanged under either style.
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
	// Accepted, not drawn: gui's Editor always wraps at its width, and has no gutter or ruler.
	s["wrap"] = tuidecl.BoolSetter(func(*widget.Editor, bool) {})
	s["lineNumbers"] = tuidecl.BoolSetter(func(*widget.Editor, bool) {})
	s["lineNumberColor"] = tuidecl.ColorSetter(func(*widget.Editor, style.Color) {})
	s["ruler"] = tuidecl.NumberSetter(func(*widget.Editor, float64) {})
	return s
}

// restyleEditor puts the palette's text on base under the Editor: the cells its view takes its
// colours from. An empty palette is the terminal theme's default, golib's own look.
func restyleEditor(c tui.Component, p tuidecl.Palette) {
	if e, ok := c.(*widget.Editor); ok {
		e.SetPageStyle(p.Look(tuidecl.RoleBase, tuidecl.RoleText))
	}
}
