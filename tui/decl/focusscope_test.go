package decl_test

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// focusscope_test.go: a Shortcut is live while focus is inside the element that declares it — a
// FocusScope or a Drawer as a Window or a Dialog — the innermost first, after the focused widget
// and before the Window; and a FocusScope is otherwise not there.

func runScoped(t *testing.T, w, h int, body string) (*decltest.Screen, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := decltest.Run(t, w, h,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+body+"\n}")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.hit": rec.handler}))
	return s, rec
}

// hits waits for the handler to have been called n times, and is what it was called with.
func hits(t *testing.T, rec *recorder, n int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for len(rec.all()) < n && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	var got []string
	for _, v := range rec.all() {
		got = append(got, v.Raw)
	}
	if len(got) < n {
		t.Fatalf("the handler was called %d times, want %d: %v", len(got), n, got)
	}
	return got
}

// press is a key as a terminal sends it: a printable one with its text, as typed.
func press(code rune) tui.KeyEvent {
	if unicode.IsPrint(code) {
		return decltest.Rune(code)
	}
	return tui.KeyEvent{Kind: tui.KeyPress, Code: code}
}

// TestAScopesShortcutWinsInsideItAndTheWindowsOutside: d inside the scope is the scope's, the
// innermost scope's where two hold it, an outer scope's own key still fires from inside the inner,
// and d outside every scope is the Window's.
func TestAScopesShortcutWinsInsideItAndTheWindowsOutside(t *testing.T) {
	s, rec := runScoped(t, 40, 8, `
    Shortcut { sequence: "d"; onActivated: App.hit("window") }
    Flex {
        direction: Tui.Vertical
        Button { text: "outside" }
        FocusScope {
            Shortcut { sequence: "d"; onActivated: App.hit("outer") }
            Shortcut { sequence: "x"; onActivated: App.hit("outer x") }
            FocusScope {
                Shortcut { sequence: "d"; onActivated: App.hit("inner") }
                Button { text: "inside" }
            }
        }
    }`)
	s.WaitForText(t, "inside")
	// nothing has the keyboard until Tab gives it to the first Button, outside every scope
	s.Keys(t, press(tui.KeyTab), press('d'))
	s.Keys(t, press(tui.KeyTab), press('d'), press('x'))
	if got, want := hits(t, rec, 3), []string{"window", "inner", "outer x"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestAFocusedFieldStillTypesAScopesLetter: the focused widget has a key before any scope does.
func TestAFocusedFieldStillTypesAScopesLetter(t *testing.T) {
	s, rec := runScoped(t, 40, 6, `
    FocusScope {
        Shortcut { sequence: "d"; onActivated: App.hit("scope") }
        TextField { text: "" }
    }`)
	s.Keys(t, press(tui.KeyTab), press('a'), press('d')) // nothing has the keyboard until Tab
	s.WaitForText(t, "ad")
	if got := rec.all(); len(got) != 0 {
		t.Fatalf("the scope took a letter the field types: %v", got)
	}
}

// TestADrawersShortcutsAreLiveWhileFocusIsInIt: open, the Drawer's d wins over the Window's, and
// its Escape claims the key it would close on; closed, d is the Window's again.
func TestADrawersShortcutsAreLiveWhileFocusIsInIt(t *testing.T) {
	s, rec := runScoped(t, 50, 10, `
    Shortcut { sequence: "d"; onActivated: App.hit("window") }
    Button { text: "page" }
    Drawer {
        id: panel
        modal: false
        Shortcut { sequence: "d"; onActivated: App.hit("drawer") }
        Shortcut { sequence: "Esc"; onActivated: App.hit("drawer esc") }
        Frame { title: "panel"; Button { id: inDrawer; text: "drw" } }
    }`)
	s.WaitForText(t, "page")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("panel", "open"); err != nil {
			t.Error(err)
		}
	})
	// a Drawer that is not modal leaves the keyboard where it was; the host moves it in, as
	// AutoDoc's panels do
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("inDrawer", "forceActiveFocus"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "drw")
	s.Keys(t, press('d'), press(tui.KeyEscape))
	if got, want := hits(t, rec, 2), []string{"drawer", "drawer esc"}; !slices.Equal(got, want) {
		t.Fatalf("open: got %v, want %v", got, want)
	}
	if !strings.Contains(s.String(), "drw") {
		t.Fatalf("a Shortcut on Escape closed the Drawer anyway:\n%s", s)
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("panel", "close"); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "the drawer closed", func(sc string) bool { return !strings.Contains(sc, "drw") })
	s.Keys(t, press('d'))
	if got := hits(t, rec, 3); got[2] != "window" {
		t.Fatalf("closed: got %v, want the Window's d", got)
	}
}

