package decl_test

import (
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// TestAStatusBarsChildrenArePermanentWidgets: a StatusBar's children are Qt's permanent widgets:
// each at its own width at the bar's right end, in order, a column apart, the right segment ending
// a column before them; a Text's color is its own, on the bar's window; a child that changes width
// moves the ones before it and the segments with it.
func TestAStatusBarsChildrenArePermanentWidgets(t *testing.T) {
	const w = 40
	s := decltest.Run(t, w, 3, tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
import demo 1.0
Window {
    StatusBar {
        Dock.edge: Tui.Bottom
        palette.window: "red"
        palette.windowText: "white"
        left: "NORMAL"
        right: "saved"
        Text { text: "●"; color: App.dot }
        Text { text: App.search }
    }
}`)), tuidecl.Singleton("demo", "1.0", "App"), tuidecl.Sources(map[string]any{"App.dot": "green", "App.search": "lexical"}))
	s.WaitForText(t, "lexical")
	row := s.Backend.Snapshot()[2]
	text := func(from, to int) string {
		out := ""
		for _, c := range row[from:to] {
			out += c.Content
		}
		return out
	}
	// "…saved ● lexical": lexical ends the row, the dot a column before it, saved a column before that
	if got := text(w-7, w); got != "lexical" {
		t.Fatalf("the row ends %q, want the last child, lexical:\n%s", got, s)
	}
	if got := text(w-9, w-7); got != "● " {
		t.Fatalf("before lexical %q, want the first child and a gap:\n%s", got, s)
	}
	if got := text(w-15, w-9); got != "saved " {
		t.Fatalf("before the children %q, want the right segment and a gap:\n%s", got, s)
	}
	if got := text(0, 6); got != "NORMAL" {
		t.Errorf("the left segment %q", got)
	}
	dot := row[w-9]
	if dot.Attrs.FG != ansi(green) || dot.Attrs.BG != ansi(red) {
		t.Errorf("the dot wears %+v on %+v, want its own green on the bar's red", dot.Attrs.FG, dot.Attrs.BG)
	}
	for x, c := range row {
		if c.Attrs.BG != ansi(red) {
			t.Errorf("column %d %q is off the bar's window: %+v", x, c.Content, c.Attrs.BG)
			break
		}
	}
	// bound: the text grows, the colour turns, and the row is laid out again
	s.Program.Post(func() {
		_ = s.Program.Set("App.search", "semantic!")
		_ = s.Program.Set("App.dot", "red")
	})
	s.WaitForText(t, "saved ● semantic!")
	row = s.Backend.Snapshot()[2]
	if dot := row[w-11]; dot.Content != "●" || dot.Attrs.FG != ansi(red) {
		t.Errorf("after the change, column %d is %q in %+v, want the dot, red", w-11, dot.Content, dot.Attrs.FG)
	}
}

// TestAStatusBarsNormalWidgetsSitAtTheLeft: a child with StatusBar.permanent: false is a normal
// widget (Qt's addWidget), at the bar's left end before the left segment, a column apart; a
// permanent one stays at the right; the dot keeps its own colour.
func TestAStatusBarsNormalWidgetsSitAtTheLeft(t *testing.T) {
	const w = 40
	s := decltest.Run(t, w, 3, tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window {
    StatusBar {
        Dock.edge: Tui.Bottom
        palette.window: "red"
        palette.windowText: "white"
        left: "NORMAL"
        right: "saved"
        Text { text: "●"; color: "green"; StatusBar.permanent: false }
        Text { text: "on"; StatusBar.permanent: false }
        Text { text: "END" }
    }
}`)))
	s.WaitForText(t, "END")
	row := s.Backend.Snapshot()[2]
	text := func(from, to int) string {
		out := ""
		for _, c := range row[from:to] {
			out += c.Content
		}
		return out
	}
	if got := text(0, 12); got != "● on NORMAL " {
		t.Fatalf("the row starts %q, want the normal widgets then the left segment:\n%s", got, s)
	}
	if got := text(w-9, w); got != "saved END" {
		t.Fatalf("the row ends %q, want the right segment then the permanent widget:\n%s", got, s)
	}
	if dot := row[0]; dot.Attrs.FG != ansi(green) || dot.Attrs.BG != ansi(red) {
		t.Errorf("the dot wears %+v on %+v, want green on the bar's red", dot.Attrs.FG, dot.Attrs.BG)
	}
}

// TestStatusBarPermanentMustBeABool: StatusBar.permanent takes true or false.
func TestStatusBarPermanentMustBeABool(t *testing.T) {
	_, err := mountDoc(t, "import tui 1.0\nWindow { StatusBar { Text { StatusBar.permanent: 3 } } }")
	if err == nil || !strings.Contains(err.Error(), "StatusBar.permanent") {
		t.Fatalf("err = %v, want StatusBar.permanent refused", err)
	}
}
