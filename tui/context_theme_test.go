package tui

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// Context.Theme is the theme frames are drawn with: WithTheme's to begin with, then the one
// SetTheme applied, as the surface's Theme reads it. A nil Context answers nil.
func TestAContextReadsTheAppsTheme(t *testing.T) {
	first, second := style.DefaultTheme(), style.NewTheme(style.ANSI(2))
	p := &probe{name: "p"}
	h := startApp(t, p, 10, 3, WithTheme(&first))
	var got *style.Theme
	h.onLoop(func() { got = p.ctx.Theme() })
	if got != &first {
		t.Fatalf("Theme = %p, want WithTheme's %p", got, &first)
	}
	h.app.SetTheme(&second)
	h.sync()
	var surface *style.Theme
	h.onLoop(func() { got, surface = p.ctx.Theme(), h.app.rctx.theme })
	if got != &second || surface != &second {
		t.Errorf("after SetTheme: Theme = %p, the surface's %p; want both %p", got, surface, &second)
	}
	var none *Context
	if th := none.Theme(); th != nil {
		t.Errorf("a nil Context's Theme = %p, want nil", th)
	}
}
