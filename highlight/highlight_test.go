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
