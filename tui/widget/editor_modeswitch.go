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

// ModeSwitchShower is a ModeSwitcher that says when its switch is shown at all: the cell Editor's
// only while it has a document view, as its own row is. A switch placed outside its owner (a
// frame's title slot) is laid out empty, and so hidden, while it says false.
type ModeSwitchShower interface {
	ModeSwitchShown() bool
}

// modeSwitchShown reports whether a switch for of is drawn: always, unless of says otherwise. A
// function of of, not a ModeSwitch method, so an embedder's Layout (gui's title-bar switch) never
// reads past an override of it.
func modeSwitchShown(of ModeSwitcher) bool {
	if sh, ok := of.(ModeSwitchShower); ok {
		return sh.ModeSwitchShown()
	}
	return true
}

// Layout is the control's width on one row; none while its owner hides it (ModeSwitchShower).
func (s *ModeSwitch) Layout(c tui.Constraints) tui.Size {
	if !modeSwitchShown(s.of) {
		return c.Constrain(tui.Size{})
	}
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
	if v, ok := ModeSwitchChoice(s.of, m.X); ok {
		s.of.SetView(v)
	}
	return true
}

// ModeSwitchSlot is a ModeSwitch mounted on its own, outside its owner: what an owner hands a
// frame's title slot (widget.Box.SetTitleSlot). It resolves a left press on a segment before the
// press moves focus, as a toolbar's control does, so the press is never skipped as focus
// redirected. It is a type of its own, not methods on ModeSwitch, so a widget that embeds ModeSwitch
// (gui's title-bar switch) keeps its own press handling.
type ModeSwitchSlot struct{ *ModeSwitch }

// ResolvePointerPress implements tui.PointerPressResolver: the press's segment, as HandleEvent's.
func (s ModeSwitchSlot) ResolvePointerPress(m tui.MouseEvent) (tui.Action, bool) {
	if m.Kind != tui.MousePress || m.Button != tui.MouseLeft || m.Y != 0 {
		return nil, false
	}
	v, ok := ModeSwitchChoice(s.of, m.X)
	if !ok {
		return nil, false
	}
	return EditorViewAction{View: v}, true
}

// HandleAction implements tui.ActionHandler: the view a resolved press chose.
func (s ModeSwitchSlot) HandleAction(inv tui.ActionInvocation) bool {
	if a, ok := inv.Action.(EditorViewAction); ok {
		s.of.SetView(a.View)
		return true
	}
	return false
}

// ModeSwitchChoice is the view a press at column x of a switch for of chooses: Raw left of the
// separator, Rendered right of it while of has it available; ok is false on the separator and on a
// faint Rendered. A function of its inputs, not a ModeSwitch method, so the switch's HandleEvent and
// the cell Editor's press resolver read the one hit-test, and an embedder's override is never
// bypassed by a call on the switch's own receiver. Exported: the Editor's files read it from outside
// the switch's own group.
func ModeSwitchChoice(of ModeSwitcher, x int) (EditorView, bool) {
	switch {
	case x >= 0 && x < len(ModeSwitchRaw):
		return ViewRaw, true
	case x > len(ModeSwitchRaw) && x < ModeSwitchWidth && of.RenderAvailable():
		return ViewRendered, true
	}
	return ViewRaw, false
}

// EditorViewAction is a view chosen on an editor's Raw | Rendered switch.
type EditorViewAction struct{ View EditorView }

// ActionID implements tui.Action.
func (EditorViewAction) ActionID() tui.ActionID { return "editor.view" }
