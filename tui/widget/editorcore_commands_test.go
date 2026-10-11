package widget_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestRunCommandGoesToALineNumber(t *testing.T) {
	c, _ := coreWith(t, "one\n  two\nthree")
	if err := c.RunCommand("2"); err != nil || c.Cursor() != pos(1, 2) {
		t.Fatalf(":2 = %v, cursor %v; want line 2's first non-blank", err, c.Cursor())
	}
	if err := c.RunCommand(":3"); err != nil || c.Cursor() != pos(2, 0) {
		t.Fatalf("::3 = %v, cursor %v", err, c.Cursor())
	}
	if err := c.RunCommand("0"); err != nil || c.Cursor() != pos(0, 0) {
		t.Fatalf(":0 = %v, cursor %v; want the first line", err, c.Cursor())
	}
	err := c.RunCommand("4")
	if err == nil || !strings.Contains(err.Error(), "the document has 3 lines") || c.Cursor() != pos(0, 0) {
		t.Fatalf(":4 past the end = %v, cursor %v; want a refusal naming the line count", err, c.Cursor())
	}
	if err := c.RunCommand("   "); err != nil {
		t.Fatalf("an empty line = %v", err)
	}
	if err := c.RunCommand("frob x"); !errors.Is(err, widget.ErrUnknownCommand) {
		t.Fatalf("an unknown verb = %v", err)
	}
}

func TestWriteAndQuitReachTheHostsHooks(t *testing.T) {
	var calls []string
	c, _ := coreWith(t, "text",
		widget.CoreOnWrite(func(quit bool) error {
			calls = append(calls, map[bool]string{false: "write", true: "write+quit"}[quit])
			return nil
		}),
		widget.CoreOnQuit(func(force bool) error {
			calls = append(calls, map[bool]string{false: "quit", true: "quit!"}[force])
			return nil
		}))
	for _, line := range []string{"w", "write", "q", "q!", "quit!", "wq", "x"} {
		if err := c.RunCommand(line); err != nil {
			t.Fatalf("%s = %v", line, err)
		}
	}
	_, _ = c.Insert(pos(0, 0), "!")
	_ = c.RunCommand("x") // modified now: x writes, then quits
	want := "write write quit quit! quit! write+quit quit write+quit"
	if got := strings.Join(calls, " "); got != want {
		t.Fatalf("hooks called %q, want %q", got, want)
	}

	bare, _ := coreWith(t, "")
	if err := bare.RunCommand("w"); !errors.Is(err, widget.ErrNoWriter) {
		t.Fatalf("w with no writer = %v", err)
	}
	if err := bare.RunCommand("q"); !errors.Is(err, widget.ErrNoQuit) {
		t.Fatalf("q with no quit = %v", err)
	}
}

