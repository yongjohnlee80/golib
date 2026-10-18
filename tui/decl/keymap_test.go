package decl_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

var (
	demoSave  = keymap.Command{ID: "demo.save", Scope: keymap.Global, Keys: tuikey.MustParse("Ctrl+S")}
	demoAdd   = keymap.Command{ID: "demo.todos.add", Scope: "demo.todos", Keys: tuikey.MustParse("A")}
	demoFiles = func(scheme string) fstest.MapFS {
		fs := fstest.MapFS{}
		if scheme != "" {
			fs["keymap.xml"] = &fstest.MapFile{Data: []byte(scheme)}
		}
		return fs
	}
)

const keysHead = "import tui 1.0\nimport demo 1.0\n"

// runKeys runs layout with the demo commands and a scheme, the status line showing what fired.
func runKeys(t *testing.T, layout, scheme string, extra ...tuidecl.ProgramOption) (*decltest.Screen, **tuidecl.Program) {
	t.Helper()
	var p *tuidecl.Program
	set := func(v string) func() error { return func() error { return p.Set("App.status", v) } }
	opts := append([]tuidecl.ProgramOption{
		tuidecl.LayoutSource("main.qml", []byte(keysHead+layout)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.status": "ready"}),
		tuidecl.Commands(map[string]func() error{"App.saved": set("saved"), "App.added": set("added"), "App.fixed": set("fixed")}),
		tuidecl.KeyCommands(demoSave, demoAdd),
		tuidecl.Keymap(demoFiles(scheme), "keymap.xml"),
		tuidecl.Types(controls.Types()...),
	}, extra...)
	s := decltest.RunWith(t, 50, 6, func(prog *tuidecl.Program) error { p = prog; return nil }, opts...)
	return s, &p
}

const keyIdWindow = `Window {
    Shortcut { sequence: keyId("demo.save"); onActivated: App.saved() }
    Flex { direction: Tui.Vertical
        Text { text: App.status }
        FocusScope { id: todos; keyScope: "demo.todos"
            Shortcut { sequence: keyId("demo.todos.add"); onActivated: App.added() }
            TextField { id: field }
        }
    }
}`

func ctrl(r rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: r, Mods: tui.ModCtrl} }

// A keyId Shortcut fires on its command's keys; a scheme moves them and the old key stops.
func TestAKeyIdShortcutFollowsTheScheme(t *testing.T) {
	s, _ := runKeys(t, keyIdWindow, "")
	s.WaitForText(t, "ready")
	s.Keys(t, ctrl('s'))
	s.WaitForText(t, "saved")

	s, _ = runKeys(t, keyIdWindow, `<mapping><shortcut id="demo.save"><key value="Ctrl+P"/></shortcut></mapping>`)
	s.WaitForText(t, "ready")
	s.Keys(t, ctrl('s'))
	settleKeys()
	if strings.Contains(s.String(), "saved") {
		t.Fatal("Ctrl+S still saves after the scheme moved demo.save")
	}
	s.Keys(t, ctrl('p'))
	s.WaitForText(t, "saved")
}

