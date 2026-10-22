package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// mountedEditor is a focused Editor in an App holding m.
func mountedEditor(t *testing.T, m *keymap.Map, opts ...widget.EditorOption) (*harness, *widget.Editor, *shell) {
	t.Helper()
	e := widget.NewEditor(append([]widget.EditorOption{widget.WithCore(widget.CoreInitialText("one two three"))}, opts...)...)
	sh := newShell(e)
	h := startAppOpts(t, sh, 40, 6, tui.WithKeymap(m))
	h.inject(tab())
	h.barrier(sh)
	return h, e, sh
}

func editorValue(h *harness, e *widget.Editor) string {
	var v string
	h.onLoop(func() { v = e.Core().Value() })
	return v
}

func ch(r rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: r, Text: string(r)} }

// A scheme rebinding Vim Normal's undo to Super+Z: a kitty Super+Z undoes, and u no longer does.
// Standard's Ctrl+Z, another scope, is untouched.
func TestAnEditorSchemeBindsSuperInVimNormal(t *testing.T) {
	m := mustKeymap(t, `<mapping><shortcut id="tui.editor.vim.normal.undo"><key value="Super+Z"/></shortcut></mapping>`)
	h, e, sh := mountedEditor(t, m)
	h.inject(ch('x')) // delete "o"
	h.barrier(sh)
	if got := editorValue(h, e); got != "ne two three" {
		t.Fatalf("x: %q", got)
	}
	h.inject(ch('u'))
	h.barrier(sh)
	if got := editorValue(h, e); got != "ne two three" {
		t.Fatalf("u still undoes after the scheme moved undo: %q", got)
	}
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 'z', Mods: tui.ModSuper})
	h.barrier(sh)
	if got := editorValue(h, e); got != "one two three" {
		t.Fatalf("Super+Z did not undo: %q", got)
	}
	cmd, ok := m.Command("tui.editor.standard.insert.undo")
	if !ok || len(m.KeysOf(cmd)) == 0 || m.KeysOf(cmd)[0].String() != "Ctrl+Z" {
		t.Errorf("Standard's undo moved with Vim's: %v %v", m.KeysOf(cmd), ok)
	}
}

// Two Vim Normal commands on one key are refused; the same key in Vim Normal and Standard is not.
func TestEditorScopesRefuseOnlyTheirOwnConflicts(t *testing.T) {
	build := func(xml string) error {
		s, err := keymap.ParseScheme(strings.NewReader(xml), "keymap.xml")
		if err != nil {
			t.Fatal(err)
		}
		_, err = keymap.Build(widget.KeyCommands(), s)
		return err
	}
	if err := build(`<mapping><shortcut id="tui.editor.vim.normal.undo"><key value="X"/></shortcut></mapping>`); err == nil {
		t.Error("undo on x, which Vim Normal's deleteChar holds, was accepted")
	}
	if err := build(`<mapping><shortcut id="tui.editor.vim.normal.undo"><key value="Ctrl+Z"/></shortcut></mapping>`); err != nil {
		t.Errorf("Ctrl+Z in Vim Normal and in Standard, two scopes: %v", err)
	}
}

// A host's Go overlay is replayed over the scheme, so it wins: its ActUnbound frees a key the scheme
// bound, and its rebinding takes a key the scheme moved away.
func TestAHostOverlayWinsOverAScheme(t *testing.T) {
	m := mustKeymap(t, `<mapping><shortcut id="tui.editor.vim.normal.deleteChar"><key value="Ctrl+Q"/></shortcut></mapping>`)
	h, e, sh := mountedEditor(t, m, widget.WithCore(widget.CoreKeymap(widget.Keymap{
		{Mode: widget.ModeNormal, Code: 'q', Ctrl: true}: widget.ActUnbound, // the host owns Ctrl+Q
		{Mode: widget.ModeNormal, Code: 'x'}:             widget.ActLineEnd, // and gives x another meaning
	})))
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 'q', Mods: tui.ModCtrl})
	h.barrier(sh)
	if got := editorValue(h, e); got != "one two three" {
		t.Fatalf("Ctrl+Q deleted though the host unbound it: %q", got)
	}
	h.inject(ch('x'), ch('i'), ch('!'))
	h.barrier(sh)
	if got := editorValue(h, e); got != "one two thre!e" {
		t.Fatalf("x is the host's LineEnd: %q", got)
	}
}

// A map installed while d is pending clears it, as Esc does: the next key is processed afresh. The
// keys and the install run in one turn on the loop, straight into the Editor: the harness's barrier key
// reaches the focused editor and would itself cancel d, hiding whether the rebuild did.
func TestAReloadedMapClearsPendingGrammar(t *testing.T) {
	h, e, sh := mountedEditor(t, mustKeymap(t, `<mapping/>`))
	other := mustKeymap(t, `<mapping><shortcut id="tui.list.down"><key value="Ctrl+N"/></shortcut></mapping>`)
	for _, reload := range []bool{false, true} {
		var got string
		h.onLoop(func() {
			e.SetValue("one two three")
			e.HandleEvent(ch('d'))
			if reload {
				sh.ctx.App().ApplyKeymap(other)
			}
			e.HandleEvent(ch('w'))
			got = e.Core().Value()
		})
		switch {
		case !reload && got != "two three":
			t.Fatalf("without a reload, dw should delete a word: %q", got) // the cell can see d pending
		case reload && got != "one two three":
			t.Fatalf("d survived the reload and w completed it: %q", got)
		}
	}
}

// An Alt chord no map binds bubbles, a pending operator untouched: autodb's Alt+h/j/k/l pane keys.
func TestAnUnboundAltChordBubblesPastPendingGrammar(t *testing.T) {
	h, e, sh := mountedEditor(t, mustKeymap(t, `<mapping/>`))
	altH := tui.KeyEvent{Kind: tui.KeyPress, Code: 'h', Mods: tui.ModAlt}
	h.inject(ch('d'), altH, ch('w'))
	h.barrier(sh)
	n := 0
	for _, k := range sh.bubbledKeys() {
		if k.Code == 'h' && k.Mods == tui.ModAlt {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("Alt+h did not bubble to the shell: %d", n)
	}
	if got := editorValue(h, e); got != "two three" {
		t.Fatalf("d's pending operator did not survive Alt+h to complete dw: %q", got)
	}
}
