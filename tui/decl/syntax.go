package decl

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// SYNTAX HIGHLIGHTING in QML — KDE KSyntaxHighlighting's type.
//
//	import editor.theme.retro 1.0
//
//	Editor {
//	    SyntaxHighlighter {
//	        definition: App.syntax              // "QML", or "" for none
//	        theme.keyword: Theme.syntax.keyword
//	        theme.string: Theme.syntax.string
//	        theme.comment: Theme.syntax.comment
//	    }
//	}
//
// QML selects and configures; Go highlights. KDE's SyntaxHighlighter takes
// its editor by reference (`textEdit: editor`); this evaluator passes no
// object references, so it highlights the Editor it is declared in, and
// anywhere else is refused.
//
// `definition` names a registered highlighter — the standard vocabulary has
// QML (with its JavaScript); a program adds its own with [WithHighlighters] or
// the Program option [Highlighters] — and an unknown one is refused, naming
// the registered ones. It is a runtime property: bound to a source, the
// language follows it. "" turns highlighting off.
//
// `theme.<style>` colours one of KSyntaxHighlighting's styles — `keyword`,
// `controlFlow`, `dataType`, `string`, `comment`, … (package highlight lists
// all 31) — in the palette's colour syntax. Bound to a theme module's `syntax`
// group, switching theme stays the import line alone. A style left unset
// paints as the editor's text.

// stdHighlighters are the definitions the standard vocabulary registers.
func stdHighlighters() map[string]highlight.Highlighter {
	q := qml.Highlighter()
	return map[string]highlight.Highlighter{"QML": q, "JavaScript": q}
}

// WithHighlighters registers syntax highlighters by the name a document's
// `definition:` gives them. A name the vocabulary already has is replaced.
func WithHighlighters(hs map[string]highlight.Highlighter) Option {
	return func(a *Adapter) {
		for name, h := range hs {
			a.highlighters[name] = h
		}
	}
}

// syntaxNode is a SyntaxHighlighter: the definition it names, its colours,
// and the Editor it is in, once that Editor is built.
type syntaxNode struct {
	widget.Base
	registry   map[string]highlight.Highlighter
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
	if n.definition != "" {
		h = n.registry[n.definition]
	}
	n.editor.SetHighlighter(h)
}

func (n *syntaxNode) setDefinition(v qml.SpecValue) error {
	name, err := stringOf(v)
	if err != nil {
		return fmt.Errorf("definition: %w", err)
	}
	if _, ok := n.registry[name]; name != "" && !ok {
		names := make([]string, 0, len(n.registry))
		for k := range n.registry {
			names = append(names, k)
		}
		sort.Strings(names)
		return fmt.Errorf("definition: %q is not a registered highlighter; registered: %s (at %s)",
			name, strings.Join(names, ", "), v.Pos)
	}
	n.definition = name
	n.apply()
	return nil
}

// themeSetters are one colour property per highlight style: `theme.keyword`.
func themeSetters() map[string]Setter {
	out := map[string]Setter{}
	for i := range highlight.Styles {
		st := highlight.Style(i)
		out["theme."+st.String()] = func(c tui.Component, v qml.SpecValue) error {
			n, ok := c.(*syntaxNode)
			if !ok {
				return fmt.Errorf("theme.%s is a SyntaxHighlighter's", st)
			}
			col, err := colorOf(v)
			if err != nil {
				return fmt.Errorf("theme.%s: %w", st, err)
			}
			n.styles[st] = style.New().Foreground(col)
			n.apply()
			return nil
		}
	}
	return out
}

func syntaxSetters() map[string]Setter {
	out := themeSetters()
	out["definition"] = func(c tui.Component, v qml.SpecValue) error {
		n, ok := c.(*syntaxNode)
		if !ok {
			return fmt.Errorf("definition is a SyntaxHighlighter's")
		}
		return n.setDefinition(v)
	}
	return out
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
