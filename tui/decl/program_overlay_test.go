package decl_test

import (
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TestAGoProgramAttachesALayerToTheWindow: Program.Overlay is the root Window's OverlayHost, so a
// Go program shows a native widget the document does not declare (toasts) over the page, the
// status line under them untouched. A root that is not a Window has none.
func TestAGoProgramAttachesALayerToTheWindow(t *testing.T) {
	s := decltest.Run(t, 40, 8, tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window {
    Text { text: "the page" }
    StatusBar { Dock.edge: Tui.Bottom; left: "NORMAL" }
}`)))
	s.WaitForText(t, "NORMAL")
	toasts := widget.NewToasts(widget.WithToastMargin(1))
	onScreenLoop(t, s, func() {
		host, ok := s.Program.Overlay()
		if !ok {
			t.Error("a Window's program has no overlay")
			return
		}
		host.Attach(toasts.Float())
		toasts.Post(widget.Toast{Text: "saved"})
	})
	s.WaitForText(t, "saved")
	rows := strings.Split(s.String(), "\n")
	if last := rows[len(rows)-1]; !strings.HasPrefix(last, "NORMAL") {
		t.Fatalf("the status line %q, want it untouched under the toast:\n%s", last, s)
	}

	bare := decltest.Run(t, 20, 3, tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nText { text: \"x\" }")))
	bare.WaitForText(t, "x")
	onScreenLoop(t, bare, func() {
		if _, ok := bare.Program.Overlay(); ok {
			t.Error("a Text root answered an overlay")
		}
	})
}

// TestAnEditorsWrapAndLineNumbersAreLive: wrap and lineNumbers, bound, change the editor as the
// program runs.
func TestAnEditorsWrapAndLineNumbersAreLive(t *testing.T) {
	s := decltest.Run(t, 20, 4, tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
import demo 1.0
Window {
    Editor { text: "one two three four five"; wrap: App.wrap; lineNumbers: App.numbers }
}`)), tuidecl.Singleton("demo", "1.0", "App"), tuidecl.Sources(map[string]any{"App.wrap": false, "App.numbers": false}))
	s.WaitForText(t, "one two")
	if strings.Contains(s.String(), "five") {
		t.Fatalf("unwrapped, the line's end shows:\n%s", s)
	}
	s.Program.Post(func() {
		_ = s.Program.Set("App.wrap", true)
		_ = s.Program.Set("App.numbers", true)
	})
	s.WaitFor(t, "wrapped and numbered", func(sc string) bool {
		return strings.HasPrefix(sc, "  1 one two") && strings.Contains(sc, "five")
	})
}
