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
	if ed.Core().Value() != "" {
		t.Fatalf("the held chord rune is already text: %q", ed.Core().Value())
	}
	ed.HandleEvent(tui.TickEvent{})
	if ed.Core().Value() != "j" {
		t.Fatalf("after the tick: %q, want the held j", ed.Core().Value())
	}
	ed.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})

	if !ed.Core().CanUndo() || ed.Core().CanUndo() != c.CanUndo() {
		t.Fatal("CanUndo does not say what the core says")
	}
	ed.Core().Undo()
	if ed.Core().Value() != "" || !ed.Core().CanRedo() || ed.Core().CanRedo() != c.CanRedo() {
		t.Fatalf("Undo left %q, CanRedo %v", ed.Core().Value(), ed.Core().CanRedo())
	}
	ed.Core().Redo()
	if ed.Core().Value() != "j" {
		t.Fatalf("Redo left %q", ed.Core().Value())
	}

	ed.Core().SetReadOnly(true)
	if !ed.Core().ReadOnly() || !c.ReadOnly() {
		t.Fatal("ReadOnly does not follow SetReadOnly")
	}
	ed.Core().SetReadOnly(false)

	if !ed.Core().SetNamedRegister('a', "alpha", true) {
		t.Fatal("register a refused")
	}
	if text, linewise, ok := ed.Core().NamedRegister('a'); !ok || text != "alpha" || !linewise {
		t.Fatalf("register a: %q %v %v", text, linewise, ok)
	}

	if got, want := len(ed.Core().BindingsForMode(widget.ModeInsert)), len(c.BindingsForMode(widget.ModeInsert)); got == 0 || got != want {
		t.Fatalf("BindingsForMode(Insert): %d bindings, the core has %d", got, want)
	}
	if len(ed.Core().Bindings()) != len(c.Bindings()) {
		t.Fatal("Bindings differs from the core's")
	}

	ed.Core().SetJumpDoc("notes.md")
	ed.Core().SetJumpView(func() string { return "raw" })
	ed.Core().SetJumps(widget.NewJumpList(100), nil)
	if p := ed.Core().JumpHere(); p.Doc != "notes.md" || p.View != "raw" || p != c.JumpHere() {
		t.Fatalf("JumpHere = %+v", p)
	}

	ed.SetCursorColor(style.ANSI(3))
	if col, ok := ed.CursorColor(); !ok || col != style.ANSI(3) {
		t.Fatalf("CursorColor = %v %v", col, ok)
	}
}
