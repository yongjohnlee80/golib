package widget_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// stopScopes is the routing order of bs as the scopes of its stops, adjacent repeats folded.
func stopScopes(bs []keymap.Binding) []keymap.Scope {
	var out []keymap.Scope
	for _, b := range bs {
		if len(out) == 0 || out[len(out)-1] != b.Scope {
			out = append(out, b.Scope)
		}
	}
	return out
}

func bindingOf(bs []keymap.Binding, id string) (keymap.Binding, bool) {
	for _, b := range bs {
		if b.Command.ID == id {
			return b, true
		}
	}
	return keymap.Binding{}, false
}

// What reaches a Button in an open Modal, by the route: the Button's activation as the scheme binds
// it, then the Modal's commands, then what the host said its modal keys consult, then traversal. The
// button the trap shuts out gets ErrConfined.
func TestTheKeysReachingAButtonInAModal(t *testing.T) {
	m := mustKeymap(t, `<mapping><shortcut id="tui.button.activate"><key value="Ctrl+Y"/></shortcut></mapping>`)
	copyCmd := keymap.Command{ID: "host.copy", Scope: "host", Keys: tuikey.MustParse("Y")}
	ok := widget.NewButton("OK")
	md := widget.NewModal(widget.NewText("Body"), widget.WithButtons(ok),
		widget.WithModalKeyBinder(func(km *keymap.Map) []keymap.Binding { return tui.BindingsOf(km, copyCmd) }))
	h, host, base := modalFixture(t, md, 40, 12)
	defer h.stop()
	h.onLoop(func() { h.app.ApplyKeymap(m) })
	openOn(t, h, md, host)

	var bs []keymap.Binding
	var scope, modalScope keymap.Scope
	var err, confined error
	h.onLoop(func() {
		bs, err = h.app.KeyBindings(ok)
		scope, modalScope = h.app.KeyScopeOf(ok), h.app.KeyScopeOf(md)
		_, confined = h.app.KeyBindings(base)
	})
	if err != nil {
		t.Fatal(err)
	}
	if scope != widget.ButtonScope || modalScope != widget.ModalScope {
		t.Errorf("scopes: button %q, modal %q", scope, modalScope)
	}
	want := []keymap.Scope{widget.ButtonScope, widget.ModalScope, "host", keymap.App}
	if got := stopScopes(bs); !slices.Equal(got, want) {
		t.Errorf("routing order %v, want %v", got, want)
	}
	act, _ := bindingOf(bs, widget.ButtonActivate.ID)
	if !slices.Equal(act.Keys, tuikey.MustParse("Ctrl+Y")) || act.Source.Kind != keymap.FromScheme || len(act.Reaches) == 0 {
		t.Errorf("the button's activation: keys %v, source %v, reaches %v", act.Keys, act.Source.Kind, act.Reaches)
	}
	if b, found := bindingOf(bs, copyCmd.ID); !found || !slices.Equal(b.Keys, copyCmd.Keys) {
		t.Errorf("the host's modal key: %+v, listed %v", b, found)
	}
	if !errors.Is(confined, tui.ErrConfined) {
		t.Errorf("the button behind the trap: %v, want ErrConfined", confined)
	}
}

// A Menu's stop lists its commands, in the order it consults them.
func TestTheKeysReachingAMenu(t *testing.T) {
	menu := widget.NewMenu()
	host := widget.NewOverlayHost(menu)
	h := startApp(t, host, 40, 8)
	defer h.stop()
	var bs []keymap.Binding
	var scope keymap.Scope
	var err error
	h.onLoop(func() { bs, err = h.app.KeyBindings(menu); scope = h.app.KeyScopeOf(menu) })
	if err != nil {
		t.Fatal(err)
	}
	if scope != widget.MenuScope {
		t.Errorf("scope %q", scope)
	}
	var ids []string
	for _, b := range bs {
		if b.Scope == widget.MenuScope {
			ids = append(ids, b.Command.ID)
		}
	}
	want := []string{widget.MenuLeft.ID, widget.MenuRight.ID, widget.MenuUp.ID, widget.MenuDown.ID,
		widget.MenuVimLeft.ID, widget.MenuVimDown.ID, widget.MenuVimUp.ID, widget.MenuVimRight.ID,
		widget.MenuActivate.ID, widget.MenuClose.ID}
	if !slices.Equal(ids, want) {
		t.Errorf("the menu's commands %v, want %v", ids, want)
	}
}

// An Editor's stop is its context menu when it has one, then the table of the mode it is in, in that
// mode's scope; both follow the mode.
func TestTheKeysReachingAnEditorFollowItsMode(t *testing.T) {
	for _, menu := range []bool{false, true} {
		var opts []widget.EditorOption
		if menu {
			opts = append(opts, widget.WithContextMenu(nil))
		}
		h, e, sh := mountedEditor(t, nil, opts...)
		read := func() (keymap.Scope, []keymap.Binding) {
			var scope keymap.Scope
			var bs []keymap.Binding
			var err error
			h.onLoop(func() { scope = h.app.KeyScopeOf(e); bs, err = h.app.KeyBindings(e) })
			if err != nil {
				t.Fatal(err)
			}
			return scope, bs
		}
		normal := keymap.Scope("tui.editor.vim.normal")
		scope, bs := read()
		if scope != normal {
			t.Fatalf("menu %v: scope %q, want %q", menu, scope, normal)
		}
		_, listed := bindingOf(bs, widget.EditorContextMenu.ID)
		if listed != menu {
			t.Errorf("menu %v: the context menu listed %v", menu, listed)
		}
		if _, ok := bindingOf(bs, string(normal)+".undo"); !ok {
			t.Errorf("menu %v: Normal's undo is not listed", menu)
		}
		h.inject(ch('i'))
		h.barrier(sh)
		insert := keymap.Scope("tui.editor.vim.insert")
		scope, bs = read()
		if scope != insert {
			t.Errorf("menu %v: after i, scope %q, want %q", menu, scope, insert)
		}
		if _, ok := bindingOf(bs, string(normal)+".undo"); ok {
			t.Errorf("menu %v: Normal's undo still listed in Insert", menu)
		}
	}
}

// IsContextMenuKey stays the default context-menu keys for a host with no command of its own: Shift+F10
// and an unmodified Menu key, on a press.
func TestIsContextMenuKeyIsTheDefaultKeys(t *testing.T) {
	for _, c := range []struct {
		name string
		k    tui.KeyEvent
		want bool
	}{
		{"Shift+F10", tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF10, Mods: tui.ModShift}, true},
		{"Menu", tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyMenu}, true},
		{"Ctrl+Menu", tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyMenu, Mods: tui.ModCtrl}, false},
		{"F10", tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF10}, false},
		{"Menu released", tui.KeyEvent{Kind: tui.KeyRelease, Code: tui.KeyMenu}, false},
	} {
		if got := widget.IsContextMenuKey(c.k); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
