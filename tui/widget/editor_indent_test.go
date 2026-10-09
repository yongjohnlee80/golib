package widget_test

import (
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/golang"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
	"testing"
)

func goCore(text string) *widget.EditorCore {
	d := golang.Definition()
	c := widget.NewEditorCore(widget.CoreKeyset(widget.KeysetStandard), widget.CoreAutoIndent(true), widget.CoreSourceFactory(func() highlight.Source { return d.NewSource(nil) }))
	c.SetValue(text)
	return c
}
func TestSourceNewlinePairAndUndoKeepTheTypingGroup(t *testing.T) {
	c := goCore("func f() {}")
	c.SetLine(0, 10)
	c.HandleKey(tui.KeyEvent{Code: tui.KeyEnter, Kind: tui.KeyPress})
	if got := c.Value(); got != "func f() {\n\t\n}" {
		t.Fatal(got)
	}
	c.HandleKey(tui.KeyEvent{Code: 'z', Mods: tui.ModCtrl, Kind: tui.KeyPress})
	if got := c.Value(); got != "func f() {}" {
		t.Fatal("undo lost the pair/group", got)
	}
}
func TestSourcePasteAndReadOnlyRemainLiteral(t *testing.T) {
	c := goCore("func f() {")
	c.SetLine(0, len(c.Value()))
	c.HandlePaste("\nx\n}")
	if c.Value() != "func f() {\nx\n}" {
		t.Fatal(c.Value())
	}
	c.SetReadOnly(true)
	before := c.Value()
	c.HandleKey(tui.KeyEvent{Code: tui.KeyEnter, Kind: tui.KeyPress})
	c.HandleKey(tui.KeyEvent{Code: '}', Text: "}", Kind: tui.KeyPress})
	if c.Value() != before {
		t.Fatal("read-only changed", c.Value())
	}
}
func TestSourceClosingUsesItsVerifiedBlockBase(t *testing.T) {
	c := goCore("func f() {\n\t")
	f := c.BeginHighlight(0)
	f.Styles(1)
	f.Close()
	c.SetLine(1, 1)
	c.HandleKey(tui.KeyEvent{Code: '}', Text: "}", Kind: tui.KeyPress})
	if c.Value() != "func f() {\n}" {
		t.Fatal(c.Value())
	}
}

func TestEnterAndUndoAfterBraceBranchTransition(t *testing.T) {
	text := "if ready {\n} else {"
	c := goCore(text)
	f := c.BeginHighlight(0)
	f.Styles(1)
	f.Close()
	c.SetLine(1, len("} else {"))
	c.HandleKey(tui.KeyEvent{Code: tui.KeyEnter, Kind: tui.KeyPress})
	if c.Value() != text+"\n\t" {
		t.Fatal("new branch body was dedented", c.Value())
	}
	c.HandleKey(tui.KeyEvent{Code: 'z', Mods: tui.ModCtrl, Kind: tui.KeyPress})
	if c.Value() != text {
		t.Fatal("branch newline undo changed source", c.Value())
	}
}
