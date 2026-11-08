package widget

import "github.com/yongjohnlee80/golib/tui/style"

// RESTYLING AT RUNTIME — a widget's looks changed after it was built.
//
// Every widget below takes its looks as constructor options, and these are the
// same looks set later: a theme switched while the program runs, or a palette
// that reached the widget from the container around it. Each follows the rule
// Menu.WithStyle and Modal.WithStyle already follow: it REPLACES the looks the
// options gave, derived exactly as the constructor derives them, and a zero
// value restores golib's own. So restyling to the zero value is a widget built
// with no style option at all — nothing of an earlier look survives.
//
// Loop goroutine, like all widget state. Each repaints; Text and Box, whose
// style can change their size (padding), ask for a layout, which repaints too.

// WithStyle replaces the text's style.
func (t *Text) WithStyle(st style.Style) *Text {
	t.st = st
	t.RequestLayout()
	return t
}

// WithStyle replaces the style WithStyle gave at construction: the frame's
// interior, padding and border colour. WithBorder's border is kept.
func (x *Box) WithStyle(st style.Style) *Box {
	x.base = boxBase(st, x.border, x.borderSet)
	x.RequestLayout()
	return x
}

// WithFocusedStyle replaces the style merged over the base while focus is
// inside. The zero value restores the focused-border token.
func (x *Box) WithFocusedStyle(st style.Style) *Box {
	x.focusedSt = boxFocused(st, st != style.Style{})
	x.MarkDirty()
	return x
}

// WithStyles replaces the input's looks: each slot given over golib's own.
func (t *TextInput) WithStyles(st TextInputStyles) *TextInput {
	t.styles = st.over(defaultTextInputStyles())
	t.MarkDirty()
	return t
}

// WithStyles replaces the text area's looks: each slot given over golib's own.
func (t *TextArea) WithStyles(st TextInputStyles) *TextArea {
	t.styles = st.over(defaultTextInputStyles())
	t.MarkDirty()
	return t
}

// WithBarStyle replaces the bar's base style; the zero value restores the
// panel tokens.
func (s *StatusBar) WithBarStyle(st style.Style) *StatusBar {
	if st == (style.Style{}) {
		st = defaultBarStyle()
	}
	s.bar = st
	s.MarkDirty()
	return s
}

// WithDividerStyle replaces the divider's style; the zero value restores the
// border token.
func (s *Split) WithDividerStyle(st style.Style) *Split {
	if st == (style.Style{}) {
		st = defaultDividerStyle()
	}
	s.divider = st
	s.MarkDirty()
	return s
}

// WithStyle dresses the bar's menu and every dropdown under it. nil restores
// golib's look.
func (b *MenuBar) WithStyle(s *MenuStyle) *MenuBar {
	b.menu.WithStyle(s)
	b.MarkDirty()
	return b
}
