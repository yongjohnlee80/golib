package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// button_selfdisable_test.go: a Button whose own onClicked turns its enabled binding false (a
// wizard's Next onto its last page) is disabled inside its own activation. That is legal there,
// by key and by pointer: nothing panics, and the pointer's capture is released.

const selfDisableDoc = `import tui 1.0
import demo 1.0
Window {
 Flex { direction: Tui.Horizontal
  Button { id: next; text: "next"; enabled: App.more; onClicked: App.last("last") }
  Button { id: other; text: "other"; onClicked: App.log("other") }
 }
}`

func runSelfDisable(t *testing.T) (*decltest.Screen, *recorder) {
	t.Helper()
	rec := &recorder{}
	var s *decltest.Screen
	s = decltest.Run(t, 40, 4, tuidecl.LayoutSource("main.qml", []byte(selfDisableDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.more": true}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler,
			"App.last": func(args []qml.SpecValue) error {
				if err := rec.handler(args); err != nil {
					return err
				}
				return s.Program.Set("App.more", false)
			}}))
	s.WaitForText(t, "other")
	return s, rec
}

// clickAt clicks the left button one cell into text.
func clickAt(t *testing.T, s *decltest.Screen, text string) {
	t.Helper()
	x, y := textAt(t, s, text)
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x + 1, Y: y},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x + 1, Y: y})
}

// textAt is the column and row where text starts on the screen.
func textAt(t *testing.T, s *decltest.Screen, text string) (x, y int) {
	t.Helper()
	for y, line := range strings.Split(s.String(), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return len([]rune(line[:i])), y
		}
	}
	t.Fatalf("%q is not on the screen:\n%s", text, s.String())
	return 0, 0
}

func nextEnabled(t *testing.T, s *decltest.Screen) bool {
	t.Helper()
	var on bool
	onScreenLoop(t, s, func() {
		c, ok := s.Program.Find("next")
		if !ok {
			t.Error("no next")
			return
		}
		on = c.(*widget.Button).Enabled()
	})
	return on
}

func TestAButtonsOwnClickMayDisableIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		drive func(t *testing.T, s *decltest.Screen)
	}{
		{"pointer", func(t *testing.T, s *decltest.Screen) {
			clickAt(t, s, "next")
		}},
		{"key", func(t *testing.T, s *decltest.Screen) {
			onScreenLoop(t, s, func() {
				c, _ := s.Program.Find("next")
				c.(*widget.Button).Context().RequestFocus()
			})
			s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, rec := runSelfDisable(t)
			tc.drive(t, s)
			s.WaitFor(t, "next disabled", func(string) bool { return !nextEnabled(t, s) })
			// a capture still held would take this click from the other button
			clickAt(t, s, "other")
			s.WaitFor(t, "other clicked", func(string) bool { return strings.Contains(logged(rec), "other") })
			if got := logged(rec); got != "last,other" {
				t.Errorf("logged %q, want last,other", got)
			}
		})
	}
}
