package decl

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// Both cursor states use a readable foreground even when a theme supplied one that blends into
// its highlight. The same viewStyles rule is used by the terminal results table.
func TestViewCursorKeepsTextReadableInBothFocusStates(t *testing.T) {
	p := palette{
		roleText:                  style.RGB(235, 235, 235),
		roleHighlight:             style.RGB(220, 210, 245),
		roleHighlightedText:       style.RGB(240, 235, 250),
		roleInactiveHighlight:     style.RGB(55, 60, 70),
		roleInactiveHighlightText: style.RGB(65, 70, 80),
	}
	s := p.viewStyles()
	for name, st := range map[string]style.Style{"focused": s.CursorRow, "blurred": s.CursorBlurred} {
		fg, okFG := st.GetForeground()
		bg, okBG := st.GetBackground()
		ratio, ok := style.ContrastRatio(fg, bg)
		if !okFG || !okBG || !ok || ratio < 4.5 {
			t.Errorf("%s cursor foreground %v on %v has contrast %.2f", name, fg, bg, ratio)
		}
	}
}
