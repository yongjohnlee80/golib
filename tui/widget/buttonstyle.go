package widget

import "github.com/yongjohnlee80/golib/tui/style"

// BUTTON STYLING, HELD SEPARATELY FROM THE BUTTON.
//
// A widget that hardcodes its own colours cannot be re-themed, and a whole
// application cannot switch between light and dark without editing every
// control it contains. The style is an ASSOCIATION: a Button holds a pointer to
// a ButtonStyle it does not own, so one immutable value can safely dress every
// button in the app.
//
// Sharing is safe precisely BECAUSE the value is immutable — but that also
// means reassigning the variable a caller happens to hold changes nothing: each
// live Button needs its own WithStyle call to point at a different style. What
// does change every button at once is the theme, because these are style tokens
// rather than literal colours and the App resolves them at render time.

// ButtonStyle carries the four looks a button can have. It is IMMUTABLE: every
// With* method returns a new value, so a style shared by a hundred buttons
// cannot be altered underneath them by whoever happens to hold a reference.
type ButtonStyle struct {
	normal   style.Style
	focused  style.Style
	armed    style.Style
	disabled style.Style
}

// NewButtonStyle builds a style from the two looks an author actually thinks
// about, deriving the other two.
//
// Disabled is normal faded, and armed is focused inverted. Both derivations are
// conventional enough to be recognisable without being told, which is the point:
// an author who has not considered the pressed look still gets one that reads as
// pressed rather than one that looks broken. Use [NewButtonStyleFull] to state
// all four.
func NewButtonStyle(normal, focused style.Style) *ButtonStyle {
	return &ButtonStyle{
		normal:   normal,
		focused:  focused,
		disabled: normal.Faint(true),
		armed:    focused.Reverse(true),
	}
}

// NewButtonStyleFull states all four looks explicitly, overriding the
// derivations.
func NewButtonStyleFull(normal, focused, disabled, armed style.Style) *ButtonStyle {
	return &ButtonStyle{normal: normal, focused: focused, disabled: disabled, armed: armed}
}

// DefaultButtonStyle is the look a Button has when its author has said nothing.
//
// Token-valued throughout, so it follows the App's theme rather than pinning
// colours a dark theme would then have to fight.
func DefaultButtonStyle() *ButtonStyle {
	normal := style.New().
		Background(style.TokenSurface).
		Foreground(style.TokenForeground)
	focused := style.New().
		Background(style.TokenPrimary).
		Foreground(style.TokenTextOnPrimary).
		Bold(true)
	return &ButtonStyle{
		normal:   normal,
		focused:  focused,
		disabled: style.New().Background(style.TokenSurface).Foreground(style.TokenTextMuted).Faint(true),
		armed:    focused.Reverse(true),
	}
}

// Normal returns the ordinary look.
//
// Every accessor is nil-safe. A Button with no style set asks its nil style for
// values constantly, and making each caller nil-check first would put that
// check in every widget rather than once here.
func (s *ButtonStyle) Normal() style.Style {
	if s == nil {
		return DefaultButtonStyle().normal
	}
	return s.normal
}

// Focused returns the look while the button holds focus.
func (s *ButtonStyle) Focused() style.Style {
	if s == nil {
		return DefaultButtonStyle().focused
	}
	return s.focused
}

// Armed returns the look while the button is pressed.
func (s *ButtonStyle) Armed() style.Style {
	if s == nil {
		return DefaultButtonStyle().armed
	}
	return s.armed
}

// Disabled returns the look while the button cannot be activated.
func (s *ButtonStyle) Disabled() style.Style {
	if s == nil {
		return DefaultButtonStyle().disabled
	}
	return s.disabled
}

// Style returns the look for one resolved state.
//
// ONE SELECTOR. Every caller goes through this rather than choosing an accessor
// itself, so a widget cannot accidentally apply its own precedence: the state
// is decided once, and this maps it. An unknown state falls back to normal
// rather than panicking — a wrong colour is a far better failure than a crash
// in the middle of a paint.
func (s *ButtonStyle) Style(st WidgetState) style.Style {
	switch st {
	case WidgetStateDisabled:
		return s.Disabled()
	case WidgetStateArmed:
		return s.Armed()
	case WidgetStateFocused:
		return s.Focused()
	}
	return s.Normal()
}

// WithNormal returns a copy with the ordinary look replaced.
//
// A copy, never a mutation: these values are shared between widgets by design,
// so changing one in place would silently restyle every button holding it.
func (s *ButtonStyle) WithNormal(v style.Style) *ButtonStyle {
	c := s.clone()
	c.normal = v
	return c
}

// WithFocused returns a copy with the focused look replaced.
func (s *ButtonStyle) WithFocused(v style.Style) *ButtonStyle {
	c := s.clone()
	c.focused = v
	return c
}

// WithArmed returns a copy with the pressed look replaced.
func (s *ButtonStyle) WithArmed(v style.Style) *ButtonStyle {
	c := s.clone()
	c.armed = v
	return c
}

// WithDisabled returns a copy with the unavailable look replaced.
func (s *ButtonStyle) WithDisabled(v style.Style) *ButtonStyle {
	c := s.clone()
	c.disabled = v
	return c
}

// clone copies the receiver, or the defaults when it is nil, so a With* call on
// a nil style produces a complete value rather than one with three empty looks.
func (s *ButtonStyle) clone() *ButtonStyle {
	if s == nil {
		d := DefaultButtonStyle()
		return d
	}
	c := *s
	return &c
}
