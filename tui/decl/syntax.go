package decl

import (
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// SYNTAX HIGHLIGHTING in QML — KDE KSyntaxHighlighting's type.
//
//	import editor.theme.retro 1.0
//
//	Window {
//	    syntax.keyword: Theme.syntax.keyword    // set once, where the theme is
//	    syntax.string: Theme.syntax.string
//	    syntax.comment: Theme.syntax.comment
//
//	    Editor {
//	        SyntaxHighlighter { definition: App.syntax }   // "QML", or "" for none
//	    }
//	}
//
// QML selects; Go highlights; the palette colours. KDE's SyntaxHighlighter
// takes its editor by reference (`textEdit: editor`); this evaluator passes no
// object references, so it highlights the Editor it is declared in, and
// anywhere else is refused.
//
// `definition` names a registered definition — the standard vocabulary has
// QML and JavaScript; a program adds its own with [WithHighlighters] or the
// Program option [Highlighters] — and an unknown one is refused, naming the
// registered ones. It is a runtime property: bound to a source, the language
// follows it. "" turns highlighting off.
//
// The colours are the `syntax.<style>` roles — `keyword`, `controlFlow`,
// `dataType`, `string`, `comment`, … (package highlight lists all 31) — which
// PROPAGATE like palette roles (palette.go): written on the Window, every
// highlighter under it wears them, a FileDialog's preview as well as an
// Editor's. A style left unset paints as `syntax.normal`, and that unset as
// the text.

// stdHighlighters are the definitions the standard vocabulary registers, with
// the files each is for — what a FileDialog's preview picks by.
func stdHighlighters() *highlight.Repository {
	q := qml.Highlighter()
	return highlight.NewRepository(
		highlight.Definition{Name: "QML", Extensions: []string{"*.qml"}, Highlighter: q},
		highlight.Definition{Name: "JavaScript", Extensions: []string{"*.js", "*.mjs"}, Highlighter: q},
	)
}

// WithHighlighters registers syntax definitions by the name a document's
// `definition:` gives them, and the file names a preview picks them by. A
// name the vocabulary already has is replaced.
func WithHighlighters(defs ...highlight.Definition) Option {
	return func(a *Adapter) { a.highlighters.Add(defs...) }
}

// syntaxNode is a SyntaxHighlighter: the definition it names, the colours it
// inherits, and the Editor it is in, once that Editor is built.
type syntaxNode struct {
	widget.Base
	registry   *highlight.Repository
	definition string
	styles     widget.SyntaxStyles
	editor     *widget.Editor
}

func buildSyntaxHighlighter(b Build) (tui.Component, []string, error) {
	if len(b.Children) != 0 {
		return nil, nil, fmt.Errorf("a SyntaxHighlighter takes no children (at %s)", b.Pos)
	}
	return &syntaxNode{registry: b.highlighters}, nil, nil
}

// attach binds the highlighter to the Editor it is declared in.
func (n *syntaxNode) attach(e *widget.Editor) {
	n.editor = e
	n.apply()
}

func (n *syntaxNode) apply() {
	if n.editor == nil {
		return
	}
	n.editor.WithSyntaxStyles(n.styles)
	var h highlight.Highlighter
	if d, ok := n.registry.Definition(n.definition); ok && n.definition != "" {
		h = d.Highlighter
	}
	n.editor.SetHighlighter(h)
}

func (n *syntaxNode) setDefinition(v qml.SpecValue) error {
	name, err := stringOf(v)
	if err != nil {
		return fmt.Errorf("definition: %w", err)
	}
	if _, ok := n.registry.Definition(name); name != "" && !ok {
		return fmt.Errorf("definition: %q is not a registered highlighter; registered: %s (at %s)",
			name, strings.Join(n.registry.Names(), ", "), v.Pos)
	}
	n.definition = name
	n.apply()
	return nil
}

// restyleSyntax dresses a highlighter in the syntax roles it inherits.
func restyleSyntax(c tui.Component, p palette) {
	n := c.(*syntaxNode)
	n.styles = p.syntaxStyles()
	n.apply()
}

func syntaxSetters() map[string]Setter {
	return map[string]Setter{
		"definition": func(c tui.Component, v qml.SpecValue) error {
			n, ok := c.(*syntaxNode)
			if !ok {
				return fmt.Errorf("definition is a SyntaxHighlighter's")
			}
			return n.setDefinition(v)
		},
	}
}

// A SyntaxHighlighter takes no place in the layout.
func (n *syntaxNode) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (n *syntaxNode) Render(tui.Surface)                {}

// editorChildren takes an Editor's children: SyntaxHighlighters, and nothing
// else — anything else would be silently lost, since an Editor lays out none.
func editorChildren(b Build) ([]*syntaxNode, error) {
	var out []*syntaxNode
	for _, c := range b.Children {
		n, ok := c.(*syntaxNode)
		if !ok {
			return nil, fmt.Errorf("an Editor holds only a SyntaxHighlighter, and lays out nothing (at %s)", b.Pos)
		}
		out = append(out, n)
	}
	if len(out) > 1 {
		return nil, fmt.Errorf("an Editor holds one SyntaxHighlighter, got %d (at %s)", len(out), b.Pos)
	}
	return out, nil
}

// VetRoot implements [decl.RootVetter]: a SyntaxHighlighter is refused where
// its parent is built unless that parent is an Editor, and a root has no
// parent — so it is refused here, not left mounted highlighting nothing.
func (a *Adapter) VetRoot(id decl.NodeID) error {
	if b, ok := a.nodes[id]; ok && b.typ == "SyntaxHighlighter" {
		return fmt.Errorf("a SyntaxHighlighter highlights the Editor it is declared in, and the root is in none")
	}
	return nil
}

var _ decl.RootVetter = (*Adapter)(nil)
