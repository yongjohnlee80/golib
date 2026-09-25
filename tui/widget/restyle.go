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

// WithStyles replaces the editor's looks: each slot given over golib's own.
func (e *Editor) WithStyles(st TextInputStyles) *Editor {
	e.styles = inheritTextStyles(st, defaultEditorStyles())
	e.MarkDirty()
	return e
}

// WithStyles replaces the input's looks: each slot given over golib's own.
func (t *TextInput) WithStyles(st TextInputStyles) *TextInput {
	t.styles = inheritTextStyles(st, defaultTextInputStyles())
	t.MarkDirty()
	return t
}

func inheritTextStyles(st, under TextInputStyles) TextInputStyles {
	return TextInputStyles{
		Text:        st.Text.Inherit(under.Text),
		Placeholder: st.Placeholder.Inherit(under.Placeholder),
		Selection:   st.Selection.Inherit(under.Selection),
		Error:       st.Error.Inherit(under.Error),
	}
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

// WithStyles replaces the looks of every part of the view — the list, the
// preview, each pane's frame and the gap between them. The zero value restores
// golib's own.
func (v *FileOpenView) WithStyles(st FilePaneStyles) *FileOpenView {
	styled := st != (FilePaneStyles{})
	v.cfg.st, v.cfg.styled = st, styled
	v.list.withStyles(st, styled)
	if v.preview != nil {
		v.preview.withStyles(st, styled)
	}
	if sp, ok := v.root.(*Split); ok {
		gap := style.Style{}
		if styled {
			gap = st.Gap
		}
		sp.WithDividerStyle(gap)
	}
	return v
}

// WithStyles replaces the looks of every part of the view: the name field and
// its frame, and the listing under it.
func (v *FileSaveView) WithStyles(st FilePaneStyles) *FileSaveView {
	styled := st != (FilePaneStyles{})
	v.cfg.st, v.cfg.styled = st, styled
	text := TextInputStyles{}
	if styled {
		text.Text = st.Surface
	}
	v.name.WithStyles(text)
	stylePane(v.namePane, st, styled)
	v.listing.WithStyles(st)
	return v
}

// withStyles restyles the list's rows and its frame, as NewFileList styles
// them.
func (l *FileList) withStyles(st FilePaneStyles, styled bool) {
	l.st, l.styled = st, styled
	l.list.styles = defaultListStyles()
	if styled {
		l.list.SetStyles(fileListStyles(st))
	}
	l.list.MarkDirty()
	stylePane(l.box, st, styled)
}

// withStyles restyles the preview's text and its frame.
func (p *FilePreview) withStyles(st FilePaneStyles, styled bool) {
	text := TextInputStyles{}
	if styled {
		text.Text = st.Surface
	}
	p.view.WithStyles(text)
	stylePane(p.box, st, styled)
}
