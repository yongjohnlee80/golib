package decl_test

import (
	"strings"
	"sync/atomic"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TestAnEditorsRulerAndCursorPositionFromTheDocument: `ruler` draws the guide at that column past
// each line's text; `cursorPosition`, bound, moves the cursor there (characters, a line break
// one) each time the source moves.
func TestAnEditorsRulerAndCursorPositionFromTheDocument(t *testing.T) {
	s := decltest.Run(t, 30, 5,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			` Editor { id: ed; text: "ab\ncdef"; ruler: 3; cursorPosition: App.at } }`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.at": 5}))
	s.WaitForText(t, "cdef")
	if r := row(s, 0); !strings.HasPrefix(r, "ab│") {
		t.Fatalf("the guide is not at column 3 past \"ab\": %q", r)
	}
	if r := row(s, 1); strings.HasPrefix(r, "cd│") {
		t.Fatalf("the guide is drawn over \"cdef\", a line that crosses it: %q", r)
	}
	at := func() [2]int {
		var got [2]int
		onScreenLoop(t, s, func() {
			e, ok := tuidecl.FindAs[*widget.Editor](s.Program, "ed")
			if !ok {
				t.Error("no editor ed") // on the UI loop: Fatal would not end the test
				return
			}
			got[0], got[1] = e.Line()
		})
		return got
	}
	if got := at(); got != [2]int{1, 2} {
		t.Fatalf("cursorPosition 5: line %d col %d, want 1 2 (a, b, the break, c, d)", got[0], got[1])
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.at", 1); err != nil {
			t.Error(err)
		}
	})
	if got := at(); got != [2]int{0, 1} {
		t.Fatalf("cursorPosition 1: line %d col %d, want 0 1", got[0], got[1])
	}
}

// TestAnEditorRefusesARulerOrPositionThatIsNotANumber, as a Frame refuses a title that is not text.
func TestAnEditorRefusesARulerOrPositionThatIsNotANumber(t *testing.T) {
	for _, src := range []string{
		`Editor { ruler: "wide" }`,
		`Editor { cursorPosition: true }`,
		`Frame { title: 3; Text { } }`,
	} {
		if _, err := mountDoc(t, "import tui 1.0\nWindow { "+src+" }"); err == nil {
			t.Errorf("%s was taken", src)
		}
	}
}

// TestAnEditorSaysItsCursorMoved: onCursorPositionChanged runs when the cursor moves, by a key or
// by the program, as Qt's TextEdit.cursorPositionChanged does.
func TestAnEditorSaysItsCursorMoved(t *testing.T) {
	var moves atomic.Int32
	s := decltest.Run(t, 30, 5,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			` Editor { id: ed; focus: true; text: "ab\ncd"; onCursorPositionChanged: App.moved() } }`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Commands(map[string]func() error{"App.moved": func() error { moves.Add(1); return nil }}))
	s.WaitForText(t, "cd")
	s.Keys(t, decltest.Rune('j'))
	s.WaitFor(t, "j reported", func(string) bool { return moves.Load() == 1 })
	onScreenLoop(t, s, func() {
		e, ok := tuidecl.FindAs[*widget.Editor](s.Program, "ed")
		if !ok {
			t.Error("no editor ed")
			return
		}
		e.SetLine(0, 1)
	})
	s.WaitFor(t, "SetLine reported", func(string) bool { return moves.Load() == 2 })
}

// TestAnImageMountsFromTheDocument: QML's Image is a widget.Image the host reaches by id, filling
// what it is given, and it refuses a property it does not have.
func TestAnImageMountsFromTheDocument(t *testing.T) {
	s := decltest.Run(t, 20, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow {\n Image { id: pic } }")))
	var cols, rows int
	var found bool
	for range 100 {
		onScreenLoop(t, s, func() {
			img, ok := tuidecl.FindAs[*widget.Image](s.Program, "pic")
			found = ok
			if ok {
				cols, rows = img.Cells()
			}
		})
		if found && cols > 0 {
			break
		}
	}
	if !found || cols != 20 || rows != 6 {
		t.Fatalf("Image found=%v cells %dx%d, want the window's 20x6", found, cols, rows)
	}
	if _, err := mountDoc(t, "import tui 1.0\nWindow { Image { source: \"x.png\" } }"); err == nil {
		t.Fatal("an Image took a source")
	}
}