// A keyId Shortcut out of its scope's holder is refused, with its position, before anything runs.
func TestKeyIdShortcutsAreRefusedOutsideTheirScope(t *testing.T) {
	for _, c := range []struct {
		name, layout, want string
	}{
		{"undeclared", `Window { Shortcut { sequence: keyId("demo.nothing"); onActivated: App.saved() }
            Text { text: "x" } }`, `keyId("demo.nothing") names no declared key command (at main.qml:3`},
		{"wrong scope", `Window { Shortcut { sequence: keyId("demo.todos.add"); onActivated: App.added() }
            Text { text: "x" } }`, `is a demo.todos command, in a Window whose scope is global`},
		{"no keyScope", `Window { FocusScope { Shortcut { sequence: keyId("demo.todos.add"); onActivated: App.added() }
            Text { text: "x" } } }`, `in a FocusScope with no keyScope: give it keyScope: "demo.todos"`},
	} {
		_, err := tuidecl.NewProgram(
			tuidecl.LayoutSource("main.qml", []byte(keysHead+c.layout)),
			tuidecl.Singleton("demo", "1.0", "App"),
			tuidecl.Commands(map[string]func() error{"App.saved": func() error { return nil }, "App.added": func() error { return nil }}),
			tuidecl.KeyCommands(demoSave, demoAdd))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

// A scheme NewProgram cannot build fails it, naming the file and line.
func TestABadSchemeFailsNewProgram(t *testing.T) {
	_, err := tuidecl.NewProgram(
		tuidecl.LayoutSource("main.qml", []byte(keysHead+keyIdWindow)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.status": "ready"}),
		tuidecl.Commands(map[string]func() error{"App.saved": func() error { return nil }, "App.added": func() error { return nil }}),
		tuidecl.KeyCommands(demoSave, demoAdd),
		tuidecl.Keymap(demoFiles("<mapping>\n<shortcut id=\"demo.save\"><key value=\"Ctrl+Foo\"/></shortcut></mapping>"), "keymap.xml"))
	if err == nil || !strings.Contains(err.Error(), "keymap.xml, line 2") {
		t.Fatalf("err = %v, want the scheme and line named", err)
	}
}

// A MenuItem's keyId shortcut shows the command's current key.
func TestAMenuItemShowsItsCommandsKey(t *testing.T) {
	layout := `Window {
    MenuBar { Menu { title: "&File"; MenuItem { text: "&Save"; shortcut: keyId("demo.save"); onTriggered: App.saved() } } }
    Text { text: App.status }
}`
	s, _ := runKeys(t, layout, `<mapping><shortcut id="demo.save"><key value="Ctrl+P"/></shortcut></mapping>`)
	s.WaitForText(t, "ready")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF10}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitForText(t, "Ctrl+P")
}

// Program.KeyScope and KeyBindings answer by id, in routing order: the field's own scope, then the
// FocusScope's, then the Window's, then traversal.
func TestProgramKeyScopeAndBindingsByID(t *testing.T) {
	s, pp := runKeys(t, keyIdWindow, "")
	s.WaitForText(t, "ready")
	p := *pp
	var scope, scopeOfTodos keymap.Scope
	var bs []keymap.Binding
	var err error
	done := make(chan struct{})
	p.App().Update(func() {
		defer close(done)
		scope, _ = p.KeyScope("field")
		scopeOfTodos, _ = p.KeyScope("todos")
		bs, err = p.KeyBindings("field")
	})
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if scope != "tui.input" || scopeOfTodos != "demo.todos" {
		t.Errorf("scopes: field %q, todos %q", scope, scopeOfTodos)
	}
	var order []keymap.Scope
	for _, b := range bs {
		if len(order) == 0 || order[len(order)-1] != b.Scope {
			order = append(order, b.Scope)
		}
	}
	want := []keymap.Scope{"tui.input", "demo.todos", keymap.Global, keymap.App}
	if strings.Join(scopeStrings(order), ",") != strings.Join(scopeStrings(want), ",") {
		t.Errorf("routing order %v, want %v", order, want)
	}
}

func scopeStrings(ss []keymap.Scope) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = string(s)
	}
	return out
}