func TestCommandsComposeAndComplete(t *testing.T) {
	c, _ := coreWith(t, "a")
	var got cmdArgs
	cmd := widget.Command{Name: "table", Args: "<cols> <rows>", Help: "a table", Run: func(x widget.CommandContext) error {
		got = cmdArgs{x.Name, x.Bang, x.Args, x.Line}
		return nil
	}}
	if err := c.Commands().Add(cmd); err != nil {
		t.Fatal(err)
	}
	if err := c.RunCommand("table!  5   3 "); err != nil {
		t.Fatal(err)
	}
	if got.Name != "table" || !got.Bang || strings.Join(got.Args, ",") != "5,3" || got.Line != "5   3" {
		t.Fatalf("context = %+v", got)
	}
	if err := c.Commands().Add(widget.Command{Name: "w", Run: cmd.Run}); !errors.Is(err, widget.ErrCommandExists) {
		t.Fatalf("Add over w = %v", err)
	}
	if err := c.Commands().Add(widget.Command{Name: "12", Run: cmd.Run}); err == nil {
		t.Fatal("a verb of digits was accepted: it would read as a line number")
	}
	// Set replaces, alias and all
	ran := false
	if err := c.Commands().Set(widget.Command{Name: "write", Run: func(widget.CommandContext) error { ran = true; return nil }}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Commands().Lookup("w"); ok {
		t.Fatal("Set kept the old command's other names")
	}
	_ = c.RunCommand("write")
	if !ran {
		t.Fatal("Set's command did not run")
	}
	var names []string
	for _, m := range c.Commands().Complete("n") {
		names = append(names, m.Name)
	}
	if strings.Join(names, ",") != "nohl" {
		t.Fatalf("Complete(n) = %v", names)
	}
	names = names[:0]
	for _, m := range c.Commands().Complete("") {
		names = append(names, m.Name)
	}
	if strings.Join(names, ",") != "q,wq,x,find,replace,nohl,table,write" {
		t.Fatalf("Complete() = %v; want every command in order", names)
	}
}

type cmdArgs struct {
	Name string
	Bang bool
	Args []string
	Line string
}

func TestFindAndReplaceCommands(t *testing.T) {
	c, _ := coreWith(t, "Foo bar\nfoo foo")
	if err := c.RunCommand("find foo"); err != nil || c.Cursor() != pos(1, 0) {
		t.Fatalf("find foo = %v, cursor %v", err, c.Cursor())
	}
	if term, o := c.FindTerm(); term != "foo" || !o.IgnoreCase {
		t.Fatalf("find's term %q, %+v: a lower-case term folds case", term, o)
	}
	if err := c.RunCommand("find Foo"); err != nil || c.Cursor() != pos(0, 0) {
		t.Fatalf("find Foo = %v, cursor %v; an upper-case letter makes it exact", err, c.Cursor())
	}
	if err := c.RunCommand("find zzz"); err == nil {
		t.Fatal("find of nothing reported no error")
	}
	// no widget to ask: every match
	if err := c.RunCommand("replace foo a b"); err != nil || c.Value() != "a b bar\na b a b" {
		t.Fatalf("replace = %v; text %q", err, c.Value())
	}
	if err := c.RunCommand("replace foo"); err == nil {
		t.Fatal("replace without a replacement was accepted")
	}
}

func TestAReplaceSessionAsksEachMatchOnceAndUndoesAsOne(t *testing.T) {
	c, _ := coreWith(t, "cat cat\ncat")
	var asked int
	c.SetReplaceAsk(func(s *widget.ReplaceSession) error {
		// the widget's questions: yes, no, yes
		for _, yes := range []bool{true, false, true} {
			if _, ok := s.Current(); !ok {
				t.Fatal("the session ended early")
			}
			if sel, ok := c.Selection(); !ok || sel.To.Col-sel.From.Col != 3 {
				t.Fatalf("the match is not selected: %v %v", sel, ok)
			}
			asked++
			if _, err := s.Answer(yes); err != nil {
				t.Fatal(err)
			}
		}
		if _, ok := s.Current(); ok || s.Replaced() != 2 {
			t.Fatalf("after three answers: open %v, replaced %d", ok, s.Replaced())
		}
		return nil
	})
	if err := c.RunCommand("replace cat concat"); err != nil {
		t.Fatal(err)
	}
	if asked != 3 || c.Value() != "concat cat\nconcat" {
		t.Fatalf("asked %d; text %q (a replacement containing the term is never asked again)", asked, c.Value())
	}
	undo(c)
	if c.Value() != "cat cat\ncat" {
		t.Fatalf("one undo after the session = %q", c.Value())
	}
	// stopping keeps what was replaced
	s, ok := c.StartReplace("cat", "dog", widget.FindOptions{})
	if !ok {
		t.Fatal("no session")
	}
	_, _ = s.Answer(true)
	s.Stop()
	if more, _ := s.Answer(true); more || c.Value() != "dog cat\ncat" || c.Mode() != widget.ModeNormal {
		t.Fatalf("after Stop: more %v, text %q, mode %v", more, c.Value(), c.Mode())
	}
}

func TestColonOpensTheWidgetsPromptOrBubbles(t *testing.T) {
	c, _ := coreWith(t, "a")
	colon := tui.KeyEvent{Code: ':', Text: ":"}
	if c.HandleKey(colon) {
		t.Fatal("':' with no prompt was consumed")
	}
	opened := 0
	c.SetCommandLine(func() bool { opened++; return true })
	if !c.HandleKey(colon) || opened != 1 {
		t.Fatalf("':' with a prompt: opened %d", opened)
	}
	keys(c, "v")
	if !c.HandleKey(colon) || opened != 2 {
		t.Fatalf("':' in Visual: opened %d", opened)
	}
	none, _ := coreWith(t, "a", widget.CoreCommands(nil))
	none.SetCommandLine(func() bool { opened++; return true })
	if none.HandleKey(colon) {
		t.Fatal("':' on a core with no commands was consumed")
	}
	ro, _ := coreWith(t, "a", widget.CoreReadOnly(true))
	ro.SetCommandLine(func() bool { return true })
	if !ro.HandleKey(colon) {
		t.Fatal("':' on a read-only core was refused: find and <n> still work there")
	}
}

func TestACommandsEditsAreOneUndo(t *testing.T) {
	c, _ := coreWith(t, "a")
	_ = c.Commands().Add(widget.Command{Name: "three", Run: func(x widget.CommandContext) error {
		for _, s := range []string{"1", "2", "3"} {
			if _, err := x.Core.Insert(x.Core.Cursor(), s); err != nil {
				return err
			}
		}
		return nil
	}})
	if err := c.RunCommand("three"); err != nil || c.Value() != "123a" {
		t.Fatalf("three = %v; text %q", err, c.Value())
	}
	undo(c)
	if c.Value() != "a" {
		t.Fatalf("one undo after a command = %q", c.Value())
	}
}
