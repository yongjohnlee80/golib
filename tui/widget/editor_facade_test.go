package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// The Editor's own methods reach its core, and say what the core says: undo and redo, read-only,
// named registers, the bindings of a mode, the jump list's place, and the escape chord's timeout
// arriving as a tick. Its cursor colour is kept as given.
func TestTheEditorsMethodsReachItsCore(t *testing.T) {
	ed := widget.NewEditor()
	c := ed.Core()
	typeKeys := func(s string) {
		for _, r := range s {
			ed.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: r, Text: string(r)})
		}
	}

	// The escape chord's first rune is held; its timeout, a tick, commits it as text.
	typeKeys("ij")
	if ed.Value() != "" {
		t.Fatalf("the held chord rune is already text: %q", ed.Value())
	}
	ed.HandleEvent(tui.TickEvent{})
	if ed.Value() != "j" {
		t.Fatalf("after the tick: %q, want the held j", ed.Value())
	}
	ed.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})

	if !ed.CanUndo() || ed.CanUndo() != c.CanUndo() {
		t.Fatal("CanUndo does not say what the core says")
	}
	ed.Undo()
	if ed.Value() != "" || !ed.CanRedo() || ed.CanRedo() != c.CanRedo() {
		t.Fatalf("Undo left %q, CanRedo %v", ed.Value(), ed.CanRedo())
	}
	ed.Redo()
	if ed.Value() != "j" {
		t.Fatalf("Redo left %q", ed.Value())
	}

	ed.SetReadOnly(true)
	if !ed.ReadOnly() || !c.ReadOnly() {
		t.Fatal("ReadOnly does not follow SetReadOnly")
	}
	ed.SetReadOnly(false)

	if !ed.SetNamedRegister('a', "alpha", true) {
		t.Fatal("register a refused")
	}
	if text, linewise, ok := ed.NamedRegister('a'); !ok || text != "alpha" || !linewise {
		t.Fatalf("register a: %q %v %v", text, linewise, ok)
	}

	if got, want := len(ed.BindingsForMode(widget.ModeInsert)), len(c.BindingsForMode(widget.ModeInsert)); got == 0 || got != want {
		t.Fatalf("BindingsForMode(Insert): %d bindings, the core has %d", got, want)
	}
	if len(ed.Bindings()) != len(c.Bindings()) {
		t.Fatal("Bindings differs from the core's")
	}

	ed.SetJumpDoc("notes.md")
	ed.SetJumpView(func() string { return "raw" })
	ed.SetJumps(widget.NewJumpList(100), nil)
	if p := ed.JumpHere(); p.Doc != "notes.md" || p.View != "raw" || p != c.JumpHere() {
		t.Fatalf("JumpHere = %+v", p)
	}

	ed.SetCursorColor(style.ANSI(3))
	if col, ok := ed.CursorColor(); !ok || col != style.ANSI(3) {
		t.Fatalf("CursorColor = %v %v", col, ok)
	}
}
