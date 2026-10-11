package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// cmdFixture mounts an editor under an OverlayHost (where the prompt opens) inside a shell
// (which records what bubbles past it), with the keyboard in the editor.
func cmdFixture(t *testing.T, text string, opts ...widget.EditorOption) (*harness, *widget.Editor, *shell) {
	t.Helper()
	ed := widget.NewEditor(append([]widget.EditorOption{widget.WithInitialText(text)}, opts...)...)
	sh := newShell(widget.NewOverlayHost(ed))
	h := startApp(t, sh, 80, 20)
	h.inject(click(0, 0))
	h.settle()
	return h, ed, sh
}

func typeIn(h *harness, s string) {
	for _, r := range s {
		h.inject(tui.KeyEvent{Code: r, Text: string(r)})
	}
	h.settle()
}

func enter(h *harness) { h.inject(tui.KeyEvent{Code: tui.KeyEnter}); h.settle() }

func promptOpen(h *harness, ed *widget.Editor) bool {
	var open bool
	h.onLoop(func() { open = ed.CommandLineOpen() })
	return open
}

func cursorOf(h *harness, ed *widget.Editor) widget.TextPos {
	var p widget.TextPos
	h.onLoop(func() { p = ed.Core().Cursor() })
	return p
}

func TestColonOpensVimCommandsAndALineNumberGoesThere(t *testing.T) {
	h, ed, _ := cmdFixture(t, "one\ntwo\nthree\nfour")
	defer h.stop()
	typeIn(h, ":")
	h.waitFor("the prompt", func() bool { return promptOpen(h, ed) })
	h.wantContains("VIM Commands")
	h.wantContains("Tab completes")
	typeIn(h, "3")
	enter(h)
	h.waitFor("the prompt to close", func() bool { return !promptOpen(h, ed) })
	if got := cursorOf(h, ed); got != (widget.TextPos{Line: 2}) {
		t.Fatalf("cursor after :3 = %v", got)
	}
	// the keyboard is back in the editor: j moves
	typeIn(h, "j")
	if got := cursorOf(h, ed); got.Line != 3 {
		t.Fatalf("after the prompt, j left the cursor at %v: the keyboard did not return", got)
	}
}

func TestAFailedCommandReopensWithItsLineAndTheError(t *testing.T) {
	h, ed, _ := cmdFixture(t, "one\ntwo")
	defer h.stop()
	typeIn(h, ":9")
	enter(h)
	h.waitFor("the prompt to reopen", func() bool { return promptOpen(h, ed) })
	h.wantContains("the document has 2 lines")
	h.wantContains("9")
	// Esc closes it, and the cursor never moved
	h.inject(tui.KeyEvent{Code: tui.KeyEscape})
	h.settle()
	h.waitFor("the prompt to close", func() bool { return !promptOpen(h, ed) })
	if got := cursorOf(h, ed); got != (widget.TextPos{}) {
		t.Fatalf("cursor = %v", got)
	}
}

