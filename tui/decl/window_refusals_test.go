package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// A Window refuses a child it cannot dock, naming why: a Dock.edge that is no edge, and a menu row
// written directly in it rather than in a MenuBar.
func TestAWindowRefusesAChildItCannotDock(t *testing.T) {
	for doc, want := range map[string]string{
		`Window { Text { text: "a"; Dock.edge: "sideways" } }`: "Dock.edge: must be",
		`Window { MenuItem { text: "&Open" } }`:                "a MenuItem belongs inside a MenuBar, not directly in a Window",
	} {
		_, err := tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\n"+doc)),
			tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(20, 4))))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v; want refused, %q", doc, err, want)
		}
	}
}
