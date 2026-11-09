package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/editor"
)

// The editor's one key system, through HandleKey: where a binding runs is its
// declaration's phase, a modified key reaches its bare key's editing command with the modifiers as
// qualifiers, and a pending key's sub-scope is looked up first.

func keyPress(code rune, mods tui.Mods) tui.KeyEvent { return tui.KeyEvent{Code: code, Mods: mods} }

// Replace mode restarts its stack on the default Right, as the raw key always did, so Backspace
// then only moves back; a host's overlay binding of Right is early and bypasses Replace, so
// Backspace restores the character it replaced.
func TestReplaceSeesTheDefaultRightButNotAnOverlaysRight(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []editor.CoreOption
		want string
	}{
		{"the default Right", nil, "xbcdef"},
		{"an overlay's Right", []editor.CoreOption{editor.CoreKeymap(editor.Keymap{{Mode: editor.ModeInsert, Code: tui.KeyRight}: editor.ActRight})}, "xacdef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := coreWith(t, "abcdef", tc.opts...)
			keys(c, "Rx")
			c.HandleKey(keyPress(tui.KeyRight, 0))
			c.HandleKey(keyPress(tui.KeyBackspace, 0))
			if got := c.Value(); got != tc.want {
				t.Fatalf("R, x, Right, Backspace: %q; want %q", got, tc.want)
			}
		})
	}
}

// In a table, Tab and Shift+Tab move between cells; Ctrl+Tab, a heavy qualifier the table assist
// declines, inserts a literal tab, as it always did.
func TestATablesTabMovesCellsAndCtrlTabInsertsATab(t *testing.T) {
	c, _ := coreWith(t, sampleTable, editor.CoreTableEditing(true))
	keys(c, "i")
	c.HandleKey(keyPress(tui.KeyTab, 0))
	if got := c.TextIn(cellSelection(t, c)); got != "Column2" {
		t.Fatalf("Tab selected %q, want Column2", got)
	}
	c.HandleKey(keyPress(tui.KeyTab, tui.ModShift))
	if got := c.TextIn(cellSelection(t, c)); got != "Column1" {
		t.Fatalf("Shift+Tab selected %q, want Column1", got)
	}
	c.HandleKey(keyPress(tui.KeyLeft, 0))
	before := c.Value()
	c.HandleKey(keyPress(tui.KeyTab, tui.ModCtrl))
	if got := c.Value(); got == before || len(got) != len(before)+1 {
		t.Fatalf("Ctrl+Tab in a table: %q, want one tab inserted into %q", got, before)
	}
}

// A list continues on Enter; Shift+Enter, Shift a qualifier the list assist declines, breaks the line
// without a new marker.
func TestAListContinuesOnEnterButNotOnShiftEnter(t *testing.T) {
	c, _ := coreWith(t, "- a", editor.CoreKeyset(editor.KeysetStandard), editor.CoreListEditing(true))
	c.HandleKey(keyPress(tui.KeyEnd, 0))
	c.HandleKey(keyPress(tui.KeyEnter, 0))
	if got := c.Value(); got != "- a\n- " {
		t.Fatalf("Enter on a list item: %q", got)
	}
	c2, _ := coreWith(t, "- a", editor.CoreKeyset(editor.KeysetStandard), editor.CoreListEditing(true))
	c2.HandleKey(keyPress(tui.KeyEnd, 0))
	c2.HandleKey(keyPress(tui.KeyEnter, tui.ModShift))
	if got := c2.Value(); got != "- a\n" {
		t.Fatalf("Shift+Enter on a list item: %q, want a plain break", got)
	}
}

