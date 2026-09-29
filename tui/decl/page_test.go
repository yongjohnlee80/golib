package decl_test

import (
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// TestAFrameWithAMaximumWidthIsACentredPage: capped at 24 columns in a 60-column Window it is
// drawn from column 18 to 41, its text inside; uncapped it fills the width again.
func TestAFrameWithAMaximumWidthIsACentredPage(t *testing.T) {
	s := decltest.Run(t, 60, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" Frame { title: \"page\"; maximumWidth: App.width; Text { text: \"words\" } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.width": 24}))
	s.WaitForText(t, "words")
	top, body := row(s, 0), row(s, 1)
	if l, r := col(top, "┌"), col(top, "┐"); l != 18 || r != 41 {
		t.Fatalf("the page's border runs from column %d to %d, want 18 to 41:\n%s", l, r, s)
	}
	if w := col(body, "words"); w <= 18 || w >= 41 {
		t.Fatalf("the text is at column %d, not inside the page:\n%s", w, s)
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.width", 0); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "the page uncapped", func(string) bool { return col(row(s, 0), "┌") == 0 && col(row(s, 0), "┐") == 59 })
	if !strings.Contains(s.String(), "words") {
		t.Fatalf("the text went:\n%s", s)
	}
}
