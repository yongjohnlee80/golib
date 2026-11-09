package decl

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

func guiProgram(src string) []tuidecl.ProgramOption {
	return []tuidecl.ProgramOption{tuidecl.Types(Types()...), tuidecl.LayoutSource("gui.qml", []byte(src))}
}

const titleAndShape = `import tui 1.0
Flex {
  direction: Tui.Vertical
  TitleBar { title: "AutoDoc"; Text { text: "synced" } }
  Shape { shape: Shape.Ellipse; fillColor: "#4080c0"; width: 5; height: 4 }
}`

// The gui types are sound QML: every property and enum they declare checks.
func TestTheGUITypesAreSoundQML(t *testing.T) {
	decltest.Check(t, guiProgram(titleAndShape)...)
}

// A document of TitleBar and Shape runs on a terminal: the bar's row with its trailing child, and
// the ellipse's rounded outline, stretched across the column as a column's children are, and its
// asked 4 rows tall, leaving the screen's last row empty.
func TestTitleBarAndShapeRunOnATerminal(t *testing.T) {
	s := decltest.Run(t, 20, 6, guiProgram(titleAndShape)...)
	want := " AutoDoc      synced\n" +
		"╭──────────────────╮\n" +
		"│                  │\n" +
		"│                  │\n" +
		"╰──────────────────╯\n" +
		"                    "
	s.WaitFor(t, "the title bar and the ellipse", func(screen string) bool { return screen == want })
}

// tuiFallback: Gui.Notice shows the notice in place of a widget's mock.
func TestTuiFallbackNoticeReplacesTheMock(t *testing.T) {
	s := decltest.Run(t, 40, 3, guiProgram(`import tui 1.0
Shape { width: 40; height: 3; tuiFallback: Gui.Notice }`)...)
	s.WaitFor(t, "the notice", func(screen string) bool {
		return strings.Contains(screen, "Not available in the terminal") && !strings.Contains(screen, "─")
	})
}

// A colour that isn't one is refused at the property's declaration.
func TestAShapesBadColourIsRefused(t *testing.T) {
	_, err := tuidecl.NewProgram(append(guiProgram(`import tui 1.0
Shape { strokeColor: "not-a-colour" }`), tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(10, 3))))...)
	if err == nil || !strings.Contains(err.Error(), `strokeColor "not-a-colour" is not a colour`) {
		t.Fatalf("NewProgram = %v; want the bad strokeColor refused", err)
	}
}
