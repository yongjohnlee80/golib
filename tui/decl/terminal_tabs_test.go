//go:build linux || darwin

package decl_test

import (
	"context"
	"errors"
	"testing"
	"time"

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
		a, _ = tabs.Add("first", widget.WithCommand("/bin/sh", "-c", "printf 'first output\\n'; read x; printf 'delivered:%s\\n' \"$x\"; read x"))
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
	})
}

func TestDeclaredTerminalTabsRejectStaticChildren(t *testing.T) {
	if _, err := mountDoc(t, "import tui 1.0\nTerminalTabs { Terminal {} }"); err == nil {
		t.Fatal("runtime container accepted a static child identity")
	}
}
