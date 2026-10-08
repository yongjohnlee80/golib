package decl_test

import (
	"testing"
	"time"

	guidecl "github.com/yongjohnlee80/golib/gui/decl"
	"github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// The native style's Editor offers tui's Editor's QML contract, property for property.
func TestNativeKeepsTheEditorsContract(t *testing.T) {
	decltest.CheckStyle(t, guidecl.Native())
}

const doc = `import tui 1.0
Window {
    Editor {
        id: ed
        focus: true
        text: "hello"
        view: Editor.Rendered
        SyntaxHighlighter { definition: "QML" }
        MarkdownRenderer { headingScale: 1.8 }
    }
}`

func run(t *testing.T, src string, opts ...tuidecl.ProgramOption) *decltest.Screen {
	t.Helper()
	s := decltest.Run(t, 60, 12, append([]tuidecl.ProgramOption{tuidecl.LayoutSource("main.qml", []byte(src))}, opts...)...)
	s.WaitForText(t, "hello")
	return s
}

func onLoop(t *testing.T, s *decltest.Screen, fn func()) {
	t.Helper()
	done := make(chan struct{})
	s.Program.Post(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the loop did not run the update")
	}
}

// valueOf is the editor id's text, whichever editor the style built.
func valueOf(t *testing.T, s *decltest.Screen) string {
	t.Helper()
	var v string
	onLoop(t, s, func() {
		c, ok := s.Program.Find("ed")
		if !ok {
			t.Error("no node ed")
			return
		}
		switch e := c.(type) {
		case *widget.Editor:
			v = e.Core().Value()
		case *tuiwidget.Editor:
			v = e.Value()
		default:
			t.Errorf("ed is a %T", c)
		}
	})
	return v
}

// Under the native style the document's Editor is gui's, Rendered with golib's Markdown
// renderer; with no style it is tui's, which accepts the same declaration and stays Raw.
func TestOneDocumentTwoEditors(t *testing.T) {
	native := run(t, doc, tuidecl.WithStyle(guidecl.Native()))
	onLoop(t, native, func() {
		c, _ := native.Program.Find("ed")
		e, ok := c.(*widget.Editor)
		if !ok {
			t.Fatalf("under the native style ed is a %T, want gui's Editor", c)
		}
		if e.Mode() != widget.Rendered {
			t.Errorf("view: Editor.Rendered left the gui Editor %v", e.Mode())
		}
	})
	plain := run(t, doc)
	onLoop(t, plain, func() {
		if c, _ := plain.Program.Find("ed"); func() bool { _, ok := c.(*tuiwidget.Editor); return !ok }() {
			t.Fatalf("with no style ed is a %T, want tui's Editor", c)
		}
	})

	// one key script, two editors, one text
	script := append([]tui.Event{decltest.Rune('A')}, decltest.Type(" world")...)
	script = append(script, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape}, decltest.Rune('0'), decltest.Rune('x'))
	for _, s := range []*decltest.Screen{native, plain} {
		s.Keys(t, script...)
	}
	want := "ello world"
	for name, s := range map[string]*decltest.Screen{"native": native, "plain": plain} {
		deadline := time.Now().Add(3 * time.Second)
		for valueOf(t, s) != want && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if got := valueOf(t, s); got != want {
			t.Errorf("%s editor: %q, want %q", name, got, want)
		}
	}
}

// fooSpec is a consumer's renderer spec: plain data, declared through its own type.
type fooSpec struct{}

func (fooSpec) RendererKind() string { return "foo" }

// A consumer's renderer reaches the gui Editor through WithRendererFor; a kind with no renderer
// leaves the Editor Raw only.
func TestAConsumersRendererReachesTheEditor(t *testing.T) {
	fooType := tuidecl.Type{Name: "FooRenderer", Build: func(tuidecl.Build) (tui.Component, []string, error) {
		return tuidecl.RendererNode(fooSpec{}), nil, nil
	}}
	src := `import tui 1.0
Window { Editor { id: ed; text: "hello"; view: Editor.Rendered; FooRenderer { } } }`
	made := 0
	withFoo := guidecl.Native(guidecl.WithRendererFor("foo", func(s tuidecl.RendererSpec) widget.Renderer {
		if _, ok := s.(fooSpec); !ok {
			t.Errorf("the factory got a %T", s)
		}
		made++
		return widget.NewMarkdownRenderer()
	}))
	s := run(t, src, tuidecl.Types(fooType), tuidecl.WithStyle(withFoo))
	onLoop(t, s, func() {
		c, _ := s.Program.Find("ed")
		if e := c.(*widget.Editor); e.Mode() != widget.Rendered || made != 1 {
			t.Errorf("with a foo renderer: mode %v, factory called %d times", e.Mode(), made)
		}
	})
	none := run(t, src, tuidecl.Types(fooType), tuidecl.WithStyle(guidecl.Native()))
	onLoop(t, none, func() {
		c, _ := none.Program.Find("ed")
		if e := c.(*widget.Editor); e.Mode() != widget.Raw {
			t.Errorf("a spec with no renderer: mode %v, want Raw", e.Mode())
		}
	})
}
