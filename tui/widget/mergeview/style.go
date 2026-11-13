package mergeview

import "github.com/yongjohnlee80/golib/tui/style"

// Style is how a MergeView marks lines: a background for each pane's added, deleted and changed
// lines and for a conflict's, and the connectors' marks. A zero field takes its default: a muted
// tint for the theme's darkness, legible under the text's own colour.
type Style struct {
	Added, Deleted, Changed, Conflict style.Style
}

// The default tints: a light theme's and a dark one's.
var defaultStyle = Style{
	Added:    style.New().Background(style.Adaptive(style.RGB(0xd5, 0xea, 0xc8), style.RGB(0x24, 0x3a, 0x2a))),
	Deleted:  style.New().Background(style.Adaptive(style.RGB(0xf2, 0xd3, 0xd0), style.RGB(0x48, 0x26, 0x28))),
	Changed:  style.New().Background(style.Adaptive(style.RGB(0xd3, 0xdf, 0xf2), style.RGB(0x24, 0x32, 0x4a))),
	Conflict: style.New().Background(style.Adaptive(style.RGB(0xf4, 0xdf, 0xb6), style.RGB(0x4c, 0x3a, 0x1c))),
}

// over is s with what each field leaves unset taken from the default.
func (s Style) over() Style {
	return Style{
		Added:    s.Added.Inherit(defaultStyle.Added),
		Deleted:  s.Deleted.Inherit(defaultStyle.Deleted),
		Changed:  s.Changed.Inherit(defaultStyle.Changed),
		Conflict: s.Conflict.Inherit(defaultStyle.Conflict),
	}
}

// look is one of the marks a line wears.
type look uint8

const (
	plain look = iota
	added
	deleted
	changed
	conflict
)

func (s Style) of(l look) (style.Style, bool) {
	switch l {
	case added:
		return s.Added, true
	case deleted:
		return s.Deleted, true
	case changed:
		return s.Changed, true
	case conflict:
		return s.Conflict, true
	}
	return style.Style{}, false
}

// marker is the connectors' mark of a change of look l: the theme's colour for it.
func marker(l look) style.Style {
	switch l {
	case added:
		return style.New().Foreground(style.TokenSuccess)
	case deleted:
		return style.New().Foreground(style.TokenError)
	case conflict:
		return style.New().Foreground(style.TokenWarning).Bold(true)
	}
	return style.New().Foreground(style.TokenPrimary)
}
