package widget_test

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Each widget is its own scope, and App.KeyBindings lists its own commands first: its stop is the
// first on the route.
func TestEachWidgetsScopeAndOwnStopByTheRoute(t *testing.T) {
	for _, c := range []struct {
		name  string
		build func() tui.Component
		scope keymap.Scope
		first string // the first command of its stop; "" for a widget with none
	}{
		{"Tabs", func() tui.Component {
			return widget.NewTabs(widget.WithTab("a", widget.NewText("a")), widget.WithTab("b", widget.NewText("b")))
		}, widget.TabsScope, widget.TabsPrev.ID},
		{"TabStrip", func() tui.Component {
			s := widget.NewTabStrip()
			s.SetTabs([]widget.StripTab{{ID: "a", Label: "a"}})
			return s
		}, widget.TabStripScope, widget.TabStripAdd.ID},
		{"TerminalTabs", func() tui.Component { return widget.NewTerminalTabs() }, widget.TabStripScope, widget.TabStripAdd.ID},
		{"Split horizontal", func() tui.Component {
			return widget.NewSplit(widget.Horizontal, widget.NewText("a"), widget.NewText("b"))
		}, widget.SplitScope, widget.SplitCancel.ID},
		{"Resizable", func() tui.Component { return widget.NewResizable(widget.NewText("a")) }, widget.ResizableScope, widget.ResizableCancel.ID},
		{"Select", func() tui.Component { return widget.NewSelect[string]() }, widget.SelectScope, widget.SelectOpen.ID},
		{"TextArea", func() tui.Component { return widget.NewTextArea() }, widget.TextAreaScope, widget.TextAreaNewline.ID},
		{"HTMLView", func() tui.Component { return widget.NewHTMLView() }, widget.HTMLViewScope, widget.HTMLViewUp.ID},
		{"BufferView", func() tui.Component { return widget.NewBufferView() }, widget.BufferViewScope, widget.BufferViewUp.ID},
		{"FileOpenView", func() tui.Component {
			return widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{
				FS: fstest.MapFS{"a.md": {Data: []byte("a")}}, Root: "/"}))
		}, widget.FileViewScope, widget.FileViewHome.ID},
		{"Image that does not scroll", func() tui.Component { return widget.NewImage() }, widget.ImageScope, ""},
		{"Terminal", func() tui.Component { return widget.NewTerminal() }, widget.TerminalScope, widget.TerminalPrefix.ID},
	} {
		w := c.build()
		h := startApp(t, newShell(w), 60, 12)
		var scope keymap.Scope
		var bs []keymap.Binding
		var err error
		h.onLoop(func() { scope = h.app.KeyScopeOf(w); bs, err = h.app.KeyBindings(w) })
		h.stop()
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if scope != c.scope {
			t.Errorf("%s: scope %q, want %q", c.name, scope, c.scope)
		}
		switch {
		case c.first == "":
			for _, b := range bs {
				if b.Scope == c.scope {
					t.Errorf("%s: lists %s, but has no keys", c.name, b.Command.ID)
				}
			}
		case len(bs) == 0 || bs[0].Command.ID != c.first:
			t.Errorf("%s: its stop is not first: %v", c.name, bs)
		}
	}
}

// A scheme moving the escape prefix moves it: the old Ctrl+\ goes to the program, the new key arms the
// prefix, and the Terminal's scope follows it into normal mode and its search line. (Not F12: the
// harness's barrier key, which would arm the prefix itself.)
func TestATerminalFollowsItsKeymap(t *testing.T) {
	m := mustKeymap(t, `<mapping><shortcut id="tui.terminal.prefix"><key value="F11"/></shortcut></mapping>`)
	f := startTermApp(t, 40, 6, []tui.AppOption{tui.WithKeymap(m)})
	scope := func() keymap.Scope {
		var s keymap.Scope
		f.h.onLoop(func() { s = f.h.app.KeyScopeOf(f.term) })
		return s
	}
	ctrl := func(r rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: r, Mods: tui.ModCtrl} }
	f.h.inject(ctrl('\\'), ctrl('n'))
	f.h.barrier(f.sh)
	if got := scope(); got != widget.TerminalScope {
		t.Fatalf("Ctrl+\\ Ctrl+n after the scheme moved the prefix: scope %q", got)
	}
	f.h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF11}, ctrl('n'))
	f.h.barrier(f.sh)
	if got := scope(); got != widget.TerminalNormalScope {
		t.Fatalf("F11 Ctrl+n: scope %q, want normal mode's", got)
	}
	f.h.inject(ch('/'))
	f.h.barrier(f.sh)
	if got := scope(); got != widget.TerminalSearchScope {
		t.Fatalf("/: scope %q, want the search line's", got)
	}
	var bs []keymap.Binding
	f.h.onLoop(func() { bs, _ = f.h.app.KeyBindings(f.term) })
	if len(bs) == 0 || bs[0].Command.ID != widget.TermSearchClose.ID {
		t.Errorf("the search line's stop: %v", bs)
	}
	f.h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape}, ch('i'))
	f.h.barrier(f.sh)
	if got := scope(); got != widget.TerminalScope {
		t.Fatalf("Esc then i: scope %q, want input mode's", got)
	}
}