// A scheme that moves the tab command moves the table's Tab with it: the new chord moves cells,
// and plain Tab, then nobody's key, is not consumed. The core reads the scheme from the App's key
// map; the keys are handed to it on the loop.
func TestAMovedTabCommandTakesTheTablesTabWithIt(t *testing.T) {
	m := mustKeymap(t, `<mapping><shortcut id="tui.editor.vim.insert.insertTab"><key value="Alt+I"/></shortcut></mapping>`)
	h, e, sh := mountedEditor(t, m, editor.WithCore(editor.CoreInitialText(sampleTable)), editor.WithCore(editor.CoreTableEditing(true)))
	defer h.stop()
	h.inject(ch('i'))
	h.barrier(sh)
	var moved, took bool
	var at editor.TextPos
	h.onLoop(func() {
		c := e.Core()
		moved = c.HandleKey(tui.KeyEvent{Kind: tui.KeyPress, Code: 'i', Mods: tui.ModAlt})
		at = c.Cursor()
		took = c.HandleKey(keyPress(tui.KeyTab, 0))
	})
	if !moved || at != (editor.TextPos{Line: 0, Col: 19}) {
		t.Fatalf("Alt+I in a table: consumed %v, cursor %+v; want it at Column2's end", moved, at)
	}
	if took {
		t.Fatal("plain Tab, no command's key after the scheme, was consumed")
	}
}

// A held escape-chord rune settles as text when a late key comes, and when an early one does; its
// second rune completes the escape.
func TestTheHeldChordRuneSettlesOnLateAndEarlyKeys(t *testing.T) {
	c, _ := coreWith(t, "")
	keys(c, "ij")
	c.HandleKey(keyPress(tui.KeyTab, 0)) // late
	if got := c.Value(); got != "j\t" {
		t.Fatalf("j then Tab: %q", got)
	}
	keys(c, "j")
	c.HandleKey(tui.KeyEvent{Code: 'w', Mods: tui.ModCtrl}) // early: delete the word back
	if got := c.Value(); got != "j\t" {
		t.Fatalf("j then Ctrl+W: %q; want the held j settled, then deleted", got)
	}
	keys(c, "jk")
	if c.Mode() != editor.ModeNormal {
		t.Fatalf("jk left the mode %v", c.Mode())
	}
}

// An overlay's unbind of Tab in Insert releases Tab and its modified forms: none reaches the tab
// command, so each bubbles.
func TestAnUnboundTabLendsItsModifiedFormsNothing(t *testing.T) {
	c, _ := coreWith(t, "", editor.CoreKeymap(editor.Keymap{{Mode: editor.ModeInsert, Code: tui.KeyTab}: editor.ActUnbound}))
	keys(c, "i")
	for _, mods := range []tui.Mods{0, tui.ModCtrl, tui.ModAlt} {
		if c.HandleKey(keyPress(tui.KeyTab, mods)) {
			t.Fatalf("Tab with %v was consumed after Tab was unbound", mods)
		}
	}
	if c.Value() != "" {
		t.Fatalf("an unbound Tab inserted: %q", c.Value())
	}
}

// Modeless Esc clears the selection anchor, else cancels a find, else is the host's.
func TestModelessEscWithNothingToClearBubbles(t *testing.T) {
	c, _ := coreWith(t, "text", editor.CoreKeyset(editor.KeysetStandard))
	if c.HandleKey(keyPress(tui.KeyEscape, 0)) {
		t.Fatal("Esc with nothing to clear was consumed")
	}
}

// A read-only core refuses an editing key, consumed, and still moves on a caret key.
func TestAReadOnlyCoreRefusesEditingKeysAndMoves(t *testing.T) {
	c, _ := coreWith(t, "ab", editor.CoreKeyset(editor.KeysetStandard), editor.CoreReadOnly(true))
	if !c.HandleKey(keyPress(tui.KeyEnter, 0)) || c.Value() != "ab" {
		t.Fatalf("Enter in a read-only core: %q", c.Value())
	}
	at := c.Cursor()
	c.HandleKey(keyPress(tui.KeyRight, 0))
	if c.Cursor() == at {
		t.Fatal("Right did not move a read-only core")
	}
}

