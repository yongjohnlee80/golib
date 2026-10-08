package decl_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A terminal build parses and carries a renderer spec; the tui Editor draws its text as ever.
func TestAnEditorCarriesARendererSpecOnATerminal(t *testing.T) {
	doc := tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window {
    Editor {
        text: "# heading"
        SyntaxHighlighter { definition: "Markdown" }
        MarkdownRenderer { headingScale: 1.8; mermaid: true }
    }
}`))
	s := decltest.Run(t, 40, 6, doc)
	s.WaitForText(t, "# heading")
}

// zoomSpec is a consumer's own renderer spec, in another package than tui/decl.
type zoomSpec struct{ Level int }

func (zoomSpec) RendererKind() string { return "zoom" }

// specCatcher is a style whose Editor records the renderer spec its declaration carries.
func specCatcher(t *testing.T) (tuidecl.Style, func() tuidecl.RendererSpec) {
	var mu sync.Mutex
	var got tuidecl.RendererSpec
	e, _ := tuidecl.StandardType("Editor")
	e.Build = func(b tuidecl.Build) (tui.Component, []string, error) {
		for _, c := range b.Children {
			if spec, ok := tuidecl.RendererOf(c); ok {
				mu.Lock()
				got = spec
				mu.Unlock()
			}
		}
		return widget.NewText("styled"), []string{"text", "wrap"}, nil
	}
	for name := range e.Setters {
		e.Setters[name] = func(tui.Component, qml.SpecValue) error { return nil }
	}
	return tuidecl.Style{Name: "catch", Types: []tuidecl.Type{e}}, func() tuidecl.RendererSpec {
		mu.Lock()
		defer mu.Unlock()
		return got
	}
}

func TestAStylesEditorFindsItsRendererSpec(t *testing.T) {
	style, spec := specCatcher(t)
	doc := tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow { Editor { MarkdownRenderer { headingScale: 1.8 } } }"))
	s := decltest.Run(t, 40, 6, doc, tuidecl.WithStyle(style))
	s.WaitForText(t, "styled")
	if got, ok := spec().(tuidecl.MarkdownSpec); !ok || got.HeadingScale != 1.8 || got.Mermaid {
		t.Fatalf("the style's Editor found %#v, want MarkdownSpec{1.8, false}", spec())
	}
}

// A consumer declares its own renderer through its own Type: RendererNode carries the spec.
func TestAConsumersRendererSpecReachesTheStylesEditor(t *testing.T) {
	style, spec := specCatcher(t)
	zoom := tuidecl.Type{Name: "ZoomRenderer", Ctor: []string{"level"},
		Build: func(b tuidecl.Build) (tui.Component, []string, error) {
			var level float64
			consumed, err := tuidecl.ReadProps(b.Props, map[string]tuidecl.Field{"level": tuidecl.NumberField(&level)})
			return tuidecl.RendererNode(zoomSpec{Level: int(level)}), consumed, err
		}}
	doc := tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow { Editor { ZoomRenderer { level: 3 } } }"))
	s := decltest.Run(t, 40, 6, doc, tuidecl.WithStyle(style), tuidecl.Types(zoom))
	s.WaitForText(t, "styled")
	if got, ok := spec().(zoomSpec); !ok || got.Level != 3 || got.RendererKind() != "zoom" {
		t.Fatalf("the style's Editor found %#v, want zoomSpec{3}", spec())
	}
}

func TestARendererSpecIsRefusedOutsideAnEditor(t *testing.T) {
	for doc, want := range map[string]string{
		"Window { Text { MarkdownRenderer { } } }":                        "is not one",
		"Window { Editor { MarkdownRenderer { } MarkdownRenderer { } } }": "one renderer",
		"MarkdownRenderer { }":                                            "the root is in none",
		"Window { Editor { MarkdownRenderer { headingScale: -1 } } }":     "negative",
	} {
		_, err := tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\n"+doc)))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want an error saying %q", doc, err, want)
		}
	}
}
