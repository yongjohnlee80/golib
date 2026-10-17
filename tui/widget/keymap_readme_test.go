package widget_test

import (
	"os"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// Every golib command is in tui/keymap's README, where a user looks a key's id up; the Editor's, which
// its tables derive, by their pattern.
func TestEveryCommandIsInTheKeymapReadme(t *testing.T) {
	b, err := os.ReadFile("../keymap/README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)
	if !strings.Contains(readme, "`tui.editor.<keyset>.<class>.<action>`") {
		t.Error("the README does not give the Editor's command pattern")
	}
	for _, c := range widget.KeyCommands() {
		if strings.HasPrefix(c.ID, "tui.editor.") && c.ID != widget.EditorContextMenu.ID {
			continue
		}
		if !strings.Contains(readme, "`"+c.ID+"`") {
			t.Errorf("%s (scope %s) is not in tui/keymap/README.md", c.ID, c.Scope)
		}
	}
}
