package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// RENDERER SPECS: how an Editor that has a Rendered view draws it, declared as a child, as
// SyntaxHighlighter is.
//
//	Editor {
//	    mode: Editor.Rendered
//	    SyntaxHighlighter { definition: "Markdown" }
//	    MarkdownRenderer { headingScale: 1.8 }
//	}
//
// A spec is plain data, so a terminal build parses and carries it without the toolkit that
// draws it; the tui Editor has no Rendered view and ignores it. A style's Editor reads it with
// RendererOf and makes its renderer. A consumer's own renderer is a spec type in its own
// package, declared through a Type whose Build returns RendererNode(spec).

// RendererSpec is a renderer's settings as a document declares them.
type RendererSpec interface {
	// RendererKind names the renderer, "markdown" or a consumer's: a style finds the
	// renderer that draws it by this.
	RendererKind() string
}

// MarkdownSpec is golib's Markdown renderer's settings.
type MarkdownSpec struct {
	HeadingScale float32 // h1's size as a multiple of the text's; 0: the renderer's default
	Mermaid      bool    // draw mermaid fences as diagrams
}

// RendererKind is "markdown".
func (MarkdownSpec) RendererKind() string { return "markdown" }

// rendererNode is a renderer spec as an Editor's declaration-only child.
type rendererNode struct {
	widget.Base
	spec RendererSpec
}

// RendererNode is spec as a child an Editor accepts: what a renderer Type's Build returns.
func RendererNode(spec RendererSpec) tui.Component { return &rendererNode{spec: spec} }

// RendererOf is the spec of a renderer child, and whether c is one.
func RendererOf(c tui.Component) (RendererSpec, bool) {
	n, ok := c.(*rendererNode)
	if !ok {
		return nil, false
	}
	return n.spec, true
}

// A renderer spec takes no place in the layout.
func (n *rendererNode) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (*rendererNode) declarationOnly()                    {}
func (n *rendererNode) Render(tui.Surface)                {}

// buildMarkdownRenderer declares golib's Markdown renderer.
func buildMarkdownRenderer(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("a MarkdownRenderer takes no children (at %s)", b.Pos)
	}
	var scale float64
	var mermaid bool
	consumed, err := readProps(b.Props, map[string]field{
		"headingScale": into(&scale, numberOf),
		"mermaid":      into(&mermaid, boolOf),
	})
	if err != nil {
		return nil, nil, err
	}
	if scale < 0 {
		return nil, nil, fmt.Errorf("headingScale: %v is negative (at %s)", scale, b.Pos)
	}
	return RendererNode(MarkdownSpec{HeadingScale: float32(scale), Mermaid: mermaid}), consumed, nil
}

// markdownRendererType is the standard vocabulary's MarkdownRenderer. Its settings are taken at
// construction: a change rebuilds the Editor's renderer.
var markdownRendererType = Type{Name: "MarkdownRenderer", Build: buildMarkdownRenderer,
	Ctor: []string{"headingScale", "mermaid"}}
