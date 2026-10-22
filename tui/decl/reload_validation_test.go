package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Program.Reload is held to what a hot reload is: a keyId Shortcut naming no declared key command
// is refused, by name, before the tree is touched, so the screen stays as it was. Before,
// Program.Reload only parsed and reconciled, and took what a hot reload refused.
func TestProgramReloadRefusesAnUndeclaredKeyId(t *testing.T) {
	s, _ := runKeys(t, keyIdWindow, "")
	s.WaitForText(t, "ready")
	bad := keysHead + `Window { Shortcut { sequence: keyId("demo.nothing"); onActivated: App.saved() }
    Text { text: "replaced" } }`
	var err error
	onScreenLoop(t, s, func() { _, err = s.Program.Reload([]byte(bad)) })
	if err == nil || !strings.Contains(err.Error(), `keyId("demo.nothing")`) {
		t.Fatalf("Reload with an undeclared keyId: %v, want it refused, naming the keyId", err)
	}
	if strings.Contains(s.String(), "replaced") {
		t.Errorf("the refused layout reached the screen:\n%s", s.String())
	}
}

const reloadHead = "import tui 1.0\nimport demo 1.0\n"

func reloadLayout(body string) string {
	return reloadHead + "Window { Flex { direction: Tui.Vertical\n" + body + "\n} }"
}

// After a Reload that fails part-way (a colour refused while applying), the next good Reload
// remounts the screen, and the host's source keeps its current value, as a hot reload does.
// Before, Program.Reload reconciled the failed tree, which only Destroy may touch.
func TestProgramReloadRemountsAfterAPartWayFailure(t *testing.T) {
	s := decltest.Run(t, 40, 6,
		tuidecl.LayoutSource("main.qml", []byte(reloadLayout("Text { text: App.status }\nText { text: \"x\"; palette.window: \"blue\" }"))),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.status": "initial"}))
	s.WaitForText(t, "initial")
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.status", "moved"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "moved")

	var err error
	onScreenLoop(t, s, func() {
		_, err = s.Program.Reload([]byte(reloadLayout("Text { text: App.status }\nText { text: \"x\"; palette.window: \"bleu\" }")))
	})
	if err == nil {
		t.Fatal("the bad colour was accepted")
	}
	failed := false
	onScreenLoop(t, s, func() { failed = s.Program.Tree().Failed() })
	if !failed {
		t.Fatal("the colour was refused before the tree was touched; this cell needs a part-way failure")
	}

	var res decl.Result
	onScreenLoop(t, s, func() {
		res, err = s.Program.Reload([]byte(reloadLayout("Text { text: App.status }\nText { text: \"recovered\" }")))
	})
	if err != nil || !res.RootReplaced {
		t.Fatalf("the good Reload after a part-way failure: %+v, %v; want a remount (root replaced)", res, err)
	}
	s.WaitForText(t, "recovered")
	if !strings.Contains(s.String(), "moved") {
		t.Errorf("the remount lost the host's source value:\n%s", s.String())
	}
}

// A program with no sources checks a reload's keyIds too. Before, the check froze the live tree's
// sources into a provider for its mount, a provider of none declared no names, the engine refused it,
// and the check, taking a mount that failed for the reconcile's to report, passed every layout.
func TestAReloadWithoutSourcesStillChecksItsKeyIds(t *testing.T) {
	window := func(id string) []byte {
		return []byte(keysHead + `Window { Shortcut { sequence: keyId("` + id + `"); onActivated: App.saved() }
    Text { text: "ready" } }`)
	}
	s := decltest.Run(t, 30, 4, tuidecl.LayoutSource("main.qml", window("demo.save")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Commands(map[string]func() error{"App.saved": func() error { return nil }}),
		tuidecl.KeyCommands(demoSave))
	s.WaitForText(t, "ready")
	var err error
	onScreenLoop(t, s, func() { _, err = s.Program.Reload(window("demo.nothing")) })
	if err == nil || !strings.Contains(err.Error(), `keyId("demo.nothing") names no declared key command`) {
		t.Errorf("a reload with an undeclared keyId, in a program with no sources: %v; want refused", err)
	}
}

// A reload's keyId check reads each holder's keyScope as the document declares it, a Drawer's and a
// FocusScope's: a keyId in the scope it names is taken, one in another scope or in a holder with none
// is refused, naming why. The check mounts keys-only (keycheck.go), so this pins what it still sees.
func TestAReloadChecksAKeyIdAgainstItsHoldersScope(t *testing.T) {
	s, _ := runKeys(t, keyIdWindow, "")
	s.WaitForText(t, "ready")
	todo := `Shortcut { sequence: keyId("demo.todos.add"); onActivated: App.added() }`
	for _, c := range []struct {
		name, holder, want string // want "" is taken
	}{
		{"a FocusScope in its scope", `FocusScope { keyScope: "demo.todos"; ` + todo + ` Text { text: "in" } }`, ""},
		{"a Drawer in its scope", `Drawer { keyScope: "demo.todos"; ` + todo + ` Text { text: "in" } }`, ""},
		{"a FocusScope in another", `FocusScope { keyScope: "demo.other"; ` + todo + ` Text { text: "in" } }`,
			"is a demo.todos command, in a FocusScope whose scope is demo.other"},
		{"a FocusScope with none", `FocusScope { ` + todo + ` Text { text: "in" } }`,
			`in a FocusScope with no keyScope: give it keyScope: "demo.todos"`},
	} {
		layout := keysHead + "Window { Flex { direction: Tui.Vertical\n Text { text: App.status }\n " + c.holder + " } }"
		var err error
		onScreenLoop(t, s, func() { _, err = s.Program.Reload([]byte(layout)) })
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v; want taken", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: %v; want refused, %q", c.name, err, c.want)
		}
	}
}

// The reload's keyId check refuses a bad keyId in a layout that places a host's Instance: a widget
// that can be in the tree once. The check passes a layout its own mount cannot build (the reconcile
// reports that), so a mount that failed on the check's own account switched the check off unseen.
// It did for a source-less program (the frozen provider of none). It would for an Instance, which a
// full second mount builds a second time, but the keys-only mount builds no Instance. This keeps a
// known-bad keyId refused there.
func TestAReloadWithAnInstanceStillChecksItsKeyIds(t *testing.T) {
	window := func(id string) []byte {
		return []byte(keysHead + `Window { Shortcut { sequence: keyId("` + id + `"); onActivated: App.saved() }
    Flex { direction: Tui.Vertical
        Text { text: App.status }
        Preview { } } }`)
	}
	s := decltest.Run(t, 30, 4, tuidecl.LayoutSource("main.qml", window("demo.save")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.status": "ready"}),
		tuidecl.Commands(map[string]func() error{"App.saved": func() error { return nil }}),
		tuidecl.KeyCommands(demoSave),
		tuidecl.Types(tuidecl.Instance("Preview", widget.NewText("preview"))))
	s.WaitForText(t, "preview")
	var err error
	onScreenLoop(t, s, func() { _, err = s.Program.Reload(window("demo.nothing")) })
	if err == nil || !strings.Contains(err.Error(), `keyId("demo.nothing") names no declared key command`) {
		t.Errorf("a reload with an undeclared keyId beside an Instance: %v; want refused", err)
	}
}