// With the g prefix moved to ! (a key Vim's Normal leaves unbound here; z is the z prefix's), the
// goLine command's key completes gg: !g goes to the first line, and !! does not.
func TestGGFollowsTheGoLineCommand(t *testing.T) {
	m := mustKeymap(t, `<mapping><shortcut id="tui.editor.vim.normal.goPrefix"><key value="!"/></shortcut></mapping>`)
	h, e, sh := mountedEditor(t, m, editor.WithCore(editor.CoreInitialText("one\ntwo\nthree")))
	defer h.stop()
	line := func() int {
		var l int
		h.onLoop(func() { l = e.Core().Cursor().Line })
		return l
	}
	h.inject(ch('j'), ch('j'), ch('!'), ch('!'))
	h.barrier(sh)
	if got := line(); got != 2 {
		t.Fatalf("!! moved to line %d; want it to do nothing", got)
	}
	h.inject(ch('!'), ch('g'))
	h.barrier(sh)
	if got := line(); got != 0 {
		t.Fatalf("!g left line %d; want the first", got)
	}
}

// A g command a scheme binds to an Alt chord runs: the sub-scope is looked up before the root's Alt
// bubbling.
func TestAnAltBoundGCommandRuns(t *testing.T) {
	m := mustKeymap(t, `<mapping><shortcut id="tui.editor.vim.g.goLine"><key value="Alt+G"/></shortcut></mapping>`)
	h, e, sh := mountedEditor(t, m, editor.WithCore(editor.CoreInitialText("one\ntwo")))
	defer h.stop()
	h.inject(ch('j'), ch('g'), tui.KeyEvent{Kind: tui.KeyPress, Code: 'g', Mods: tui.ModAlt})
	h.barrier(sh)
	var l int
	h.onLoop(func() { l = e.Core().Cursor().Line })
	if l != 0 {
		t.Fatalf("g then Alt+G left line %d; want the first", l)
	}
}

// A root unbind of i in Normal does not stop i naming a text object after an operator: the object
// sub-scope is looked up first.
func TestARootUnbindDoesNotHideTheObjectSubScope(t *testing.T) {
	c, _ := coreWith(t, "one two", editor.CoreKeymap(editor.Keymap{{Mode: editor.ModeNormal, Code: 'i'}: editor.ActUnbound}))
	keys(c, "diw")
	if got := c.Value(); got != " two" {
		t.Fatalf("diw with i unbound in Normal: %q", got)
	}
}

// A digit with Alt held is not a count: Alt+2 then j moves one line, not two.
func TestAModifiedDigitIsNotACount(t *testing.T) {
	c, _ := coreWith(t, "a\nb\nc")
	c.HandleKey(tui.KeyEvent{Code: '2', Text: "2", Mods: tui.ModAlt})
	keys(c, "j")
	if got := c.Cursor().Line; got != 1 {
		t.Fatalf("Alt+2 j: line %d; want 1", got)
	}
}

// Insert's editing commands are listed: the help reads them as bindings.
func TestInsertsEditingCommandsAreListed(t *testing.T) {
	c, _ := coreWith(t, "")
	var tab bool
	for _, b := range c.BindingsForMode(editor.ModeInsert) {
		if b.Action == editor.ActInsertTab {
			tab = true
		}
	}
	if !tab {
		t.Fatal("Insert's Tab is not among Insert's bindings")
	}
	if got := editor.VimKeymap()[editor.KeyChord{Mode: editor.ModeInsert, Code: tui.KeyTab}]; got != editor.ActUnbound {
		t.Fatalf("VimKeymap, the legacy table, has Insert Tab bound to %v", got)
	}
}

// Replace takes its keys bare: Ctrl+Backspace reaches the backspace command with Ctrl as a heavy
// qualifier, which Replace declines, so it deletes as Insert's Backspace does: the x goes, and the a
// it overwrote does not come back as Replace's Backspace would bring it.
func TestReplaceDeclinesAHeavilyQualifiedKey(t *testing.T) {
	c, _ := coreWith(t, "abc")
	keys(c, "Rx")
	c.HandleKey(keyPress(tui.KeyBackspace, tui.ModCtrl))
	if got := c.Value(); got != "bc" {
		t.Fatalf("R, x, Ctrl+Backspace: %q; want the x deleted", got)
	}
}

// A keyset names itself; one the editor does not know is Custom.
func TestAKeysetNamesItself(t *testing.T) {
	for ks, want := range map[editor.Keyset]string{editor.KeysetVim: "Vim", editor.KeysetNano: "Nano", editor.KeysetStandard: "Standard", editor.Keyset(99): "Custom"} {
		if got := ks.String(); got != want {
			t.Errorf("Keyset(%d) is %q, want %q", int(ks), got, want)
		}
	}
}

