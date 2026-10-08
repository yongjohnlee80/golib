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
//	    view: Editor.Rendered
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

// DOCUMENT VIEW SPECS: a whole-document Rendered view, declared as an Editor's child beside its
// renderer.
//
//	Editor {
//	    MarkdownRenderer {}   // Rendered, line by line: the text stays editable
//	    HTMLDocumentView {}   // Rendered, the whole document as a page: for a host's HTML files
//	}
//
// A renderer lays the text out line by line; a document view draws the whole document as one
// read-only widget, which the host turns on for a document it reads (gui's
// Editor.SetRenderedDocument). Like a renderer spec it is plain data: the tui Editor has no
// Rendered view and ignores it, and a style's Editor makes the view from it.

// DocumentSpec is a document view's settings as a document declares them.
type DocumentSpec interface {
	// DocumentKind names the view, "html" or a consumer's: a style finds what builds it by this.
	DocumentKind() string
}

// HTMLDocumentSpec is golib's HTML document view: an HTMLView showing the text as a page.
type HTMLDocumentSpec struct{}

// DocumentKind is "html".
func (HTMLDocumentSpec) DocumentKind() string { return "html" }

// documentNode is a document view spec as an Editor's declaration-only child.
type documentNode struct {
	widget.Base
	spec DocumentSpec
}

// DocumentNode is spec as a child an Editor accepts: what a document view Type's Build returns.
func DocumentNode(spec DocumentSpec) tui.Component { return &documentNode{spec: spec} }

// DocumentOf is the spec of a document view child, and whether c is one.
func DocumentOf(c tui.Component) (DocumentSpec, bool) {
	n, ok := c.(*documentNode)
	if !ok {
		return nil, false
	}
	return n.spec, true
}

// A document view spec takes no place in the layout.
func (n *documentNode) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (*documentNode) declarationOnly()                    {}
func (n *documentNode) Render(tui.Surface)                {}

// buildHTMLDocumentView declares golib's HTML document view.
func buildHTMLDocumentView(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("an HTMLDocumentView takes no children (at %s)", b.Pos)
	}
	consumed, err := readProps(b.Props, map[string]field{})
	if err != nil {
		return nil, nil, err
	}
	return DocumentNode(HTMLDocumentSpec{}), consumed, nil
}

// htmlDocumentViewType is the standard vocabulary's HTMLDocumentView.
var htmlDocumentViewType = Type{Name: "HTMLDocumentView", Build: buildHTMLDocumentView}
