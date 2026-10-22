package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

const menuRowsHead = "import tui 1.0\n"

func menuRowsLayout(item string) string {
	return menuRowsHead + "Window {\n MenuBar { Menu { title: \"&File\"\n " + item + "\n } }\n Text { text: \"body\" }\n}"
}

// A MenuItem holds no rows, and says so before the engine edits it: it is no rearranger, so a
// reload that gives it a child rebuilds it, and its Children arity refuses the child, with the
// position and the hint to use a Menu. Before, one node type served Menu, MenuItem and
// MenuSeparator, so CanRestructure said yes for a MenuItem whose arrange then refused, with no
// position.
func TestAReloadGivingAMenuItemAChildIsRefusedAtItsPosition(t *testing.T) {
	s := decltest.Run(t, 40, 6, tuidecl.LayoutSource("main.qml", []byte(menuRowsLayout(`MenuItem { text: "&Open" }`))))
	s.WaitForText(t, "body")
	var err error
	onScreenLoop(t, s, func() {
		_, err = s.Program.Reload([]byte(menuRowsLayout(`MenuItem { text: "&Open"; MenuItem { text: "&Recent" } }`)))
	})
	if err == nil || !strings.Contains(err.Error(), "MenuItem takes no children") ||
		!strings.Contains(err.Error(), "use a Menu for a submenu") || !strings.Contains(err.Error(), "(at main.qml:") {
		t.Errorf("a reload giving a MenuItem a child: %v; want the arity's refusal, positioned, naming a Menu", err)
	}
}

// A node that belongs only in some parents says so itself (parentBound), in its parent's Create
// and, for the root, in VetRoot: a Shortcut as the root is refused, with its position, rather
// than mounted live in nothing. Before, VetRoot asked only about a SyntaxHighlighter and the
// renderers, so a root Shortcut was taken.
func TestARootShortcutIsRefused(t *testing.T) {
	_, err := tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte(menuRowsHead+`Shortcut { sequence: "Ctrl+Q" }`)),
		tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(10, 2))))
	if err == nil || !strings.Contains(err.Error(), "a Shortcut is live while focus is in the element that declares it") ||
		!strings.Contains(err.Error(), "the root is in none") || !strings.Contains(err.Error(), "(at main.qml:2:1)") {
		t.Errorf("a root Shortcut: %v; want refused, as in no holder, at main.qml:2:1", err)
	}
}