// A caret key a keyset binds early (Nano's and Standard's Home, End and Page keys, an overlay's
// arrow) still moves the caret held with a modifier no binding names, as Insert's caret keys always
// did: Ctrl+Home goes to the line's start, and Ctrl+PgDn moves a page rather than bubbling to the
// application.
func TestAnEarlyBoundCaretKeyStillMovesWithAModifier(t *testing.T) {
	text := "0123\n1\n2\n3\n4\n5\n6\n7\n8\n9"
	right := editor.CoreKeymap(editor.Keymap{{Mode: editor.ModeInsert, Code: tui.KeyRight}: editor.ActLineEnd})
	for _, tc := range []struct {
		name string
		opts []editor.CoreOption
		pre  string // Vim keys before the caret key; Nano and Standard start in Insert
		key  tui.KeyEvent
		want editor.TextPos
	}{
		{"Nano Ctrl+End", []editor.CoreOption{editor.CoreKeyset(editor.KeysetNano)}, "", keyPress(tui.KeyEnd, tui.ModCtrl), editor.TextPos{Line: 0, Col: 4}},
		{"Standard Ctrl+End", []editor.CoreOption{editor.CoreKeyset(editor.KeysetStandard)}, "", keyPress(tui.KeyEnd, tui.ModCtrl), editor.TextPos{Line: 0, Col: 4}},
		{"Nano Alt+PgDn", []editor.CoreOption{editor.CoreKeyset(editor.KeysetNano)}, "", keyPress(tui.KeyPageDown, tui.ModAlt), editor.TextPos{Line: 5, Col: 0}},
		{"Standard Ctrl+PgDn", []editor.CoreOption{editor.CoreKeyset(editor.KeysetStandard)}, "", keyPress(tui.KeyPageDown, tui.ModCtrl), editor.TextPos{Line: 5, Col: 0}},
		{"an overlay's Right, with Super", []editor.CoreOption{right}, "i", keyPress(tui.KeyRight, tui.ModSuper), editor.TextPos{Line: 0, Col: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := coreWith(t, text, tc.opts...)
			keys(c, tc.pre)
			if !c.HandleKey(tc.key) {
				t.Fatal("the key bubbled; want it to move the caret")
			}
			if got := c.Cursor(); got != tc.want {
				t.Fatalf("the caret at %+v, want %+v", got, tc.want)
			}
		})
	}
	c, _ := coreWith(t, text, editor.CoreKeyset(editor.KeysetStandard))
	c.HandleKey(keyPress(tui.KeyEnd, 0))
	if !c.HandleKey(keyPress(tui.KeyHome, tui.ModCtrl)) || c.Cursor() != (editor.TextPos{}) {
		t.Fatalf("Standard End, Ctrl+Home: the caret at %+v, want the line's start", c.Cursor())
	}
}

// A read-only core consumes and refuses Tab, Enter, Backspace and Delete whatever a scheme binds
// them to, as it always did: an overlay's Enter bound to Undo, or an unbound Tab, never reaches the
// host while the viewer does nothing.
func TestAReadOnlyCoreConsumesAnEditingKeyWhateverItsBinding(t *testing.T) {
	km := editor.CoreKeymap(editor.Keymap{
		{Mode: editor.ModeInsert, Code: tui.KeyEnter}: editor.ActUndo,
		{Mode: editor.ModeInsert, Code: tui.KeyTab}:   editor.ActUnbound,
	})
	c, _ := coreWith(t, "ab", editor.CoreKeyset(editor.KeysetStandard), editor.CoreReadOnly(true), km)
	for _, k := range []tui.KeyEvent{keyPress(tui.KeyEnter, 0), keyPress(tui.KeyTab, 0), keyPress(tui.KeyBackspace, tui.ModCtrl)} {
		if !c.HandleKey(k) || c.Value() != "ab" {
			t.Errorf("%+v in a read-only core: consumed false or %q; want it consumed and refused", k, c.Value())
		}
	}
}
