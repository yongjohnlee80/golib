package mermaid

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// A cell page's diagrams are on the page's own background: each of the theme's styles carries it,
// so no line, outline, label or selected box shows the terminal's own through the page.
func TestTheTokenThemeIsOnThePagesBackground(t *testing.T) {
	th := TokenTheme()
	for name, st := range map[string]style.Style{"Line": th.Line, "Box": th.Box, "Label": th.Label, "Selected": th.Selected} {
		bg, ok := st.GetBackground()
		if tok, isTok := bg.Token(); !ok || !isTok || tok != style.TokenBackground {
			t.Errorf("%s's background %v (set %v); want TokenBackground", name, bg, ok)
		}
	}
}
