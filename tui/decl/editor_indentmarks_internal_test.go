package decl

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// indentMarks takes its three names and refuses anything else by naming the choices.
func TestIndentMarksReadsItsThreeNames(t *testing.T) {
	for name, want := range map[string]widget.IndentMarks{
		"off": widget.IndentMarksOff, "cursorLine": widget.IndentMarksCursorLine, "all": widget.IndentMarksAll,
	} {
		if got, err := indentMarksOf(strValue(name)); err != nil || got != want {
			t.Errorf("indentMarks %q = %v, %v; want %v", name, got, err, want)
		}
	}
	for _, bad := range []string{"", "CursorLine", "on", "sometimes"} {
		if _, err := indentMarksOf(strValue(bad)); err == nil || !strings.Contains(err.Error(), `"cursorLine"`) {
			t.Errorf("indentMarks %q: %v, want refused with the choices named", bad, err)
		}
	}
}
