package decl_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/widget/core"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// bench_test.go measures what the QML layer costs: mounting a document,
// reloading one that did not change, reloading one edit, moving a palette
// role that N widgets inherit — each at 10, 100 and 1000 nodes — and the
// same screen built natively and from QML. Run:
//
//	go test ./tui/decl -run '^$' -bench . -benchmem

var benchSizes = []int{10, 100, 1000}

// benchDoc is a column of n texts, each bound to one source so a reload has
// bindings to keep, under a Window whose palette they inherit.
func benchDoc(n int, edit int) string {
	var b strings.Builder
	b.WriteString("import tui 1.0\nimport demo 1.0\nWindow { palette.window: App.bg\n Flex { direction: Tui.Vertical\n")
	for i := range n {
		text := fmt.Sprintf("row %d", i)
		if i == edit {
			text = "edited"
		}
		fmt.Fprintf(&b, "  Text { id: t%d; text: %q; palette.windowText: App.fg }\n", i, text)
	}
	b.WriteString(" }\n}")
	return b.String()
}

func benchTree(b *testing.B, src string) (*decl.Tree, *tuidecl.Adapter) {
	b.Helper()
	a := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(), tuidecl.WithErrorSink(func(error) {}))...)
	tr := decl.New(a)
	if err := tr.DeclareModule(decl.Module{Name: "demo", Exports: []string{"App"}}); err != nil {
		b.Fatal(err)
	}
	for name, v := range map[string]string{"App.bg": "blue", "App.fg": "white"} {
		if err := tr.DeclareSource(name, qml.SpecValue{Kind: qml.SpecValueString, Raw: v}); err != nil {
			b.Fatal(err)
		}
	}
	spec, err := qml.QML{}.Parse([]byte(src))
	if err != nil {
		b.Fatal(err)
	}
	if err := tr.Mount(spec); err != nil {
		b.Fatal(err)
	}
	return tr, a
}

func BenchmarkMount(b *testing.B) {
	for _, n := range benchSizes {
		src := benchDoc(n, -1)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				tr, _ := benchTree(b, src)
				_ = tr.Destroy()
			}
		})
	}
}

func BenchmarkReloadUnchanged(b *testing.B) {
	for _, n := range benchSizes {
		src := benchDoc(n, -1)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			tr, _ := benchTree(b, src)
			spec, _ := qml.QML{}.Parse([]byte(src))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := tr.Reconcile(spec); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkReloadOneEdit(b *testing.B) {
	for _, n := range benchSizes {
		before, after := benchDoc(n, -1), benchDoc(n, n/2)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			tr, _ := benchTree(b, before)
			specs := [2]qml.SpecTree{}
			specs[0], _ = qml.QML{}.Parse([]byte(after))
			specs[1], _ = qml.QML{}.Parse([]byte(before))
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				res, err := tr.Reconcile(specs[i%2])
				if err != nil || res.Applied != 1 {
					b.Fatalf("res %+v, err %v", res, err)
				}
			}
		})
	}
}

// BenchmarkPaletteChange moves the Window's role: every text inherits it and
// is restyled in place.
func BenchmarkPaletteChange(b *testing.B) {
	for _, n := range benchSizes {
		src := benchDoc(n, -1)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			tr, _ := benchTree(b, src)
			colours := [2]string{"green", "blue"}
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				if _, err := tr.SetSource("App.bg", qml.SpecValue{Kind: qml.SpecValueString, Raw: colours[i%2]}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkEditorScreen builds the same screen twice: natively, as a Go
// program writes it, and from QML — parse, mount, adapter and all.
func BenchmarkEditorScreen(b *testing.B) {
	const doc = "import tui 1.0\nWindow {\n" +
		" MenuBar { Dock.edge: Tui.Top\n  Menu { title: \"&File\"\n   MenuItem { text: \"&Open\" }\n   MenuItem { text: \"&Save\" } }\n" +
		"  Menu { title: \"&Help\"\n   MenuItem { text: \"&About\" } } }\n" +
		" Frame { title: \"untitled\"; Editor { focus: true } }\n" +
		" StatusBar { Dock.edge: Tui.Bottom; left: \"NORMAL\"; center: \"untitled\"; right: \"1:1\" } }"
	b.Run("native", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			menu := core.NewMenu()
			_ = menu.SetModel([]core.MenuItemModel{
				core.NewSubmenu("file", "File", []core.MenuItemModel{
					core.NewCommand("open", "Open", nil), core.NewCommand("save", "Save", nil)}),
				core.NewSubmenu("help", "Help", []core.MenuItemModel{core.NewCommand("about", "About", nil)}),
			})
			bar := core.NewMenuBar(menu)
			frame := core.NewBox(editor.NewEditor(), core.WithTitle("untitled"))
			status := core.NewStatusBar()
			status.SetLeft("NORMAL")
			status.SetCenter("untitled")
			status.SetRight("1:1")
			dock := tui.NewDock()
			dock.Pin(tui.DockTop, bar)
			dock.Add(frame)
			dock.Pin(tui.DockBottom, status)
			_ = core.NewOverlayHost(dock)
		}
	})
	b.Run("qml", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			a := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(), tuidecl.WithErrorSink(func(error) {}))...)
			tr := decl.New(a)
			spec, err := qml.QML{}.Parse([]byte(doc))
			if err != nil {
				b.Fatal(err)
			}
			if err := tr.Mount(spec); err != nil {
				b.Fatal(err)
			}
		}
	})
}
