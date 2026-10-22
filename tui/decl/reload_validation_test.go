package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
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
