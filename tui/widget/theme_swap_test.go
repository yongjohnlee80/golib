package widget_test

import (
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// styleSwatches paints every default look introduced by the interactive
// widget layers. It uses a real Surface, so this verifies the App-owned theme
// and resolution cache rather than merely inspecting token-valued structs.
type styleSwatches struct {
	widget.Base
	styles []style.Style
}

func (s *styleSwatches) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: 1, H: len(s.styles)})
}

func (s *styleSwatches) Render(su tui.Surface) {
	for y, st := range s.styles {
		su.SetCell(0, y, "x", st)
	}
}

func (*styleSwatches) HandleEvent(tui.Event) bool { return false }

func interactiveDefaultStyles() []style.Style {
	b := widget.DefaultButtonStyle()
	m := widget.DefaultModalStyle()
	menu := widget.DefaultMenuStyle()
	r := widget.DefaultResizableStyle()
	return []style.Style{
		b.Normal(), b.Focused(), b.Armed(), b.Disabled(),
		m.Card(), m.Title(), m.Border(), m.Scrim(),
		menu.Surface(), menu.Selected(), menu.Armed(), menu.Disabled(),
		menu.Accel(), menu.Border(),
		r.Handle(), r.Active(),
		// Split's divider is a single TokenBorder style rather than a separate
		// public style association. The production-source audit ties its
		// constructor and render path to the same no-literal-colour rule.
		style.New().Foreground(style.TokenBorder),
	}
}

func solidTheme(c style.Color) style.Theme {
	return style.NewTheme(c,
		style.WithToken(style.TokenForeground, c),
		style.WithToken(style.TokenBackground, c),
		style.WithToken(style.TokenSurface, c),
		style.WithToken(style.TokenPanel, c),
		style.WithToken(style.TokenTextMuted, c),
		style.WithToken(style.TokenTextOnPrimary, c),
		style.WithToken(style.TokenBorder, c),
		style.WithToken(style.TokenBorderFocused, c),
	)
}

// TestThemeSwapRestylesEveryInteractiveDefaultLook verifies that every visual
// state introduced by Button, Modal, Menu/MenuItem and
// Resizable is rendered before and after one live App theme swap. MenuBar owns
// no paint and delegates to Menu; Split's one divider look is included too.
func TestThemeSwapRestylesEveryInteractiveDefaultLook(t *testing.T) {
	a := solidTheme(style.ANSI(1))
	b := solidTheme(style.ANSI(2))
	swatches := &styleSwatches{styles: interactiveDefaultStyles()}
	h := startAppOpts(t, swatches, 1, len(swatches.styles), tui.WithTheme(&a))
	defer h.stop()
	h.settle()

	before := h.tb.Snapshot()
	flushes := h.tb.Flushes()
	h.app.SetTheme(&b)
	h.waitFor("theme swap repaint", func() bool { return h.tb.Flushes() > flushes })
	after := h.tb.Snapshot()

	for y := range swatches.styles {
		if before[y][0].Attrs == after[y][0].Attrs {
			t.Errorf("default interactive look %d did not change after App.SetTheme: %s",
				y, fmt.Sprint(before[y][0].Attrs))
		}
	}
}