// CheckKeymap reports what keeps a key from working, and keeps Johno's legal shadowing a warning.
func TestCheckKeymap(t *testing.T) {
	check := func(layout string, cmds ...keymap.Command) ([]keymap.Problem, error) {
		return tuidecl.CheckKeymap(
			tuidecl.LayoutSource("main.qml", []byte(keysHead+layout)),
			tuidecl.Singleton("demo", "1.0", "App"),
			tuidecl.Commands(map[string]func() error{"App.saved": func() error { return nil }, "App.added": func() error { return nil }, "App.fixed": func() error { return nil }}),
			tuidecl.KeyCommands(cmds...))
	}
	// a fixed sequence on a command's key, in the command's scope
	_, err := check(`Window { Shortcut { sequence: "Ctrl+S"; onActivated: App.fixed() }
        Shortcut { sequence: keyId("demo.save"); onActivated: App.saved() }
        Text { text: "x" } }`, demoSave)
	if err == nil || !strings.Contains(err.Error(), "the fixed sequence Ctrl+S overlaps Ctrl+S of \"demo.save\"") {
		t.Errorf("fixed key: %v", err)
	}
	// a Global command on Alt and a menu's mnemonic
	altF := keymap.Command{ID: "demo.find", Scope: keymap.Global, Keys: tuikey.MustParse("Alt+F")}
	_, err = check(`Window { MenuBar { Menu { title: "&File"; MenuItem { text: "Quit" } } }
        Text { text: "x" } }`, altF)
	if err == nil || !strings.Contains(err.Error(), "takes Alt+F, the menu") {
		t.Errorf("menu mnemonic: %v", err)
	}
	// legal shadowing: a warning, never an error
	enterGlobal := keymap.Command{ID: "demo.go", Scope: keymap.Global, Keys: tuikey.MustParse("Enter")}
	warnings, err := check(`Window { Text { text: "x" } }`, enterGlobal)
	if err != nil {
		t.Fatalf("shadowing refused: %v", err)
	}
	if len(warnings) == 0 {
		t.Error("Enter on Global shares a key with the List's activate, and no warning says so")
	}
	// golib's own defaults contest keys by design (Tab completes in a TextField and moves focus): not
	// the program's warning
	warnings, err = check(`Window { Text { text: "x" } }`)
	if err != nil || len(warnings) != 0 {
		t.Errorf("a program with no keys of its own: warnings %v, err %v", warnings, err)
	}
	// a scheme that moves a golib key onto another's is the program's doing, and warned of
	warnings, err = tuidecl.CheckKeymap(
		tuidecl.LayoutSource("main.qml", []byte(keysHead+`Window { Text { text: "x" } }`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Keymap(demoFiles(`<mapping><shortcut id="tui.list.down"><key value="Tab"/></shortcut></mapping>`), "keymap.xml"))
	if err != nil || !slices.ContainsFunc(warnings, func(p keymap.Problem) bool {
		return p.ID == "tui.list.down" || p.With == "tui.list.down"
	}) {
		t.Errorf("a scheme moving tui.list.down onto Tab: warnings %v, err %v", warnings, err)
	}
	// a scheme Build refuses is an error
	_, err = tuidecl.CheckKeymap(
		tuidecl.LayoutSource("main.qml", []byte(keysHead+`Window { Text { text: "x" } }`)),
		tuidecl.Keymap(demoFiles(`<mapping><shortcut id="nobody"><key value="X"/></shortcut></mapping>`), "keymap.xml"))
	var e *keymap.Error
	if !errors.As(err, &e) {
		t.Errorf("an unknown id: %v, want *keymap.Error", err)
	}
}

// settleKeys lets a key that should do nothing have had its chance to.
func settleKeys() { settle() }

// Under HotReload the key-mapping scheme is followed: a file created after start applies, a broken one
// is refused and the old keys stay, a layout whose keyId the candidate map refuses is refused as a
// whole with the screen unchanged, and deleting the file returns the defaults.
func TestHotReloadFollowsTheKeymap(t *testing.T) {
	layout := func(id string) string {
		return hotHead + `Window { Shortcut { sequence: keyId("` + id + `"); onActivated: App.saved() }
    Flex { direction: Tui.Vertical
        Text { text: App.status }
        Text { text: App.tick } } }` // a provider's source: the reload's check must still mount
	}
	d := newHotDir(t, layout("demo.save"))
	var p *tuidecl.Program
	n := 0
	s := d.runWith(func(prog *tuidecl.Program) error { p = prog; return nil },
		tuidecl.KeyCommands(demoSave),
		tuidecl.Keymap(os.DirFS(d.root), "keymap.xml"),
		tuidecl.Providers(tickProvider{}),
		tuidecl.Commands(map[string]func() error{"App.saved": func() error { n++; return p.Set("App.status", fmt.Sprintf("saved %d", n)) }}))
	s.WaitForText(t, "initial")
	s.Keys(t, ctrl('s'))
	s.WaitForText(t, "saved 1")

	reloads, _ := d.counts()
	d.write("keymap.xml", `<mapping><shortcut id="demo.save"><key value="Ctrl+P"/></shortcut></mapping>`)
	s.WaitFor(t, "the created scheme applied", func(string) bool { r, _ := d.counts(); return r > reloads })
	s.Keys(t, ctrl('s'))
	settle()
	if strings.Contains(s.String(), "saved 2") {
		t.Fatal("Ctrl+S still saves after the scheme moved it")
	}
	s.Keys(t, ctrl('p'))
	s.WaitForText(t, "saved 2")

	_, refused := d.counts()
	d.write("keymap.xml", `<mapping><shortcut id="demo.save"><key valu="Ctrl+P"/></shortcut></mapping>`)
	s.WaitFor(t, "the broken scheme refused", func(string) bool { _, r := d.counts(); return r > refused })
	if d.mu.Lock(); !strings.Contains(d.refusals[refused].Error(), "keymap.xml") {
		t.Errorf("the refusal does not name the scheme: %v", d.refusals[refused])
	}
	d.mu.Unlock()
	s.Keys(t, ctrl('p'))
	s.WaitForText(t, "saved 3")

	d.write("keymap.xml", `<mapping><shortcut id="demo.save"><key value="Ctrl+P"/></shortcut></mapping>`)
	reloads, refused = d.counts()
	s.WaitFor(t, "the fixed scheme applied", func(string) bool { r, _ := d.counts(); return r > reloads })
	d.write("main.qml", layout("demo.nothing"))
	s.WaitFor(t, "the layout's undeclared keyId refused", func(string) bool { _, r := d.counts(); return r > refused })
	if d.mu.Lock(); !strings.Contains(d.refusals[refused].Error(), `keyId("demo.nothing")`) {
		t.Errorf("the refusal does not name the keyId: %v", d.refusals[refused])
	}
	d.mu.Unlock()
	s.Keys(t, ctrl('p'))
	s.WaitForText(t, "saved 4")

	reloads, _ = d.counts()
	d.write("main.qml", layout("demo.save"))
	s.WaitFor(t, "the good layout applied", func(string) bool { r, _ := d.counts(); return r > reloads })
	reloads, _ = d.counts()
	if err := os.Remove(filepath.Join(d.root, "keymap.xml")); err != nil {
		t.Fatal(err)
	}
	s.WaitFor(t, "the deleted scheme followed", func(string) bool { r, _ := d.counts(); return r > reloads })
	s.Keys(t, ctrl('s'))
	s.WaitForText(t, "saved 5")
}

// tickProvider provides App.tick once, as a host's clock would.
type tickProvider struct{}

func (tickProvider) Subscribe(fn func(decl.Update)) ([]string, func() error, error) {
	fn(decl.Update{Version: 1, Values: map[string]qml.SpecValue{"App.tick": {Kind: qml.SpecValueString, Raw: "tick 0"}}})
	return []string{"App.tick"}, func() error { return nil }, nil
}

// Every item's keyScope reads by id in a handler: a widget's own scope, a FocusScope's declared one,
// and for an item with none of its own the nearest enclosing scope (the Window's, Global).
func TestKeyScopeReadsByIDInAHandler(t *testing.T) {
	var prog **tuidecl.Program
	show := func(args []qml.SpecValue) error {
		parts := make([]string, len(args))
		for i := range args {
			s, err := tuidecl.Arg(args, i)
			if err != nil {
				return err
			}
			parts[i] = s
		}
		return (*prog).Set("App.status", strings.Join(parts, "|"))
	}
	s, pp := runKeys(t, `Window {
    Shortcut { sequence: "F5"; onActivated: App.show(field.keyScope, todos.keyScope, status.keyScope) }
    Flex { direction: Tui.Vertical
        Text { id: status; text: App.status }
        FocusScope { id: todos; keyScope: "demo.todos"
            TextField { id: field }
        }
    }
}`, "", tuidecl.Handlers(map[string]decl.HandlerFunc{"App.show": show}))
	prog = pp
	s.WaitForText(t, "ready")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF5})
	s.WaitForText(t, "tui.input|demo.todos|global")
}

