package widget

import "github.com/yongjohnlee80/golib/tui/style"

// ModalStyle carries the surfaces a dialog paints. It follows ButtonStyle
// exactly — immutable, nil-safe, token-valued — so a consumer who has learned
// one styling type has learned them all, and one style value can safely dress
// every dialog in an application.
type ModalStyle struct {
	card   style.Style
	title  style.Style
	border style.Style
	scrim  style.Style
	// footer is the help line under the buttons; zero means "derive it".
	footer    style.Style
	footerSet bool
}

// NewModalStyle builds a style from the card and title looks, deriving the
// border from the card and the scrim from a dimmed background.
func NewModalStyle(card, title style.Style) *ModalStyle {
	return &ModalStyle{
		card:   card,
		title:  title,
		border: card,
		scrim:  defaultScrim(),
	}
}

// NewModalStyleFull states all four surfaces explicitly.
func NewModalStyleFull(card, title, border, scrim style.Style) *ModalStyle {
	return &ModalStyle{card: card, title: title, border: border, scrim: scrim}
}

// DefaultModalStyle is the look a Modal has when its author has said nothing.
// Token-valued throughout, so it follows the App's theme.
func DefaultModalStyle() *ModalStyle {
	card := style.New().Background(style.TokenPanel).Foreground(style.TokenForeground)
	return &ModalStyle{
		card:   card,
		title:  card.Bold(true),
		border: card,
		scrim:  defaultScrim(),
	}
}

// defaultScrim is the dimming behind a dialog: faint muted text on the ordinary
// background, so the covered content reads as present but unavailable rather
// than being hidden outright.
func defaultScrim() style.Style {
	return style.New().
		Background(style.TokenBackground).
		Foreground(style.TokenTextMuted).
		Faint(true)
}

// Card returns the dialog body's look. Nil-safe, like every accessor here: a
// Modal with no style asks a nil style for values on every paint, and the
// alternative is that check in every widget.
func (s *ModalStyle) Card() style.Style {
	if s == nil {
		return DefaultModalStyle().card
	}
	return s.card
}

// Title returns the title bar's look.
func (s *ModalStyle) Title() style.Style {
	if s == nil {
		return DefaultModalStyle().title
	}
	return s.title
}

// Border returns the card border's look.
func (s *ModalStyle) Border() style.Style {
	if s == nil {
		return DefaultModalStyle().border
	}
	return s.border
}

// Rule returns the look of the line between a dialog's body and its buttons.
// It IS the border's: the rule joins the frame at both ends, so a rule in any
// other colour would break the frame where it meets it.
func (s *ModalStyle) Rule() style.Style { return s.Border() }

// Footer returns the look of the help line under the buttons. Unless set, it is
// the card faded, so the keys read as chrome rather than as one more line of
// the message.
func (s *ModalStyle) Footer() style.Style {
	if s == nil || !s.footerSet {
		return s.Card().Faint(true)
	}
	return s.footer
}

// WithFooter returns a copy with the help line's look replaced.
func (s *ModalStyle) WithFooter(v style.Style) *ModalStyle {
	c := s.cloneModal()
	c.footer, c.footerSet = v, true
	return c
}

// Scrim returns the look of the dimming painted behind the topmost dialog.
func (s *ModalStyle) Scrim() style.Style {
	if s == nil {
		return DefaultModalStyle().scrim
	}
	return s.scrim
}

// WithCard returns a copy with the body look replaced. A copy, never a
// mutation: these values are shared between dialogs by design.
func (s *ModalStyle) WithCard(v style.Style) *ModalStyle {
	c := s.cloneModal()
	c.card = v
	return c
}

// WithTitle returns a copy with the title look replaced.
//
// Named WithTitle as a METHOD on the style, which does not collide with the
// package-level WithModalTitle option that sets a dialog's text.
func (s *ModalStyle) WithTitle(v style.Style) *ModalStyle {
	c := s.cloneModal()
	c.title = v
	return c
}

// WithBorder returns a copy with the border look replaced.
func (s *ModalStyle) WithBorder(v style.Style) *ModalStyle {
	c := s.cloneModal()
	c.border = v
	return c
}

// WithScrim returns a copy with the scrim look replaced.
//
// This is the style method; the package-level WithScrim option decides whether
// a scrim is painted at all. One says what it looks like, the other whether it
// exists.
func (s *ModalStyle) WithScrim(v style.Style) *ModalStyle {
	c := s.cloneModal()
	c.scrim = v
	return c
}

// cloneModal copies the receiver, or the defaults when it is nil, so a With*
// call on a nil style yields a complete value rather than three empty looks.
func (s *ModalStyle) cloneModal() *ModalStyle {
	if s == nil {
		return DefaultModalStyle()
	}
	c := *s
	return &c
}
