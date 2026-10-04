//go:build linux || darwin

package decl_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// terminal_test.go: a Terminal built from QML runs its program and raises its signals, and the
// Drawer holding it moves between all five positions with the program kept.

func TestATerminalFromQMLRunsAndSignals(t *testing.T) {
	script := filepath.Join(t.TempDir(), "prog.sh")
	prog := "#!/bin/sh\nprintf '\\033]2;decl-title\\007hello from sh\\n'\nread x\nexit 3\n"
	if err := os.WriteFile(script, []byte(prog), 0o755); err != nil {
		t.Fatal(err)
	}
	exited, titled, moded := &recorder{}, &recorder{}, &recorder{}
	s := decltest.Run(t, 60, 12,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" Text { text: \"the page\" }\n"+
			" Drawer { id: d; modal: false; edge: App.edge; size: 50; length: 60\n"+
			"  Frame { title: \"term\"; Terminal { id: term; command: \""+script+"\"; vimKeys: true\n"+
			"   onExited: App.exited(code); onTitleChanged: App.titled(title); onModeChanged: App.moded(mode) } } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.edge": "bottom"}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{
			"App.exited": exited.handler, "App.titled": titled.handler, "App.moded": moded.handler,
		}))
	s.WaitForText(t, "the page")
	onScreenLoop(t, s, func() {
		for _, m := range []string{"open", "start"} {
			target := "d"
			if m == "start" {
				target = "term"
			}
			if err := s.Program.Call(target, m); err != nil {
				t.Error(err)
			}
		}
		if err := s.Program.Call("term", "focus"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "hello from sh")
	s.WaitFor(t, "titleChanged", func(string) bool { return len(titled.all()) == 1 })
	if got := titled.all()[0].Raw; got != "decl-title" {
		t.Errorf("titleChanged(%q)", got)
	}

	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	s.WaitFor(t, "modeChanged(normal)", func(string) bool { return len(moded.all()) == 1 })
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: 'i', Text: "i"})
	s.WaitFor(t, "modeChanged(terminal)", func(string) bool { return len(moded.all()) == 2 })
	if m := moded.all(); m[0].Raw != "normal" || m[1].Raw != "terminal" {
		t.Errorf("modeChanged %q, %q", m[0].Raw, m[1].Raw)
	}

	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitFor(t, "exited", func(string) bool { return len(exited.all()) == 1 })
	if got := exited.all()[0].Raw; got != "3" {
		t.Errorf("exited(%s), want 3", got)
	}
	s.WaitForText(t, "[process exited 3]")

	// Every position, Tui.Center included, with the program's screen kept. Size 50 across the
	// edge and length 60 along it: on 60 x 12, a side panel is 30 wide, a top or bottom one 6 high,
	// a side one 7 rows high, centred from row 2, and the centred one 36 x 6 in the middle.
	want := map[string][2]int{ // the frame's top-left: column, row
		"top": {12, 0}, "left": {0, 2}, "right": {30, 2}, "center": {12, 3}, "bottom": {12, 6},
	}
	for _, edge := range []string{"top", "left", "right", "center", "bottom"} {
		onScreenLoop(t, s, func() {
			if err := s.Program.Set("App.edge", edge); err != nil {
				t.Error(err)
			}
		})
		s.WaitFor(t, "the terminal at "+edge, func(sc string) bool {
			if !strings.Contains(sc, "hello from sh") || !strings.Contains(sc, "process exited 3") {
				return false
			}
			for y, l := range strings.Split(sc, "\n") {
				if c := col(l, "┌ term"); c >= 0 {
					return [2]int{c, y} == want[edge]
				}
			}
			return false
		})
	}
}

func TestADrawersEdgeTakesCenter(t *testing.T) {
	if _, err := mountDoc(t, "import tui 1.0\nWindow { Drawer { edge: Tui.Center; Text { } } }"); err != nil {
		t.Fatalf("Drawer { edge: Tui.Center } refused: %v", err)
	}
}

// A Terminal's construction properties and its runtime setters and stop() reach the widget; a
// property of the wrong kind is refused.
func TestATerminalsPropertiesAndStopReachTheWidget(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "pwd.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\npwd\nread x\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	exited := &recorder{}
	s := decltest.Run(t, 200, 8,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			"Terminal { id: term; command: App.cmd; dir: \""+dir+"\"; scrollback: 10; onExited: App.exited(code) }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.cmd": "/nonexistent/first"}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.exited": exited.handler}))
	// The command and the folder change before the start, through the setters.
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.cmd", script); err != nil {
			t.Error(err)
		}
		if err := s.Program.Call("term", "start"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, dir[max(0, len(dir)-20):])
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("term", "stop"); err != nil {
			t.Error(err)
		}
	})
	if _, err := mountDoc(t, "import tui 1.0\nTerminal { scrollback: \"many\" }"); err == nil {
		t.Error("Terminal { scrollback: \"many\" } was not refused")
	}
}
