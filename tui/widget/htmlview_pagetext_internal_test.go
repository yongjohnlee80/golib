package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// The plain page text is the theme's foreground on its own background, and the styles built on it
// keep that background unless they name their own: a code span's surface, a mark's warning.
func TestThePagesTextIsOnThePagesBackground(t *testing.T) {
	bg, ok := style.PageText().GetBackground()
	if tok, isTok := bg.Token(); !ok || !isTok || tok != style.TokenBackground {
		t.Errorf("PageText's background %v (set %v); want TokenBackground", bg, ok)
	}
	fg, ok := style.PageText().GetForeground()
	if tok, isTok := fg.Token(); !ok || !isTok || tok != style.TokenForeground {
		t.Errorf("PageText's foreground %v (set %v); want TokenForeground", fg, ok)
	}
	if got, ok := panMark.GetBackground(); !ok || got != bg {
		t.Errorf("the pan marks' background %v (set %v); want the page's", got, ok)
	}
}
