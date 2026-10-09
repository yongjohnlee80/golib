package decl

import (
	"github.com/yongjohnlee80/golib/gui"
	guiwidget "github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/indent"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"image/color"
	"strings"
	"testing"
)

func TestNativeDeclarativeSourceAndRenderedFenceShareTheFinalCatalog(t *testing.T) {
	var renderer guiwidget.Renderer
	var seen []*highlight.Catalog
	custom := highlight.Definition{Name: "Custom", Extensions: []string{"*.custom"}, Aliases: []string{"custom"}, SourceFactory: func(c *highlight.Catalog) highlight.Source {
		seen = append(seen, c)
		return highlight.Source{Highlighter: highlight.HighlighterFunc(func(line string, previous highlight.State) ([]highlight.Span, highlight.State) {
			at := strings.Index(line, "token")
			if at < 0 {
				return nil, previous
			}
			return []highlight.Span{{Start: at, End: at + 5, Style: highlight.Keyword}}, previous
		}), Indenter: indent.PolicyFunc(func(r indent.Request) (indent.Decision, bool) { return indent.Decision{Prefix: "   "}, r.StateKnown })}
	}}
	style := Native(func(n *native) {
		original := n.renderers["markdown"]
		n.renderers["markdown"] = func(spec tuidecl.RendererSpec) guiwidget.Renderer { renderer = original(spec); return renderer }
	})
	p, err := tuidecl.NewProgram(tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(60, 12))), tuidecl.WithStyle(style), tuidecl.Highlighters(custom), tuidecl.LayoutSource("custom.qml", []byte(`import tui 1.0
Window { Editor { id: ed; text: "token"; autoIndent: true
SyntaxHighlighter { definition: "Custom" }
MarkdownRenderer {}
} }`)))
	if err != nil {
		t.Fatal(err)
	}
	c, _ := p.Find("ed")
	ed := c.(*guiwidget.Editor)
	f := ed.Core().BeginHighlight(0)
	styles := f.Styles(0)
	f.Close()
	if styles[0] != highlight.Keyword {
		t.Fatal(styles)
	}
	file, ok := p.SourceLanguages().DefinitionForFileName("a.custom")
	if !ok || file.Name != "Custom" {
		t.Fatal(file, ok)
	}
	lines := []string{"  ```custom", "  é token", "  ```"}
	th := guiwidget.Theme{Text: color.NRGBA{A: 255}, Mono: gui.Font{Size: 14}, Prose: gui.Font{Size: 14}}
	th.Syntax[highlight.Keyword] = color.NRGBA{R: 255, A: 255}
	canvas := gui.NewRecordingCanvas(gui.Size{W: 400, H: 200}, gui.Size{W: 8, H: 16})
	layout := renderer.LayOut(guiwidget.Block{From: 0, To: 3}, lines, 400, false, canvas.Text(), th, nil)
	coloured := false
	for _, span := range layout.Lines[1].Spans {
		if strings.Contains(span.Text, "token") && span.Color == th.Syntax[highlight.Keyword] {
			coloured = true
			if span.Line != 1 || span.Col != 4 {
				t.Fatal("source mapping changed", span)
			}
		}
	}
	if !coloured {
		t.Fatal("actual native renderer lost the injected provider", layout.Lines[1].Spans)
	}
	for _, catalog := range seen {
		if catalog != p.SourceLanguages() {
			t.Fatal("source and renderer got different catalogs")
		}
	}
	if len(seen) < 2 {
		t.Fatal("renderer did not create the registered provider")
	}
}