// A scheme moving the Tabs' next key moves it while focus is in the content: the new key steps, the old
// one no longer does.
func TestTabsFollowTheirKeymap(t *testing.T) {
	m := mustKeymap(t, `<mapping><shortcut id="tui.tabs.next"><key value="Alt+N"/></shortcut></mapping>`)
	tb := widget.NewTabs(widget.WithTab("a", widget.NewTextInput()), widget.WithTab("b", widget.NewTextInput()))
	sh := newShell(tb)
	h := startAppOpts(t, sh, 40, 6, tui.WithKeymap(m))
	h.inject(tab())
	h.barrier(sh)
	active := func() int {
		var i int
		h.onLoop(func() { i = tb.Active() })
		return i
	}
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyPageDown, Mods: tui.ModCtrl})
	h.barrier(sh)
	if got := active(); got != 0 {
		t.Fatalf("Ctrl+PgDn after the scheme moved next: tab %d", got)
	}
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 'n', Mods: tui.ModAlt})
	h.barrier(sh)
	if got := active(); got != 1 {
		t.Fatalf("Alt+N: tab %d, want 1", got)
	}
}

// A scheme moving the TextArea's newline moves it: Ctrl+J breaks the line, and Enter no longer does.
func TestATextAreaFollowsItsKeymap(t *testing.T) {
	m := mustKeymap(t, `<mapping><shortcut id="tui.textarea.newline"><key value="Ctrl+J"/></shortcut></mapping>`)
	ta := widget.NewTextArea()
	sh := newShell(ta)
	h := startAppOpts(t, sh, 40, 6, tui.WithKeymap(m))
	h.inject(tab(), ch('a'), tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}, ch('b'),
		tui.KeyEvent{Kind: tui.KeyPress, Code: 'j', Mods: tui.ModCtrl}, ch('c'))
	h.barrier(sh)
	var v string
	h.onLoop(func() { v = ta.Value() })
	if v != "ab\nc" {
		t.Fatalf("value %q, want %q", v, "ab\nc")
	}
}

// g's grammar: any plain key ends a pending g, so g, x, g does not go to the first line; g, g does.
func TestATerminalsPendingGEndsOnAnyPlainKey(t *testing.T) {
	f := startTerm(t, 20, 4, widget.WithVimKeys(true), widget.WithScrollback(50))
	for i := range 20 {
		f.output(fmt.Sprintf("line %d\r\n", i))
	}
	f.h.inject(key(tui.KeyEscape))
	f.h.waitFor("normal", func() bool { return f.mode() == widget.TerminalNormal })
	row := func() int {
		_, y, _ := f.h.tb.CursorPos()
		return y
	}
	f.h.inject(key('G'))
	f.h.waitFor("G", func() bool { return row() == 3 })
	f.h.inject(key('g'), key('x'), key('g'))
	f.h.barrier(f.sh)
	if y := row(); y != 3 {
		t.Fatalf("g x g moved the cursor to row %d: x did not end the first g", y)
	}
	f.h.inject(key('g'), key('g'))
	f.h.waitFor("g g", func() bool { return row() == 0 })
}

// Esc in a file chooser's listing clears a filter it kept, and every entry is listed again.
func TestAFileChoosersEscClearsItsFilter(t *testing.T) {
	v := widget.NewFileOpenView(widget.WithFileViewPreview(false), widget.WithFileViewDir("/"),
		widget.WithFileViewSource(widget.FileSource{
			FS: fstest.MapFS{"a.md": {Data: []byte("a")}, "b.txt": {Data: []byte("b")}}, Root: "/"}))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("b.txt")
	h.waitFor("the list has the keyboard", func() bool { return hintIs(h, v, "tui.files.hint.openList") })
	h.inject(ch('/'))
	// the filter's pane is shown by /, and its field takes the keyboard once it is laid out
	h.waitFor("the filter has the keyboard", func() bool { return hintIs(h, v, "tui.files.hint.filter") })
	h.inject(ch('a'), key(tui.KeyEnter))
	for deadline := time.Now().Add(3 * time.Second); strings.Contains(h.grid(), "b.txt"); h.settle() {
		if time.Now().After(deadline) {
			var hint string
			h.onLoop(func() { hint = v.HintMessage().ID })
			t.Fatalf("/ a Enter: b.txt is still listed (hint %s):\n%s", hint, h.grid())
		}
	}
	h.inject(key(tui.KeyEscape))
	h.shows("b.txt")
}
