//go:build linux || darwin

package decl_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestDeclaredTerminalTabsKeepOwnedProcessesThroughHideAndPaletteChanges(t *testing.T) {
	layout := "import tui 1.0\nimport demo 1.0\nWindow {\n" +
		" palette.base: App.base; palette.text: \"white\"\n Text { text: \"page\" }\n" +
		" Drawer { id: d; modal: false; edge: Tui.Bottom; size: 60; length: 100\n TerminalTabs { id: terminals } } }"
	s := decltest.Run(t, 60, 16, tuidecl.LayoutSource("main.qml", []byte(layout)), tuidecl.Singleton("demo", "1.0", "App"), tuidecl.Sources(map[string]any{"App.base": "blue"}))
	var tabs *widget.TerminalTabs
	var a, b widget.TerminalTabID
	onScreenLoop(t, s, func() {
		c, ok := s.Program.Find("terminals")
		if !ok {
			t.Error("missing owned parent")
			return
		}
		tabs, ok = tuidecl.TerminalTabsOf(c)
		if !ok {
			t.Error("wrong parent type")
			return
		}
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
		// the red line comes in a later write than the first, as a program's output may: the cell
		// is read once it is on screen, not once the first line is
		a, _ = tabs.Add("first", widget.WithCommand("/bin/sh", "-c", "printf 'first output\\n'; sleep 0.2; printf '\\033[31mred words\\033[0m\\n'; read x; printf 'delivered:%s\\n' \"$x\"; read x"))
		b, _ = tabs.Add("second", widget.WithCommand("/bin/sh", "-c", "printf 'second output\\n'; read x"))
		if err := tabs.Start(a); err != nil {
			t.Error(err)
		}
		if err := tabs.Start(b); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "first output")
	waitBG(t, s, "first output", ansi(blue))
	s.WaitForText(t, "red words")
	if cellOf(t, s, "red words").Attrs.FG != ansi(red) {
		t.Error("explicit ANSI foreground was overridden by parent palette")
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "close"); err != nil {
			t.Error(err)
		}
		if err := tabs.SendText(a, "hidden task\n"); err != nil {
			t.Error(err)
		}
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "delivered:hidden task")
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.base", "red"); err != nil {
			t.Error(err)
		}
		if _, err := s.Program.Reload([]byte(layout)); err != nil {
			t.Error(err)
		}
		c, _ := s.Program.Find("terminals")
		if same, ok := tuidecl.TerminalTabsOf(c); !ok || same != tabs {
			t.Error("unchanged reload replaced runtime parent")
		}
	})
	waitBG(t, s, "first output", ansi(red))
	if cellOf(t, s, "red words").Attrs.FG != ansi(red) {
		t.Error("palette update changed explicit ANSI cells")
	}
	onScreenLoop(t, s, func() {
		if err := tabs.Select(b); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "second output")
	var result *widget.TerminalTabRemovalResult
	onScreenLoop(t, s, func() {
		var err error
		result, err = tabs.Remove(b)
		if err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := result.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	s.WaitForText(t, "delivered:hidden task")
	onScreenLoop(t, s, func() {
		if err := tabs.SendText(b, "stale"); !errors.Is(err, widget.ErrTerminalTabUnknown) {
			t.Errorf("removed ID accepted: %v", err)
		}
		if _, err := s.Program.Reload([]byte(strings.Replace(layout, "TerminalTabs { id: terminals }", "Text { id: replacement; text: \"replaced\" }", 1))); err != nil {
			t.Error(err)
		}
		if err := tabs.SendText(a, "destroyed"); !errors.Is(err, widget.ErrTerminalShutdown) {
			t.Errorf("destroyed owner accepted input: %v", err)
		}
	})
	if err := tabs.ShutdownWithResult().Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDeclaredTerminalTabsRejectStaticChildren(t *testing.T) {
	if _, err := mountDoc(t, "import tui 1.0\nTerminalTabs { Terminal {} }"); err == nil {
		t.Fatal("runtime container accepted a static child identity")
	}
}

// A document's TerminalTabs says what its body is with no tab (emptyText, bound and live), and
// takes the look every tab gets (minimumContrast, cursorColor, vimKeys).
func TestDeclaredTerminalTabsEmptyTextAndLook(t *testing.T) {
	layout := "import tui 1.0\nimport demo 1.0\nWindow {\n" +
		" TerminalTabs { id: agents; emptyText: App.empty; minimumContrast: 4.5; cursorColor: \"red\"; vimKeys: true; cellFont: \"Iosevka\" } }"
	s := decltest.Run(t, 60, 6, tuidecl.LayoutSource("main.qml", []byte(layout)), tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.empty": "No agent running"}))
	s.WaitForText(t, "No agent running")
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.empty", "Start one with +"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "Start one with +")
	if strings.Contains(s.String(), "No terminals") {
		t.Errorf("the default empty text showed:\n%s", s.String())
	}
}

// hostKeys is a comma-separated list of Shortcut sequences: each matches its key; "" is none; a
// sequence a Shortcut would refuse is refused.
func TestDeclaredHostKeys(t *testing.T) {
	match, err := tuidecl.HostKeysOfForTest(`"F11, Ctrl+Shift+M"`)
	if err != nil || match == nil {
		t.Fatalf("F11, Ctrl+Shift+M: %v", err)
	}
	if !match(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF11}) || match(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF10}) {
		t.Error("F11 matched wrongly")
	}
	if match, err := tuidecl.HostKeysOfForTest(`""`); err != nil || match != nil {
		t.Errorf(`"": %v %v`, match != nil, err)
	}
	if _, err := tuidecl.HostKeysOfForTest(`"Ctrl+Nope"`); err == nil {
		t.Error("a bad sequence was taken")
	}
}

// modeKey takes hostKeys' list: Ctrl+T matches its press, and a bad sequence is refused, named as
// modeKey's.
func TestDeclaredModeKey(t *testing.T) {
	match, err := tuidecl.ModeKeyOfForTest(`"Ctrl+T"`)
	if err != nil || match == nil || !match(tui.KeyEvent{Kind: tui.KeyPress, Code: 't', Mods: tui.ModCtrl}) {
		t.Fatalf("Ctrl+T: %v", err)
	}
	if _, err := tuidecl.ModeKeyOfForTest(`"Ctrl+Nope"`); err == nil || !strings.Contains(err.Error(), "modeKey") {
		t.Errorf("a bad sequence: %v", err)
	}
}

// TestADrawersTerminalTabsMenuMaximizesTheDrawer: a TerminalTabs in a maximizable Drawer, its strip
// menu's row calling the Drawer's toggleMaximize, as a host's Maximize does: a click on the row
// maximizes the Drawer over the page. The menu's layer traps the input scope while it is open, and
// the Drawer refuses a window operation from outside it, so the row runs once the menu has gone.
func TestADrawersTerminalTabsMenuMaximizesTheDrawer(t *testing.T) {
	layout := "import tui 1.0\nWindow {\n Text { text: \"page\" }\n" +
		" Drawer { id: d; modal: false; maximizable: true; edge: Tui.Bottom; size: 50; length: 100\n TerminalTabs { id: terminals } } }"
	s := decltest.Run(t, 60, 16, tuidecl.LayoutSource("main.qml", []byte(layout)))
	var errs []error // on the loop only
	onScreenLoop(t, s, func() {
		c, _ := s.Program.Find("terminals")
		tabs, ok := tuidecl.TerminalTabsOf(c)
		if !ok {
			t.Error("no TerminalTabs")
			return
		}
		tabs.SetMenu(func(widget.TerminalTabID) []widget.MenuItemModel {
			return []widget.MenuItemModel{widget.NewCommand("max", "Maximize", widget.TerminalTabsMenuAction{ID: "max",
				Run: func(widget.TerminalTabID) { errs = append(errs, s.Program.Call("d", "toggleMaximize")) }})}
		})
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "No terminals")
	x, y := textAt(t, s, "No terminals")
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: x + 1, Y: y},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseRight, X: x + 1, Y: y})
	s.WaitForText(t, "Maximize")
	mx, my := textAt(t, s, "Maximize")
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: mx + 1, Y: my},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: mx + 1, Y: my})
	s.WaitFor(t, "the row ran", func(string) bool {
		n := 0
		onScreenLoop(t, s, func() { n = len(errs) })
		return n > 0
	})
	onScreenLoop(t, s, func() {
		if len(errs) != 1 || errs[0] != nil {
			t.Errorf("the menu's toggleMaximize: %v", errs)
		}
	})
	s.WaitFor(t, "the Drawer over the page", func(sc string) bool { return !strings.Contains(sc, "page") })
}