// A key declaration the parser cannot read is refused with its position, before anything runs.
func TestMalformedKeyDeclarationsAreRefused(t *testing.T) {
	for _, c := range []struct {
		name, layout, want string
	}{
		{"an unknown key name", `Window { Shortcut { sequence: "Ctrl+Bogus"; onActivated: App.saved() }
            Text { text: "x" } }`, `(at main.qml:3`},
		{"a FocusScope with two contents", `Window { FocusScope { keyScope: "demo.todos"
            Text { text: "a" }
            Text { text: "b" } } }`, `a FocusScope holds exactly one content item besides its Shortcuts, got 2`},
	} {
		_, err := tuidecl.NewProgram(
			tuidecl.LayoutSource("main.qml", []byte(keysHead+c.layout)),
			tuidecl.Singleton("demo", "1.0", "App"),
			tuidecl.Commands(map[string]func() error{"App.saved": func() error { return nil }}),
			tuidecl.KeyCommands(demoSave, demoAdd))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

// A fixed sequence QML has always taken, Shift and a character it types that is no letter
// ("Shift+*"), still mounts and is that character: * typed bare, or with the Shift that typed it.
func TestAShiftedSymbolSequenceIsTheSymbol(t *testing.T) {
	for _, ev := range []tui.KeyEvent{
		{Kind: tui.KeyPress, Code: '*', Text: "*"},
		{Kind: tui.KeyPress, Code: '8', Mods: tui.ModShift, Text: "*"},
	} {
		s, _ := runKeys(t, `Window {
    Shortcut { sequence: "Shift+*"; onActivated: App.fixed() }
    Text { text: App.status }
}`, "")
		s.WaitForText(t, "ready")
		s.Keys(t, ev)
		s.WaitForText(t, "fixed")
	}
}
