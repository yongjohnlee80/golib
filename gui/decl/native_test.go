package decl_test

import (
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/gui"
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
			v = e.Core().Value()
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
		// no renderer: Raw is drawn, Rendered kept as the choice
		if e := c.(*widget.Editor); e.Shown() != widget.Raw || e.Mode() != widget.Rendered {
			t.Errorf("a spec with no renderer: drawn %v, chosen %v; want Raw drawn, Rendered chosen", e.Shown(), e.Mode())
		}
	})
}

// The view properties mean on gui's Editor what they mean on tui's: one document sets them all,
// runs under both styles, and gui's Editor carries each one; a setter changes it at runtime.
func TestTheViewPropertiesReachTheGuiEditor(t *testing.T) {
	src := `import tui 1.0
Window {
    Editor {
        id: ed
        focus: true
        text: "hello"
        wrap: true
        lineNumbers: true
        lineNumberColor: "#ff0000"
        ruler: 80
        renderedEnabled: false
    }
}`
	native := run(t, src, tuidecl.WithStyle(guidecl.Native()))
	onLoop(t, native, func() {
		c, _ := native.Program.Find("ed")
		e, ok := c.(*widget.Editor)
		if !ok {
			t.Fatalf("ed is a %T", c)
		}
		if e.Wrap() != tuiwidget.WrapSoft || !e.LineNumbers() || e.Ruler() != 80 {
			t.Errorf("gui Editor: wrap %v, lineNumbers %v, ruler %d; want WrapSoft, true, 80", e.Wrap(), e.LineNumbers(), e.Ruler())
		}
		if e.RenderedEnabled() {
			t.Error("renderedEnabled: false left the Rendered view available")
		}
	})
	plain := run(t, src)
	onLoop(t, plain, func() {
		c, _ := plain.Program.Find("ed")
		if e, ok := c.(*tuiwidget.Editor); !ok || e.GutterWidth() == 0 {
			t.Errorf("with no style ed is a %T with gutter %v, want tui's Editor with line numbers", c, e)
		}
	})

	// a document that sets none: the defaults are tui's Editor's (no wrap, no numbers, no ruler)
	bare := run(t, "import tui 1.0\nWindow { Editor { id: ed; text: \"hello\" } }", tuidecl.WithStyle(guidecl.Native()))
	onLoop(t, bare, func() {
		c, _ := bare.Program.Find("ed")
		e := c.(*widget.Editor)
		if e.Wrap() != tuiwidget.WrapNone || e.LineNumbers() || e.Ruler() != 0 {
			t.Errorf("defaults: wrap %v, lineNumbers %v, ruler %d; want WrapNone, false, 0", e.Wrap(), e.LineNumbers(), e.Ruler())
		}
		if !e.RenderedEnabled() {
			t.Error("by default the Rendered view is not available")
		}
	})
}

// An HTMLView under the native style is tui's widget, which a host reaches by id with no build
// tag, bound to the pixel layout gui draws; with no style it stays laid out in cells.
func TestNativeBindsTheHTMLViewsPixelLayout(t *testing.T) {
	const page = `import tui 1.0
Window { HTMLView { id: page; html: "<p>hello</p>" } }`
	for _, native := range []bool{true, false} {
		var opts []tuidecl.ProgramOption
		if native {
			opts = append(opts, tuidecl.WithStyle(guidecl.Native()))
		}
		// not run(): under the native style the cells are blank, its pixel layout draws the text
		s := decltest.Run(t, 60, 12, append([]tuidecl.ProgramOption{tuidecl.LayoutSource("main.qml", []byte(page))}, opts...)...)
		s.WaitFor(t, "the page mounted", func(string) bool {
			found := false
			onLoop(t, s, func() { _, found = tuidecl.FindAs[*tuiwidget.HTMLView](s.Program, "page") })
			return found
		})
		onLoop(t, s, func() {
			v, ok := tuidecl.FindAs[*tuiwidget.HTMLView](s.Program, "page")
			if !ok {
				t.Fatal("no HTMLView page")
			}
			_, pixels := v.BoundLayout().(interface{ Pixels() bool })
			if pixels != native {
				t.Errorf("native=%v: bound to a pixel layout = %v", native, pixels)
			}
		})
	}
	_ = widget.BindHTML
}

