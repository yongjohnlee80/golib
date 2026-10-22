package widget_test

import (
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// The deprecated facade still reaches what its replacement does, until the minor removes it: the
// With* options configure the core as WithCore does, the pass-throughs read and set the core, and
// the command-line ones are the CommandLine's.
func TestTheDeprecatedEditorFacadeForwards(t *testing.T) {
	ed := widget.NewEditor(widget.WithKeyset(widget.KeysetNano), widget.WithEscapeChord("kj"),
		widget.WithInitialText("one\ntwo"), widget.WithEditorReadOnly(true), widget.WithCommandLine(false))
	c := ed.Core()
	if c.Keyset() != widget.KeysetNano || c.EscapeChord() != "kj" || !c.ReadOnly() || ed.CommandLine().Enabled() {
		t.Fatalf("the options: keyset %v, chord %q, read-only %v, prompt %v; want Nano, kj, true, off",
			c.Keyset(), c.EscapeChord(), c.ReadOnly(), ed.CommandLine().Enabled())
	}
	for _, tc := range []struct {
		opt  widget.EditorOption
		want widget.Keyset
	}{{widget.WithVimKeymap(), widget.KeysetVim}, {widget.WithNanoKeymap(), widget.KeysetNano}, {widget.WithStandardKeymap(), widget.KeysetStandard}} {
		if got := widget.NewEditor(tc.opt).Core().Keyset(); got != tc.want {
			t.Errorf("keyset option: %v, want %v", got, tc.want)
		}
	}
	if ed.Value() != "one\ntwo" || len(ed.Lines()) != 2 || !ed.ReadOnly() {
		t.Errorf("Value %q, %d lines, read-only %v; want the core's", ed.Value(), len(ed.Lines()), ed.ReadOnly())
	}
	ed.SetLine(1, 2)
	if row, col := c.Line(); row != 1 {
		t.Errorf("SetLine(1, 2) left the core at (%d, %d)", row, col)
	}
	// the reads agree with the core's
	if ed.Mode() != c.Mode() || ed.Keyset() != c.Keyset() || ed.EscapeChord() != c.EscapeChord() ||
		ed.CanUndo() != c.CanUndo() || ed.CanRedo() != c.CanRedo() || ed.SelectedText() != c.SelectedText() ||
		len(ed.Bindings()) != len(c.Bindings()) || len(ed.BindingsForMode(widget.ModeNormal)) != len(c.BindingsForMode(widget.ModeNormal)) ||
		!reflect.DeepEqual(ed.SnapshotKeymap(), c.SnapshotKeymap()) || !reflect.DeepEqual(ed.Keymap(), c.Keymap()) ||
		!reflect.DeepEqual(ed.ChordsForAction(widget.ActUndo), c.ChordsForAction(widget.ActUndo)) ||
		ed.JumpHere() != c.JumpHere() || ed.Commands() != c.Commands() {
		t.Error("a deprecated read disagrees with the core's")
	}
	if a1, ok1 := ed.ActionForChord(widget.KeyChord{Mode: widget.ModeNormal, Code: 'u'}); true {
		if a2, ok2 := c.ActionForChord(widget.KeyChord{Mode: widget.ModeNormal, Code: 'u'}); a1 != a2 || ok1 != ok2 {
			t.Error("ActionForChord disagrees with the core's")
		}
	}
	r1, c1, r2, c2, ok := ed.SelectionRange()
	if q1, d1, q2, d2, qok := c.SelectionRange(); r1 != q1 || c1 != d1 || r2 != q2 || c2 != d2 || ok != qok {
		t.Error("SelectionRange disagrees with the core's")
	}
	// the writes reach the core
	ed.SetReadOnly(false)
	ed.SetKeyset(widget.KeysetStandard)
	ed.SetCursorPosition(4)
	ed.SetRegister("reg", true)
	if text, lw := ed.Register(); text != "reg" || !lw {
		t.Errorf("Register = %q, %v; want the one set", text, lw)
	}
	ed.SetNamedRegister('a', "named", false) // as a yank into "a does, it sets the unnamed register too
	ed.SetJumpDoc("doc")
	ed.SetJumpView(func() string { return "view" })
	ed.OverlayKeymap(widget.Keymap{})
	ed.SetHighlighter(nil)
	_ = ed.WithSyntaxStyles(widget.SyntaxStyles{})
	ed.Undo()
	ed.Redo()
	if c.ReadOnly() || c.Keyset() != widget.KeysetStandard {
		t.Error("SetReadOnly or SetKeyset did not reach the core")
	}
	if row, _ := c.Line(); row != 1 {
		t.Errorf("SetCursorPosition(4) left the core on row %d, want 1", row)
	}
	if text, _, ok := ed.NamedRegister('a'); !ok || text != "named" {
		t.Errorf("NamedRegister('a') = %q, %v; want the one set", text, ok)
	}
	if err := ed.RunCommand("nosuchcommand"); err == nil {
		t.Error("RunCommand of an unknown command reported no error")
	}
	ed.SetCommandLineEnabled(true)
	if !ed.CommandLineEnabled() || !ed.CommandLine().Enabled() || ed.CommandLineOpen() || ed.ContextMenuOpen() {
		t.Error("the command line's and menu's deprecated names do not read the CommandLine and the menu")
	}
}
