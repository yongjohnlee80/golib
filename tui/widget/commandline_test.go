package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
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
	// the match is shown behind the card, which holds the keyboard: the editor is not focused
	snap := h.tb.Snapshot()
	for x := 0; x < 3; x++ {
		if snap[0][x].Attrs.Mask&tui.AttrReverse == 0 {
			t.Fatalf("the match's cluster %d is not highlighted behind the card:\n%s", x, h.grid())
		}
	}
	if snap[0][3].Attrs.Mask&tui.AttrReverse != 0 {
		t.Fatal("the highlight runs past the match")
	}
	// at the top right, clear of the notifications at the bottom
	if y := h.rowWith("Replace this match?"); y > 5 {
		t.Fatalf("the card is on row %d, want the top:\n%s", y, h.grid())
	}
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

// The candidates under the field wear the card's background, not the screen's default: a Text
// with no colours on the card was a dark block in a themed window.
func TestTheCandidatesWearTheCardsBackground(t *testing.T) {
	// a themed panel, so the card's background is not the default, and a selection unlike it
	th := style.NewTheme(style.ANSI(4),
		style.WithToken(style.TokenPanel, style.ANSI(4)),
		style.WithToken(style.TokenForeground, style.ANSI(7)),
		style.WithToken(style.TokenSurface, style.ANSI(6)),
		style.WithToken(style.TokenPrimary, style.ANSI(1)),
		style.WithToken(style.TokenTextOnPrimary, style.ANSI(7)))
	ed := widget.NewEditor(widget.WithInitialText("text"))
	h := startAppOpts(t, newShell(widget.NewOverlayHost(ed)), 80, 20, tui.WithTheme(&th))
	defer h.stop()
	h.inject(click(0, 0))
	h.settle()
	typeIn(h, ":w")
	h.inject(tui.KeyEvent{Code: tui.KeyTab})
	h.settle()
	snap := h.tb.Snapshot()
	bgAt := func(label string) tui.CellColor {
		y := h.rowWith(label)
		x := len([]rune(h.row(y)[:strings.Index(h.row(y), label)]))
		return snap[y][x].Attrs.BG
	}
	card := func(label string) tui.CellColor { // the card's padding left of the row
		y := h.rowWith(label)
		x := len([]rune(h.row(y)[:strings.Index(h.row(y), label)]))
		return snap[y][x-1].Attrs.BG
	}
	if got, want := bgAt("wq — save"), card("wq — save"); got != want {
		t.Fatalf("an unchosen candidate's background %+v, the card's %+v", got, want)
	}
	if bgAt("w — save") == card("w — save") {
		t.Fatal("the chosen candidate is not set apart from the card")
	}
}

// The dropdown: one candidate a row, the chosen one highlighted; Down and Up move as Tab and
// Shift+Tab do; with more than eight, the view follows the chosen one.
func TestTheDropdownFollowsTheChosenCandidate(t *testing.T) {
	h, ed, _ := cmdFixture(t, "text")
	defer h.stop()
	h.onLoop(func() {
		for _, n := range []string{"ca", "cb", "cc", "cd", "ce", "cf", "cg", "ch", "ci", "cj"} {
			_ = ed.Commands().Add(widget.Command{Name: n, Help: "help " + n, Run: func(widget.CommandContext) error { return nil }})
		}
	})
	typeIn(h, ":c")
	h.inject(tui.KeyEvent{Code: tui.KeyTab})
	h.settle()
	h.wantContains("ca — help ca")
	h.wantContains("ch — help ch")
	// framed: the box's top edge sits right over the first candidate
	if y := h.rowWith("ca — help ca"); !strings.Contains(h.row(y-1), "┌─") || !strings.Contains(h.row(y), "│ca — help ca") {
		t.Fatalf("the dropdown is not framed:\n%s", h.grid())
	}
	h.wantNotContains("ci — help ci") // eight in view
	for range 8 {
		h.inject(tui.KeyEvent{Code: tui.KeyDown})
	}
	h.settle()
	if !strings.Contains(h.grid(), "│ ci ") {
		t.Fatalf("eight Downs from ca did not choose ci:\n%s", h.grid())
	}
	h.wantContains("ci — help ci") // the view followed
	h.wantNotContains("ca — help ca")
	h.inject(tui.KeyEvent{Code: tui.KeyUp})
	h.settle()
	if !strings.Contains(h.grid(), "│ ch ") {
		t.Fatalf("Up did not go back to ch:\n%s", h.grid())
	}
	if !promptOpen(h, ed) {
		t.Fatal("the arrows moved the keyboard out of the prompt")
	}
}
