package decl

import (
	"image/color"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/widget"
	"github.com/yongjohnlee80/golib/parse/qml"
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

// strokeWidth: omitted, a stroked shape's outline is 1 wide; declared 0, it draws none.
func TestAShapesStrokeWidthDefaultsToOneAndTakesZero(t *testing.T) {
	for _, c := range []struct {
		src  string
		want bool // an outline is painted
	}{
		{`Shape { id: s; strokeColor: "red" }`, true},
		{`Shape { id: s; strokeColor: "red"; strokeWidth: 0 }`, false},
	} {
		p, err := tuidecl.NewProgram(append(guiProgram("import tui 1.0\n"+c.src), tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(10, 3))))...)
		if err != nil {
			t.Fatal(err)
		}
		w, ok := p.Find("s")
		if !ok {
			t.Fatal("no shape s")
		}
		rc := gui.NewRecordingCanvas(gui.Size{W: 80, H: 48}, gui.Size{W: 8, H: 16})
		w.(gui.View).Paint(rc)
		var width float32
		stroked := false
		for _, call := range rc.Calls {
			if call.Op == "StrokeRRect" {
				stroked, width = true, call.Width
			}
		}
		if stroked != c.want || (stroked && width != 1) {
			t.Errorf("%s: stroked %v (width %v); want stroked %v, 1 wide", c.src, stroked, width, c.want)
		}
	}
}

// fillColor at run time takes a colour and refuses what isn't one, keeping the shape's colour.
func TestAShapesFillColorSetterRefusesWhatIsNotAColour(t *testing.T) {
	set := shapeType().Setters["fillColor"]
	s := widget.NewShape(widget.ShapeRect, widget.WithFillColor(color.NRGBA{R: 1, A: 255}))
	if err := set(s, qml.SpecValue{Kind: qml.SpecValueString, Raw: "#0000ff"}); err != nil {
		t.Fatalf("fillColor #0000ff refused: %v", err)
	}
	paint := func() color.NRGBA {
		rc := gui.NewRecordingCanvas(gui.Size{W: 8, H: 16}, gui.Size{W: 8, H: 16})
		s.Paint(rc)
		return rc.Calls[0].Brush.Color
	}
	if got := paint(); got != (color.NRGBA{B: 255, A: 255}) {
		t.Fatalf("after fillColor #0000ff the fill is %v", got)
	}
	err := set(s, qml.SpecValue{Kind: qml.SpecValueString, Raw: "not-a-colour"})
	if err == nil || !strings.Contains(err.Error(), `fillColor "not-a-colour" is not a colour`) {
		t.Fatalf("fillColor not-a-colour = %v; want it refused", err)
	}
	if got := paint(); got != (color.NRGBA{B: 255, A: 255}) {
		t.Fatalf("a refused fillColor changed the fill to %v", got)
	}
	if err := set(s, qml.SpecValue{Kind: qml.SpecValueNumber, Raw: "3"}); err == nil || !strings.Contains(err.Error(), "want a colour string, got") {
		t.Fatalf("fillColor 3 = %v; want it refused as not a string", err)
	}
}
