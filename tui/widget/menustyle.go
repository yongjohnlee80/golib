package widget

import "github.com/yongjohnlee80/golib/tui/style"

// MenuStyle carries the looks a menu paints. It follows ButtonStyle and
// ModalStyle exactly — immutable, nil-safe, token-valued — so a consumer who has
// learned one styling type has learned them all, and one value can dress every
// menu in an application.
//
// Token-valued throughout: a widget holds style VALUES and never a theme, and
// the App resolves the tokens at render time. A default built from literal
// colours would be invisible to a theme swap, which is the defect this shape
// exists to prevent.
type MenuStyle struct {
	surface  style.Style // the menu's background and ordinary rows
	selected style.Style // the row the keyboard would act on
	armed    style.Style // pressed and not yet released
	disabled style.Style // present but not interactive
	accel    style.Style // the accelerator text at a row's right edge
	border   style.Style // the popup frame
}

// NewMenuStyle builds a style from the two looks a caller usually has an opinion
// about, deriving the rest: disabled is the surface faded, armed is the
// selection reversed, the accelerator is muted, and the border follows the
// surface.
func NewMenuStyle(surface, selected style.Style) *MenuStyle {
	return &MenuStyle{
		surface:  surface,
		selected: selected,
		armed:    selected.Reverse(true),
		disabled: surface.Faint(true),
		accel:    surface.Faint(true),
		border:   surface,
	}
}

// DefaultMenuStyle is the look a menu has when its author has said nothing.
func DefaultMenuStyle() *MenuStyle {
	surface := style.New().Background(style.TokenPanel).Foreground(style.TokenForeground)
	// The selection uses the theme's primary fill and the text token derived to
	// be readable on it, which is the same pairing List and Select already use —
	// so a menu's highlight matches the rest of the suite under any theme.
	selected := style.New().Background(style.TokenPrimary).
		Foreground(style.TokenTextOnPrimary)
	return &MenuStyle{
		surface:  surface,
		selected: selected,
		armed:    selected.Reverse(true),
		disabled: surface.Faint(true),
		accel:    style.New().Background(style.TokenPanel).Foreground(style.TokenTextMuted),
		border:   style.New().Background(style.TokenPanel).Foreground(style.TokenBorder),
	}
}

// Surface returns the menu's background look. Nil-safe, like every accessor
// here: a menu with no style asks a nil style for values on every paint, and the
// alternative is that check at every call site.
func (s *MenuStyle) Surface() style.Style {
	if s == nil {
		return DefaultMenuStyle().surface
	}
	return s.surface
}

// Selected returns the highlighted row's look.
func (s *MenuStyle) Selected() style.Style {
	if s == nil {
		return DefaultMenuStyle().selected
	}
	return s.selected
}

// Armed returns the pressed row's look.
func (s *MenuStyle) Armed() style.Style {
	if s == nil {
		return DefaultMenuStyle().armed
	}
	return s.armed
}

// Disabled returns an unavailable row's look.
func (s *MenuStyle) Disabled() style.Style {
	if s == nil {
		return DefaultMenuStyle().disabled
	}
	return s.disabled
}

// Accel returns the accelerator text's look.
func (s *MenuStyle) Accel() style.Style {
	if s == nil {
		return DefaultMenuStyle().accel
	}
	return s.accel
}

// Border returns the popup frame's look.
func (s *MenuStyle) Border() style.Style {
	if s == nil {
		return DefaultMenuStyle().border
	}
	return s.border
}

// rowStyle is the built-in look for one row — ONE selector, so the Menu's
// painter, MenuItem and anything else drawing a row share the precedence rather
// than each reimplementing it.
//
// PRIVATE, and it takes BOTH the row and the state. Disabled is a property of
// the ROW (not enabled, or a separator) while armed and selected are properties
// of the interaction, so a selector given only the state could not apply the
// rule below; and this precedence is the package's own presentation choice
// rather than a contract a consumer's renderer has to obey. A RowRenderer gets
// the four independent flags and decides for itself.
//
// What the first case actually does is give a disabled row and a separator the
// DISABLED look rather than the ordinary surface. It is written as a precedence
// over Selected as well, and that half is currently unreachable: repairSelection
// moves the selection off any row that stops being selectable, so "disabled and
// selected" is a state the Menu does not produce. It is kept because the
// ordering is the right shape if that ever changes, and it is recorded as
// unreachable rather than asserted as behaviour nothing can exercise.
//
// Armed above Selected IS reachable — a press arms the row the selection is
// already on — and a press that did not visibly arm reads as a dropped click.
func rowStyle(s *MenuStyle, row RowView, st RowState) style.Style {
	switch {
	case !row.Enabled || row.Kind == ItemKindSeparator:
		return s.Disabled()
	case st.Armed:
		return s.Armed()
	case st.Selected:
		return s.Selected()
	}
	return s.Surface()
}

// WithSurface returns a copy with the background look replaced. A copy, never a
// mutation: these values are shared between menus by design.
func (s *MenuStyle) WithSurface(v style.Style) *MenuStyle {
	c := s.cloneMenu()
	c.surface = v
	return c
}

// WithSelected returns a copy with the highlighted row's look replaced.
func (s *MenuStyle) WithSelected(v style.Style) *MenuStyle {
	c := s.cloneMenu()
	c.selected = v
	return c
}

// WithArmed returns a copy with the pressed row's look replaced.
func (s *MenuStyle) WithArmed(v style.Style) *MenuStyle {
	c := s.cloneMenu()
	c.armed = v
	return c
}

// WithDisabled returns a copy with the unavailable row's look replaced.
func (s *MenuStyle) WithDisabled(v style.Style) *MenuStyle {
	c := s.cloneMenu()
	c.disabled = v
	return c
}

// WithAccel returns a copy with the accelerator look replaced.
func (s *MenuStyle) WithAccel(v style.Style) *MenuStyle {
	c := s.cloneMenu()
	c.accel = v
	return c
}

// WithBorder returns a copy with the frame look replaced.
func (s *MenuStyle) WithBorder(v style.Style) *MenuStyle {
	c := s.cloneMenu()
	c.border = v
	return c
}

// cloneMenu copies the receiver, or the defaults when it is nil, so a With* call
// on a nil style yields a complete value rather than a set of empty looks.
func (s *MenuStyle) cloneMenu() *MenuStyle {
	if s == nil {
		return DefaultMenuStyle()
	}
	c := *s
	return &c
}