func TestTabCompletesAndListsTheCandidates(t *testing.T) {
	h, ed, _ := cmdFixture(t, "text")
	defer h.stop()
	h.onLoop(func() {
		_ = ed.Commands().Add(widget.Command{Name: "table", Args: "<cols> <rows>", Help: "a Markdown table",
			Run: func(widget.CommandContext) error { return nil }})
	})
	typeIn(h, ":")
	h.waitFor("the prompt", func() bool { return promptOpen(h, ed) })
	typeIn(h, "t")
	h.inject(tui.KeyEvent{Code: tui.KeyTab})
	h.settle()
	// one candidate: completed with a space, listed with its arguments
	h.wantContains("table <cols> <rows> — a Markdown table")
	if !strings.Contains(h.grid(), "│ table ") {
		t.Fatalf("t + Tab did not complete table:\n%s", h.grid())
	}
	h.inject(tui.KeyEvent{Code: tui.KeyEscape})
	h.settle()

	typeIn(h, ":")
	h.waitFor("the prompt", func() bool { return promptOpen(h, ed) })
	typeIn(h, "re")
	h.inject(tui.KeyEvent{Code: tui.KeyTab})
	h.settle()
	h.wantContains("replace <term> <with>")
	h.inject(tui.KeyEvent{Code: tui.KeyEscape})
	h.settle()

	// several candidates: all listed, Tab cycles them
	typeIn(h, ":")
	h.waitFor("the prompt", func() bool { return promptOpen(h, ed) })
	typeIn(h, "w")
	h.inject(tui.KeyEvent{Code: tui.KeyTab})
	h.settle()
	h.wantContains("w — save")
	h.wantContains("wq — save, then quit")
	if !strings.Contains(h.grid(), "│ w ") {
		t.Fatalf("the first Tab over w, wq did not keep w:\n%s", h.grid())
	}
	h.inject(tui.KeyEvent{Code: tui.KeyTab})
	h.settle()
	if !strings.Contains(h.grid(), "│ wq ") {
		t.Fatalf("a second Tab over w, wq did not reach wq:\n%s", h.grid())
	}
	h.inject(tui.KeyEvent{Code: tui.KeyTab, Mods: tui.ModShift})
	h.settle()
	if !strings.Contains(h.grid(), "│ w ") {
		t.Fatalf("Shift+Tab did not go back to w:\n%s", h.grid())
	}
	if !promptOpen(h, ed) {
		t.Fatal("Tab moved the keyboard out of the prompt")
	}
}

func TestWriteAndQuitFromThePromptReachTheHost(t *testing.T) {
	var got []string
	h, _, _ := cmdFixture(t, "text",
		widget.WithOnWrite(func(quit bool) error { got = append(got, map[bool]string{false: "w", true: "wq"}[quit]); return nil }),
		widget.WithOnQuit(func(force bool) error { got = append(got, map[bool]string{false: "q", true: "q!"}[force]); return nil }))
	defer h.stop()
	for _, line := range []string{"w", "wq", "q!"} {
		typeIn(h, ":"+line)
		enter(h)
	}
	var calls string
	h.onLoop(func() { calls = strings.Join(got, " ") })
	if calls != "w wq q!" {
		t.Fatalf("hooks = %q", calls)
	}
}

func TestReplaceAsksAtEachMatch(t *testing.T) {
	h, ed, _ := cmdFixture(t, "cat cat\ncat")
	defer h.stop()
	typeIn(h, ":replace cat dog")
	enter(h)
	h.waitFor("the question", func() bool { return strings.Contains(h.grid(), "Replace this match?") })
	typeIn(h, "y")
	typeIn(h, "n")
	typeIn(h, "y")
	h.waitFor("the question to go", func() bool { return !strings.Contains(h.grid(), "Replace this match?") })
	var text string
	h.onLoop(func() { text = ed.Value() })
	if text != "dog cat\ndog" {
		t.Fatalf("text = %q", text)
	}
	// q stops at once, keeping what was replaced
	typeIn(h, ":replace cat bird")
	enter(h)
	h.waitFor("the question", func() bool { return strings.Contains(h.grid(), "Replace this match?") })
	typeIn(h, "q")
	h.waitFor("the question to go", func() bool { return !strings.Contains(h.grid(), "Replace this match?") })
	h.onLoop(func() { text = ed.Value() })
	if text != "dog cat\ndog" {
		t.Fatalf("after q: %q", text)
	}
}

func TestWithCommandLineOffLetsColonBubble(t *testing.T) {
	h, ed, sh := cmdFixture(t, "text", widget.WithCommandLine(false))
	defer h.stop()
	typeIn(h, ":")
	if promptOpen(h, ed) {
		t.Fatal("the prompt opened while off")
	}
	bubbled := false
	for _, k := range sh.bubbledKeys() {
		bubbled = bubbled || k.Code == ':'
	}
	if !bubbled {
		t.Fatal("':' did not reach the application")
	}
}
