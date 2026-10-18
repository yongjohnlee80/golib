package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// csvEditor is a terminal Editor holding text, its document view a TableDocument over its own core,
// turned on, focused, at 40×10.
func csvEditor(t *testing.T, text string, core ...widget.CoreOption) (*harness, *shell, *widget.Editor, *widget.TableDocument) {
	t.Helper()
	ed := widget.NewEditor(widget.WithCore(append([]widget.CoreOption{widget.CoreInitialText(text)}, core...)...))
	doc := widget.NewTableDocument(ed.Core(), ',')
	ed.SetDocumentView(doc)
	ed.SetRenderedDocument(true)
	sh := newShell(ed)
	h := startApp(t, sh, 40, 10)
	h.onLoop(func() { h.app.FocusInto(ed) })
	h.barrier(sh)
	return h, sh, ed, doc
}

// views is the editor's view chosen and drawn, and whether the table has the focus.
func views(h *harness, ed *widget.Editor, doc *widget.TableDocument) (chosen, shown widget.EditorView, inTable bool) {
	h.onLoop(func() { chosen, shown, inTable = ed.View(), ed.ShownView(), h.app.FocusWithin(doc) })
	return
}

// TestTheTerminalEditorsDocumentView: Ctrl+T shows the table in place of the
// text and moves the focus into it; keys reach the table and its open field, and a kept value is
// the editor's text; Ctrl+T, which the table leaves, returns to the text with the focus.
func TestTheTerminalEditorsDocumentView(t *testing.T) {
	h, sh, ed, doc := csvEditor(t, "name,n\nann,1\nbob,2\n")
	h.inject(ctrlEv('t'))
	h.barrier(sh)
	if c, s, in := views(h, ed, doc); c != widget.ViewRendered || s != widget.ViewRendered || !in {
		t.Fatalf("Ctrl+T: chosen %v, drawn %v, table focused %v", c, s, in)
	}
	if !strings.HasPrefix(h.row(0), "name") || !strings.Contains(h.row(2), "bob") {
		t.Fatalf("the table is not drawn:\n%q\n%q", h.row(0), h.row(2))
	}
	h.inject(keyText("j"), keyText("l"), dtEnter, keyText("0"), dtEnter)
	h.barrier(sh)
	if v := ed.Core().Value(); v != "name,n\nann,1\nbob,20\n" {
		t.Fatalf("the edit through the table: %q", v)
	}
	h.inject(ctrlEv('t'))
	h.barrier(sh)
	if c, s, in := views(h, ed, doc); c != widget.ViewRaw || s != widget.ViewRaw || in {
		t.Fatalf("Ctrl+T from the table: chosen %v, drawn %v, table focused %v", c, s, in)
	}
	if h.row(2) != "bob,20"+strings.Repeat(" ", 34) {
		t.Fatalf("the text back: %q", h.row(2))
	}
}

// TestTheTableToggleIsTheKeymaps (the gate): a keymap moving the toggle to Ctrl+G moves it in and
// out of the table, Ctrl+T then toggling nothing; with the toggle unbound the table never shows
// by key, and Ctrl+T from it is not consumed.
func TestTheTableToggleIsTheKeymaps(t *testing.T) {
	h, sh, ed, doc := csvEditor(t, "a,b\n1,2\n", widget.CoreKeymap(widget.Keymap{
		{Mode: widget.ModeNormal, Code: 'g', Ctrl: true}: widget.ActToggleRendered,
		{Mode: widget.ModeNormal, Code: 't', Ctrl: true}: widget.ActUnbound,
	}))
	h.inject(ctrlEv('t'))
	h.barrier(sh)
	if _, s, _ := views(h, ed, doc); s != widget.ViewRaw {
		t.Fatal("Ctrl+T, unbound, showed the table")
	}
	h.inject(ctrlEv('g'))
	h.barrier(sh)
	if _, s, in := views(h, ed, doc); s != widget.ViewRendered || !in {
		t.Fatalf("Ctrl+G, rebound: drawn %v, focused %v", s, in)
	}
	h.inject(ctrlEv('t'))
	h.barrier(sh)
	if _, s, _ := views(h, ed, doc); s != widget.ViewRendered {
		t.Fatal("Ctrl+T, unbound, left the table")
	}
	h.inject(ctrlEv('g'))
	h.barrier(sh)
	if _, s, _ := views(h, ed, doc); s != widget.ViewRaw {
		t.Fatal("Ctrl+G from the table did not return to the text")
	}

	h2, sh2, ed2, doc2 := csvEditor(t, "a,b\n1,2\n", widget.CoreKeymap(widget.Keymap{
		{Mode: widget.ModeNormal, Code: 't', Ctrl: true}: widget.ActUnbound,
	}))
	h2.inject(ctrlEv('t'))
	h2.barrier(sh2)
	if _, s, _ := views(h2, ed2, doc2); s != widget.ViewRaw {
		t.Fatal("an unbound toggle showed the table")
	}
	h2.onLoop(func() { ed2.SetView(widget.ViewRendered) })
	h2.barrier(sh2)
	before := len(sh2.bubbledKeys())
	h2.inject(ctrlEv('t'))
	h2.barrier(sh2)
	if _, s, _ := views(h2, ed2, doc2); s != widget.ViewRendered || len(sh2.bubbledKeys()) == before {
		t.Fatalf("an unbound Ctrl+T in the table: drawn %v; bubbled %v", s, len(sh2.bubbledKeys()) > before)
	}
}

// TestTheTableKeepsTheChosenView: withheld for a document, the text is drawn
// and Rendered kept; given back, the table shows with no key. Hidden, an open field is dropped and
// nothing written.
func TestTheTableKeepsTheChosenView(t *testing.T) {
	const text = "a,b\n1,2\n"
	h, sh, ed, doc := csvEditor(t, text)
	h.onLoop(func() { ed.SetView(widget.ViewRendered); ed.SetRenderedEnabled(false) })
	h.barrier(sh)
	if c, s, _ := views(h, ed, doc); c != widget.ViewRendered || s != widget.ViewRaw {
		t.Fatalf("withheld: chosen %v, drawn %v", c, s)
	}
	h.onLoop(func() { ed.SetRenderedEnabled(true) })
	h.barrier(sh)
	if _, s, in := views(h, ed, doc); s != widget.ViewRendered || !in {
		t.Fatalf("given back: drawn %v, focused %v", s, in)
	}
	h.inject(dtEnter, keyText("9"))
	h.barrier(sh)
	h.inject(ctrlEv('t'))
	h.barrier(sh)
	var editing bool
	h.onLoop(func() { editing = doc.Core().Editing() != nil })
	if editing || ed.Core().Value() != text {
		t.Fatalf("hidden with a field open: editing %v, text %q", editing, ed.Core().Value())
	}
}

func ctrlEv(r rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: r, Mods: tui.ModCtrl} }
