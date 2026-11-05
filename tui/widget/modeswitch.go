package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// ModeSwitcher is what a ModeSwitch switches: an editor's view, chosen Raw or Rendered, and whether
// Rendered is available for the document shown. Both golib editors are one: the cell Editor here,
// and gui's.
type ModeSwitcher interface {
	View() EditorView
	SetView(EditorView)
	RenderAvailable() bool
}

// The switch's segments, as drawn: a click left of the separator is Raw, right of it Rendered.
const (
	ModeSwitchRaw      = "[ Raw "
	ModeSwitchSep      = "|"
	ModeSwitchRendered = " Rendered ]"
)

// ModeSwitch is an editor's [ Raw | Rendered ] control: one row, the view chosen bold and
// underlined, Rendered faint while the document shown has no Rendered view. It reads the editor
// at each paint, so it shows the view whoever chose it (the control, the editor's toggle key, the
// host); its owner marks it dirty when the view or its availability changes. It draws with text
// attributes alone, never a colour, so it reads in every theme.
type ModeSwitch struct {
	Base
	of   ModeSwitcher
	base style.Style // what its text wears under the attributes: the owner's text (SetStyle)
}

// NewModeSwitch is the switch for of.
func NewModeSwitch(of ModeSwitcher) *ModeSwitch { return &ModeSwitch{of: of} }

// SetStyle is the style the switch's text wears, its segments' bold, underline and faint over it:
// the owner's own text style, so the switch reads in the owner's theme as its text does. Unset, the
// surface's default.
func (s *ModeSwitch) SetStyle(base style.Style) { s.base = base }

// ModeSwitchWidth is the switch's width in cells.
const ModeSwitchWidth = len(ModeSwitchRaw) + len(ModeSwitchSep) + len(ModeSwitchRendered)

// Layout is the control's width on one row.
func (s *ModeSwitch) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: ModeSwitchWidth, H: 1})
}

// Render draws the two segments, the current one bold and underlined; Rendered is faint when it
// is not available.
func (s *ModeSwitch) Render(sur tui.Surface) {
	on, off := s.base.Bold(true).Underline(true), s.base
	raw, rendered := on, off
	if s.of.View() == ViewRendered {
		raw, rendered = off, on
	}
	if !s.of.RenderAvailable() {
		rendered = rendered.Faint(true)
	}
	x := tui.DrawText(sur, 0, 0, ModeSwitchRaw, raw)
	x = tui.DrawText(sur, x, 0, ModeSwitchSep, off)
	tui.DrawText(sur, x, 0, ModeSwitchRendered, rendered)
}

// HandleEvent switches the view on a left press of a segment. A press of Rendered while it is not
// available changes nothing; the press is still the control's.
func (s *ModeSwitch) HandleEvent(ev tui.Event) bool {
	m, ok := ev.(tui.MouseEvent)
	if !ok || m.Kind != tui.MousePress || m.Button != tui.MouseLeft || m.Y != 0 {
		return false
	}
	switch {
	case m.X < len(ModeSwitchRaw):
		s.of.SetView(ViewRaw)
	case m.X > len(ModeSwitchRaw) && s.of.RenderAvailable():
		s.of.SetView(ViewRendered)
	}
	return true
}