// An HTMLDocumentView child gives gui's Editor its HTML document view, bound to the pixel layout
// and off until the host turns it on; tui's Editor carries the declaration and draws none; a
// consumer's maker replaces golib's.
func TestAnHTMLDocumentViewReachesTheEditor(t *testing.T) {
	const src = `import tui 1.0
Window { Editor { id: ed; text: "<p>hello</p>"; MarkdownRenderer { } HTMLDocumentView { } } }`
	s := run(t, src, tuidecl.WithStyle(guidecl.Native()))
	onLoop(t, s, func() {
		c, _ := s.Program.Find("ed")
		e := c.(*widget.Editor)
		v, ok := e.DocumentView().(*tuiwidget.HTMLView)
		if !ok {
			t.Fatalf("the document view is a %T, want an HTMLView", e.DocumentView())
		}
		if c, _ := e.DocumentComponent().(*tuiwidget.HTMLView); c != v {
			t.Error("DocumentComponent is not the document view")
		}
		if _, pixels := v.BoundLayout().(interface{ Pixels() bool }); !pixels {
			t.Error("the HTMLView is not bound to the pixel layout")
		}
		if e.SetMode(widget.Rendered); e.Mode() != widget.Rendered || string(v.Source()) != "" {
			t.Errorf("before the host turns it on, Rendered is the renderer's: mode %v, view %q", e.Mode(), v.Source())
		}
		e.SetRenderedDocument(true)
		if string(v.Source()) != "<p>hello</p>" {
			t.Errorf("turned on while Rendered, the view shows %q", v.Source())
		}
	})
	cells := run(t, src)
	onLoop(t, cells, func() {
		c, _ := cells.Program.Find("ed")
		if _, ok := c.(*tuiwidget.Editor); !ok {
			t.Errorf("with no style the Editor is a %T", c)
		}
	})
	var mine *tuiwidget.HTMLView
	custom := run(t, src, tuidecl.WithStyle(guidecl.Native(guidecl.WithDocumentViewFor("html", func(tuidecl.DocumentSpec) widget.DocumentView {
		mine = tuiwidget.NewHTMLView()
		return mine
	}))))
	onLoop(t, custom, func() {
		c, _ := custom.Program.Find("ed")
		if c.(*widget.Editor).DocumentView() != widget.DocumentView(mine) || mine == nil {
			t.Error("WithDocumentViewFor's maker did not make the view")
		}
	})
}

// TestTheEditorsTabsWearThePalette: the native Editor's title-bar tabs take the palette's looks, as
// TerminalTabs's do: the active tab the highlight, another the button.
func TestTheEditorsTabsWearThePalette(t *testing.T) {
	src := `import tui 1.0
Window {
    palette.highlight: "red"
    palette.button: "blue"
    Editor { id: ed; text: "hello" }
}`
	s := run(t, src, tuidecl.WithStyle(guidecl.Native()))
	onLoop(t, s, func() {
		c, _ := s.Program.Find("ed")
		tabs := c.(*widget.Editor).Tabs()
		tabs.SetTabs([]tuiwidget.StripTab{{ID: "1", Label: "alpha"}, {ID: "2", Label: "beta"}})
		tabs.SetActive("1")
	})
	// bgOf is the background of the first cell of text on the screen, and whether it is there
	bgOf := func(text string) (tui.CellColor, bool) {
		for _, row := range s.Backend.Snapshot() {
			var line strings.Builder
			var cols []int
			for x, c := range row {
				line.WriteString(c.Content)
				for range len(c.Content) {
					cols = append(cols, x)
				}
			}
			if i := strings.Index(line.String(), text); i >= 0 {
				return row[cols[i]].Attrs.BG, true
			}
		}
		return tui.CellColor{}, false
	}
	red, blue := tui.CellColor{Kind: tui.CellColorANSI, Index: 1}, tui.CellColor{Kind: tui.CellColorANSI, Index: 4}
	s.WaitFor(t, "the tabs in the palette's looks", func(string) bool {
		a, okA := bgOf("alpha")
		b, okB := bgOf("beta")
		return okA && okB && a == red && b == blue
	})
}

// A TableView with a cellCursor under the native style shows tui's DataTable bound to gui's pixel
// layout, over the same core; with no style, or with a row cursor, the table stays in cells.
func TestNativeBindsACellCursorTableViewsPixelLayout(t *testing.T) {
	rows := func() *tuidecl.ListModel {
		m := tuidecl.NewListModel("name")
		m.SetColumns(tuidecl.Column{Role: "name", Title: "NAME"})
		m.Reset([]tuidecl.Row{{"name": "carol"}, {"name": "alice"}})
		return m
	}
	for _, tc := range []struct {
		doc            string
		native, pixels bool
	}{
		{"TableView { id: v; model: App.rows; cellCursor: true }", true, true},
		{"TableView { id: v; model: App.rows; cellCursor: true }", false, false},
		{"TableView { id: v; model: App.rows }", true, false},
	} {
		opts := []tuidecl.ProgramOption{
			tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+tc.doc)),
			tuidecl.Singleton("demo", "1.0", "App"),
			tuidecl.Sources(map[string]any{"App.rows": rows()}),
		}
		if tc.native {
			opts = append(opts, tuidecl.WithStyle(guidecl.Native()))
		}
		s := decltest.Run(t, 40, 8, opts...)
		s.WaitFor(t, "the table mounted", func(string) bool {
			found := false
			onLoop(t, s, func() { _, found = s.Program.Find("v") })
			return found
		})
		var cellCursor bool
		onLoop(t, s, func() {
			c, _ := s.Program.Find("v")
			dt := tuidecl.TableOf(c)
			cellCursor = dt != nil
			pixels := false
			if dt != nil {
				_, pixels = dt.BoundLayout().(interface{ Paint(gui.Canvas) })
			}
			if pixels != tc.pixels {
				t.Errorf("%q native=%v: bound to a pixel layout = %v", tc.doc, tc.native, pixels)
			}
		})
		if !tc.pixels {
			s.WaitForText(t, "carol") // in cells, as tui draws it
		} else if !cellCursor {
			t.Errorf("%q: no DataTable", tc.doc)
		}
	}
}
