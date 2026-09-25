package highlight_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
)

// TestTheStylesAreKSyntaxHighlightingsTextStyle: all 31, in KDE's order, and
// each name round-trips.
func TestTheStylesAreKSyntaxHighlightingsTextStyle(t *testing.T) {
	if highlight.Styles != 31 {
		t.Fatalf("%d styles, want KSyntaxHighlighting's 31", highlight.Styles)
	}
	for _, c := range []struct {
		s    highlight.Style
		name string
	}{
		{highlight.Normal, "normal"}, {highlight.ControlFlow, "controlFlow"},
		{highlight.DataType, "dataType"}, {highlight.Error, "error"}, {highlight.Others, "others"},
	} {
		if c.s.String() != c.name {
			t.Errorf("%d is %q, want %q", c.s, c.s.String(), c.name)
		}
	}
	for i := range highlight.Styles {
		s := highlight.Style(i)
		if back, ok := highlight.StyleNamed(s.String()); !ok || back != s {
			t.Errorf("%s does not round-trip", s)
		}
	}
	if _, ok := highlight.StyleNamed("keywords"); ok {
		t.Error("a misspelt style was found")
	}
	if got := highlight.Style(99).String(); got != "Style(99)" {
		t.Errorf("an unknown style prints %q", got)
	}
}

func TestStyleForCaptureMapsTreeSitterNames(t *testing.T) {
	for capture, want := range map[string]highlight.Style{
		"@keyword":               highlight.Keyword,
		"keyword.control.import": highlight.ControlFlow, // longest known prefix
		"keyword.import":         highlight.Import,
		"@string.escape":         highlight.SpecialChar,
		"type.builtin":           highlight.DataType,
		"number.float":           highlight.Float,
		"comment.todo.fixme":     highlight.Comment,
		"nothing.known":          highlight.Normal,
		"":                       highlight.Normal,
	} {
		if got := highlight.StyleForCapture(capture); got != want {
			t.Errorf("%q → %s, want %s", capture, got, want)
		}
	}
}

func TestHighlighterFuncIsAHighlighter(t *testing.T) {
	var h highlight.Highlighter = highlight.HighlighterFunc(func(line string, prev highlight.State) ([]highlight.Span, highlight.State) {
		return []highlight.Span{{Start: 0, End: len(line), Style: highlight.Comment}}, prev + 1
	})
	spans, next := h.HighlightBlock("ab", 1)
	if len(spans) != 1 || spans[0].End != 2 || next != 2 {
		t.Fatalf("%+v %d", spans, next)
	}
}

func TestTheRepositoryFindsADefinitionByFileName(t *testing.T) {
	none := highlight.HighlighterFunc(func(string, highlight.State) ([]highlight.Span, highlight.State) { return nil, 0 })
	r := highlight.NewRepository(
		highlight.Definition{Name: "QML", Extensions: []string{"*.qml"}, Highlighter: none},
		highlight.Definition{Name: "JavaScript", Extensions: []string{"*.js", "*.mjs"}, Highlighter: none},
		highlight.Definition{Name: "Also", Extensions: []string{"*.js"}, Highlighter: none},
	)
	for file, want := range map[string]string{
		"/a/b/view.qml": "QML",
		"lib.mjs":       "JavaScript",
		"x.js":          "Also", // two claim it: the first by name
		"notes.txt":     "",
		"qml":           "",
	} {
		d, ok := r.DefinitionForFileName(file)
		if d.Name != want || ok != (want != "") {
			t.Errorf("%s: %q %v, want %q", file, d.Name, ok, want)
		}
	}
	if got := r.Names(); len(got) != 3 || got[0] != "Also" {
		t.Errorf("Names() = %v", got)
	}
	r.Add(highlight.Definition{Name: "QML"})
	if d, _ := r.Definition("QML"); d.Extensions != nil {
		t.Error("Add did not replace the definition of the same name")
	}
}
