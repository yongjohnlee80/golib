package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// The command line's w, wq and q! raise the document's writeRequested(quit) and
// quitRequested(force); openCommandLine opens the prompt from a key of the document's own.
func TestTheCommandLineRaisesTheEditorsSignals(t *testing.T) {
	rec := &recorder{}
	s := decltest.Run(t, 70, 14,
		tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
import demo 1.0
Window {
    Shortcut { sequence: "Ctrl+P"; onActivated: ed.openCommandLine("w") }
    Editor {
        id: ed
        text: "hello"
        focus: true
        onWriteRequested: App.log("write", quit)
        onQuitRequested: App.log("quit", force)
    }
}`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "hello")
	for _, line := range []string{"wq", "q!"} {
		s.Keys(t, decltest.Rune(':'))
		s.WaitForText(t, "VIM Commands")
		s.Keys(t, decltest.Type(line)...)
		s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	}
	s.Keys(t, decltest.Ctrl('p'))
	s.WaitForText(t, "VIM Commands")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}) // the field holds "w"
	s.WaitFor(t, "three signals", func(string) bool { return len(rec.all()) == 6 })
	var raw []string
	for _, v := range rec.all() {
		raw = append(raw, v.Raw)
	}
	if got := strings.Join(raw, " "); got != "write true quit true write false" {
		t.Fatalf("signals = %q", got)
	}
}

// With no handler the editor cannot save: w says so in the prompt rather than doing nothing.
func TestWithoutAHandlerWSaysTheEditorCannotSave(t *testing.T) {
	s := decltest.Run(t, 70, 14, tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window { Editor { text: "hello"; focus: true } }`)))
	s.WaitForText(t, "hello")
	s.Keys(t, decltest.Rune(':'))
	s.WaitForText(t, "VIM Commands")
	s.Keys(t, decltest.Rune('w'), tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitForText(t, "this editor cannot save")
}

// commandLine: false turns the prompt off; ':' is then the application's.
func TestCommandLineFalseTurnsThePromptOff(t *testing.T) {
	rec := &recorder{}
	s := decltest.Run(t, 70, 14,
		tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
import demo 1.0
Window {
    Shortcut { sequence: ":"; onActivated: App.log("colon") }
    Editor { text: "hello"; commandLine: false; focus: true }
}`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "hello")
	s.Keys(t, decltest.Rune(':'))
	s.WaitFor(t, "the window's ':'", func(string) bool { return len(rec.all()) == 1 })
	if rec.all()[0].Raw != "colon" {
		t.Fatalf("logged %v", rec.all())
	}
	if strings.Contains(s.String(), "VIM Commands") {
		t.Fatal("the prompt opened while off")
	}
}
