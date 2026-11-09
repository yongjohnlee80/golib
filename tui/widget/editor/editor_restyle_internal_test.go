package editor

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/core"

	"github.com/yongjohnlee80/golib/tui/style"
)

// TestEditorRestylesAndReverts: an Editor restyled takes the new looks whole, the earlier ones
// dropped, and its zero styles restore the default.
func TestEditorRestylesAndReverts(t *testing.T) {
	red := style.New().Background(style.ANSI(1)).Foreground(style.ANSI(7))
	e := NewEditor(WithEditorStyles(core.TextInputStyles{Text: red}))
	e.WithStyles(core.TextInputStyles{Selection: red})
	if e.cells.styles != NewEditor(WithEditorStyles(core.TextInputStyles{Selection: red})).cells.styles {
		t.Fatal("the editor's earlier Text look survived a restyle")
	}
	if e.WithStyles(core.TextInputStyles{}).cells.styles != NewEditor().cells.styles {
		t.Fatal("the editor's zero styles did not restore the default")
	}
}
