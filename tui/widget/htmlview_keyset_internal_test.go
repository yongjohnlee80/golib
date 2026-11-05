package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// pagePrinting is every printing key the page reads under Vim: under a modeless keyset each types.
const pagePrinting = "hjkl[]{}gGy "

// pageKey is r as a key typed: its code and its text, Shift held for an upper-case letter as a
// terminal reports it.
func pageKey(r rune) tui.KeyEvent {
	e := tui.KeyEvent{Kind: tui.KeyPress, Code: r, Text: string(r)}
	if r >= 'A' && r <= 'Z' {
		e.Mods = tui.ModShift
	}
	return e
}

// A page reads its printing keys by its keyset: under Vim each navigates (it is read, and the page
// keeps it), under Standard and Nano none is read, so each goes on to the host to type, and the
// keys that never type (the arrows, Alt and an arrow to travel, PgDn, Ctrl+C) are read in all three.
func TestThePagesPrintingKeysAreVimsOnly(t *testing.T) {
	page := "<p>top</p>" + navFence(wideSequence) + "<h2>Two</h2><p>after</p>"
	for _, ks := range []Keyset{KeysetVim, KeysetStandard, KeysetNano} {
		v, _ := navView(t, page, navRoom+1, 8)
		v.FollowKeyset(func() Keyset { return ks })
		for _, r := range pagePrinting {
			e := pageKey(r)
			reads := v.ReadsKey(e)
			if handled := v.HandleEvent(e); handled != reads {
				t.Errorf("%v %q: HandleEvent %v but ReadsKey %v", ks, r, handled, reads)
			}
			if want := ks == KeysetVim; reads != want {
				t.Errorf("%v: %q read %v, want %v (Vim reads the printing keys, a modeless keyset types them)", ks, r, reads, want)
			}
		}
		for _, e := range []tui.KeyEvent{navKey(tui.KeyLeft, ""), navKey(tui.KeyRight, ""), navKey(tui.KeyDown, ""),
			{Kind: tui.KeyPress, Code: tui.KeyDown, Mods: tui.ModAlt}, {Kind: tui.KeyPress, Code: tui.KeyUp, Mods: tui.ModAlt},
			{Kind: tui.KeyPress, Code: tui.KeyDown, Mods: tui.ModAlt | tui.ModShift},
			{Kind: tui.KeyPress, Code: tui.KeyUp, Mods: tui.ModAlt | tui.ModShift},
			navKey(tui.KeyPageDown, ""), {Kind: tui.KeyPress, Code: 'c', Mods: tui.ModCtrl}} {
			if !v.ReadsKey(e) {
				t.Errorf("%v: %+v isn't read; a key that never types is the page's in every keyset", ks, e)
			}
		}
	}
}

// Under a modeless keyset the page travels by Alt and an arrow: Alt+Down to the next block and
// Alt+Up back, Alt+Shift+Down to the next heading and Alt+Shift+Up back, as ] [ } { do under Vim.
func TestAModelessPageTravelsByAltAndAnArrow(t *testing.T) {
	v, c := navView(t, markdownHTML(t, navPage), 40, 4)
	v.FollowKeyset(func() Keyset { return KeysetStandard })
	blocks, headings := c.Starts(false), c.Starts(true)
	press := func(code rune, mods tui.Mods) float32 {
		t.Helper()
		if !v.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: code, Mods: mods}) {
			t.Fatalf("%q with %v wasn't read", code, mods)
		}
		return v.ScrollY()
	}
	if got := press(tui.KeyDown, tui.ModAlt); got != blocks[1] {
		t.Fatalf("Alt+Down from the top: %v, want the next block's %v", got, blocks[1])
	}
	if got := press(tui.KeyUp, tui.ModAlt); got != blocks[0] {
		t.Fatalf("Alt+Up: %v, want the first block's %v", got, blocks[0])
	}
	if got := press(tui.KeyDown, tui.ModAlt|tui.ModShift); got != headings[1] {
		t.Fatalf("Alt+Shift+Down: %v, want the next heading's %v", got, headings[1])
	}
	if got := press(tui.KeyUp, tui.ModAlt|tui.ModShift); got != headings[0] {
		t.Fatalf("Alt+Shift+Up: %v, want the first heading's %v", got, headings[0])
	}
	if got := press(tui.KeyDown, 0); got != 1 {
		t.Fatalf("Down (no Alt) moved the top to %v, want a line, 1", got)
	}
}

// An Editor's document view follows the editor's keyset, as it is at each key: a page mounted in a
// Vim editor reads j, and once the editor is switched to Standard it doesn't (j goes on to type),
// and back.
func TestAnEditorsPageFollowsItsKeyset(t *testing.T) {
	e := NewEditor()
	v := NewHTMLView()
	e.SetDocumentView(v)
	j := pageKey('j')
	if e.Core().Keyset() != KeysetVim || !v.ReadsKey(j) {
		t.Fatalf("a page in a Vim editor (%v) doesn't read j", e.Core().Keyset())
	}
	e.Core().SetKeyset(KeysetStandard)
	if v.ReadsKey(j) {
		t.Fatal("the editor switched to Standard: its page still reads j, which must type")
	}
	e.Core().SetKeyset(KeysetVim)
	if !v.ReadsKey(j) {
		t.Fatal("the editor back on Vim: its page doesn't read j")
	}
}