// TestAShortcutOutsideAHolderIsRefused, and the refusal names the fix.
func TestAShortcutOutsideAHolderIsRefused(t *testing.T) {
	_, err := tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window {
    Frame {
        Shortcut { sequence: "d" }
        Text { text: "x" }
    }
}`)))
	if err == nil || !strings.Contains(err.Error(), "put it in a FocusScope") || !strings.Contains(err.Error(), "Frame") {
		t.Fatalf("err = %v, want the Shortcut in a Frame refused, naming FocusScope", err)
	}
}

// TestAFocusScopeIsNotThere: wrapping an item in a FocusScope leaves the screen as it was (the
// wrapped Frame's Layout.fillHeight is the scope's), the Tab order as it was, and a click on a
// Button in it presses that Button.
func TestAFocusScopeIsNotThere(t *testing.T) {
	doc := func(wrap bool) string {
		middle := `Frame { Layout.fillHeight: true; title: "body"; Button { text: "b2"; onClicked: App.hit("b2") } }`
		if wrap {
			middle = "FocusScope {\n Shortcut { sequence: \"z\" }\n " + middle + "\n}"
		}
		return `
    Flex {
        direction: Tui.Vertical
        Button { text: "b1"; onClicked: App.hit("b1") }
        ` + middle + `
        Button { text: "b3"; onClicked: App.hit("b3") }
    }`
	}
	type seen struct {
		screen string
		order  []string
	}
	look := func(wrap bool) seen {
		s, rec := runScoped(t, 40, 12, doc(wrap))
		s.WaitForText(t, "b3")
		screen := s.String()
		// each Tab moves the keyboard on, from nothing to the first Button; Enter presses the one
		// that has it
		for range 3 {
			s.Keys(t, press(tui.KeyTab), press(tui.KeyEnter))
		}
		return seen{screen, hits(t, rec, 3)}
	}
	bare, wrapped := look(false), look(true)
	if bare.screen != wrapped.screen {
		t.Fatalf("the screen changed:\nbare:\n%s\nwrapped:\n%s", bare.screen, wrapped.screen)
	}
	if !slices.Equal(bare.order, wrapped.order) || !slices.Equal(bare.order, []string{"b1", "b2", "b3"}) {
		t.Fatalf("Tab order: bare %v, wrapped %v; want b1 b2 b3 for both", bare.order, wrapped.order)
	}

	s, rec := runScoped(t, 40, 12, doc(true))
	s.WaitForText(t, "b2")
	x, y := -1, -1
	for row, line := range strings.Split(s.String(), "\n") {
		if i := strings.Index(line, "b2"); i >= 0 {
			x, y = len([]rune(line[:i])), row
		}
	}
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
	if got := hits(t, rec, 1); got[0] != "b2" {
		t.Fatalf("a click on b2 in the scope: got %v", got)
	}
}

// TestAFocusScopesContentAttachedIsCheckedByItsParent: a Layout property on a FocusScope's content
// reaches the Flex around the scope; under a Window, which reads no Layout, it is refused.
func TestAFocusScopesContentAttachedIsCheckedByItsParent(t *testing.T) {
	_, err := tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window {
    FocusScope {
        Shortcut { sequence: "z" }
        Frame { Layout.fillHeight: true; Text { text: "x" } }
    }
}`)))
	if err == nil || !strings.Contains(err.Error(), "FocusScope's parent Window does not read Layout") {
		t.Fatalf("err = %v, want Layout.fillHeight refused under the Window", err)
	}
}
