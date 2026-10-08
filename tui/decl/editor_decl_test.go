package decl_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// The ADR's document runs under the standard vocabulary: the cell Editor takes mode, carries the
// renderer, and stays Raw, showing its source as written.
func TestTheRenderedEditorDocumentRunsOnATerminal(t *testing.T) {
	doc := tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window {
    Editor {
        text: "## Title"
        view: Editor.Rendered
        SyntaxHighlighter { definition: "Markdown" }
        MarkdownRenderer { headingScale: 1.8 }
    }
}`))
	s := decltest.Run(t, 40, 6, doc)
	s.WaitForText(t, "## Title")
}

func TestAnEditorRefusesAModeItDoesNotHave(t *testing.T) {
	_, err := tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow { Editor { view: Editor.Pretty } }")))
	if err == nil || !strings.Contains(err.Error(), "Editor.Pretty") {
		t.Fatalf("view: Editor.Pretty: %v, want it refused by name", err)
	}
}

// fakeEditor is another package's editor widget: it has the behaviour methods EditorSetters call,
// and records them.
type fakeEditor struct {
	*widget.Text
	mu     sync.Mutex
	core   *widget.EditorCore
	keyset widget.Keyset
	ro     bool
	value  string
	cursor int
	mode   string
	spec   tuidecl.RendererSpec
}

func (f *fakeEditor) SetKeyset(k widget.Keyset) { f.mu.Lock(); f.keyset = k; f.mu.Unlock() }
func (f *fakeEditor) SetReadOnly(v bool)        { f.mu.Lock(); f.ro = v; f.mu.Unlock() }
func (f *fakeEditor) SetValue(s string)         { f.mu.Lock(); f.value = s; f.mu.Unlock() }
func (f *fakeEditor) SetCursorPosition(p int)   { f.mu.Lock(); f.cursor = p; f.mu.Unlock() }

// A style's Editor reads the declaration with ReadEditor and takes the behaviour properties
// through EditorSetters: the same meaning as the cell Editor's, from another widget.
func TestAStylesEditorSharesTheDeclarationsMeaning(t *testing.T) {
	var made *fakeEditor
	var mu sync.Mutex
	e, _ := tuidecl.StandardType("Editor")
	e.Build = func(b tuidecl.Build) (tui.Component, []string, error) {
		d, err := tuidecl.ReadEditor(b)
		if err != nil {
			return nil, nil, err
		}
		f := &fakeEditor{Text: widget.NewText("styled " + d.Text), core: widget.NewEditorCore(d.CoreOptions()...), spec: d.Renderer}
		d.Attach(f.core)
		mu.Lock()
		made = f
		mu.Unlock()
		return f, d.Consumed, nil
	}
	for name, set := range tuidecl.EditorSetters() {
		e.Setters[name] = set
	}
	e.Setters["view"] = tuidecl.EnumSetter(tuidecl.EditorViews, func(f *fakeEditor, v string) { f.mu.Lock(); f.mode = v; f.mu.Unlock() })
	style := tuidecl.Style{Name: "fake", Types: []tuidecl.Type{e}}
	decltest.CheckStyle(t, style)

	doc := tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window {
    Editor {
        text: "doc"
        keyset: Tui.Nano
        readOnly: true
        cursorPosition: 2
        view: Editor.Rendered
        MarkdownRenderer { }
    }
}`))
	s := decltest.Run(t, 40, 6, doc, tuidecl.WithStyle(style))
	s.WaitForText(t, "styled doc")
	s.WaitFor(t, "the properties applied", func(string) bool {
		mu.Lock()
		f := made
		mu.Unlock()
		if f == nil {
			return false
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.keyset == widget.KeysetNano && f.ro && f.cursor == 2 && f.mode == "Rendered"
	})
	mu.Lock()
	defer mu.Unlock()
	if _, ok := made.spec.(tuidecl.MarkdownSpec); !ok {
		t.Errorf("the declaration's renderer: %#v", made.spec)
	}
	if got := made.core.Value(); got != "doc" {
		t.Errorf("the core's text %q, want the declaration's", got)
	}
}
