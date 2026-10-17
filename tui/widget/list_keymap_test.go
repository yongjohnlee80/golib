package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// mustKeymap builds golib's commands with a scheme over them.
func mustKeymap(t *testing.T, xml string) *keymap.Map {
	t.Helper()
	s, err := keymap.ParseScheme(strings.NewReader(xml), "keymap.xml")
	if err != nil {
		t.Fatal(err)
	}
	m, err := keymap.Build(widget.KeyCommands(), s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// A scheme rebinding a List command reaches the mounted List: the new key moves it, the old one no
// longer does, and an unbound command does nothing.
func TestAListFollowsItsKeymap(t *testing.T) {
	m := mustKeymap(t, `<mapping>
<shortcut id="tui.list.down"><key value="Ctrl+N"/></shortcut>
<shortcut id="tui.list.bottom"><key value=""/></shortcut>
</mapping>`)
	l := widget.NewList(widget.WithSource(widget.SliceSource([]string{"alpha", "beta", "gamma"}), func(s string) string { return s }))
	sh := newShell(l)
	h := startAppOpts(t, sh, 20, 5, tui.WithKeymap(m))
	h.inject(tab())
	h.barrier(sh)
	sels := record[widget.SelectionChangedEvent](h)
	cursor := func() int {
		ev, ok := sels.last()
		if !ok {
			return 0
		}
		return ev.Index
	}
	h.inject(tui.KeyEvent{Code: 'n', Mods: tui.ModCtrl})
	h.barrier(sh)
	if got := cursor(); got != 1 {
		t.Fatalf("Ctrl+N: cursor %d, want 1", got)
	}
	h.inject(tui.KeyEvent{Code: 'j'}, tui.KeyEvent{Code: tui.KeyDown})
	h.barrier(sh)
	if got := cursor(); got != 1 {
		t.Fatalf("j and Down after the scheme replaced them: cursor %d, want 1", got)
	}
	h.inject(tui.KeyEvent{Code: 'G'})
	h.barrier(sh)
	if got := cursor(); got != 1 {
		t.Fatalf("unbound G moved the cursor to %d", got)
	}
	h.inject(tui.KeyEvent{Code: tui.KeyEnd})
	h.barrier(sh)
	if got := cursor(); got != 2 {
		t.Fatalf("End, still bound: cursor %d, want 2", got)
	}
}
