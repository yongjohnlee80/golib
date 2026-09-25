package decl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// PALETTES — how a widget looks, written the way Qt writes it.
//
//	import editor.theme.retro 1.0
//
//	MenuBar {
//	    palette.window: Theme.menu.window
//	    palette.accent: Theme.menu.accent
//	}
//
// A Qt Quick Control takes its colours from a grouped `palette` property whose
// members are QPalette's colour ROLES, and a document binds those roles to a
// theme singleton. Here too: the layout names roles, a theme module supplies
// the values, and switching theme is the import line alone.
//
// A role means the same thing on every widget that takes it, which is what
// lets one theme serve them all:
//
//	window, windowText           a surface and the text on it
//	base, text                   an editing area and the text in it
//	highlight, highlightedText   the selected row, or the frame that has focus
//	accent                       the menu's access-key letter
//
// A widget type states WHICH roles it takes; the reading, the colour syntax and
// the looks they combine into are written once, below.

// role is one QPalette colour role, spelled as a document writes it.
type role string

const (
	roleWindow          role = "window"
	roleWindowText      role = "windowText"
	roleBase            role = "base"
	roleText            role = "text"
	roleHighlight       role = "highlight"
	roleHighlightedText role = "highlightedText"
	roleAccent          role = "accent"
)

// palette is the roles one declaration set.
type palette map[role]style.Color

// prop is the property a document writes for a role: `palette.window`.
func (r role) prop() string { return "palette." + string(r) }

// paletteProps names a type's palette properties, for its constructor list.
func paletteProps(roles []role) []string {
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = r.prop()
	}
	return out
}

// withPalette adds a type's palette roles to its builder's fields, so the one
// readProps call reads both and reports both consumed.
func withPalette(fields map[string]field, p palette, roles []role) map[string]field {
	for _, r := range roles {
		fields[r.prop()] = func(v qml.SpecValue) error {
			c, err := colorOf(v)
			if err != nil {
				return fmt.Errorf("%s: %w", r.prop(), err)
			}
			p[r] = c
			return nil
		}
	}
	return fields
}

// has reports whether any of the roles was set.
func (p palette) has(roles ...role) bool {
	for _, r := range roles {
		if _, ok := p[r]; ok {
			return true
		}
	}
	return false
}

// look is a background role and a foreground role as one style. A role left
// unset is left unset in the style, so whatever the look is merged over shows
// through.
func (p palette) look(bg, fg role) style.Style {
	st := style.New()
	if c, ok := p[bg]; ok {
		st = st.Background(c)
	}
	if c, ok := p[fg]; ok {
		st = st.Foreground(c)
	}
	return st
}

// ---------------------------------------------------------------- colours

// colourNames are the eight ANSI colours, by slot. They name SLOTS, not RGB:
// "blue" is whatever the user's terminal palette says blue is, which is what a
// 1990s text-mode look is made of. "bright" before a name is the slot eight
// above it.
var colourNames = map[string]int{
	"black": 0, "red": 1, "green": 2, "yellow": 3,
	"blue": 4, "magenta": 5, "cyan": 6, "white": 7,
}

// colorOf reads a colour as a theme writes it:
//
//	"blue"          an ANSI slot           "brightwhite"  the bright slot
//	"gray"/"grey"   bright black           "#1e90ff"      truecolor
//	"default"       the terminal's own foreground or background
//
// Case-insensitive, as Qt's colour names are.
func colorOf(v qml.SpecValue) (style.Color, error) {
	s, err := stringOf(v)
	if err != nil {
		return style.Color{}, err
	}
	name := strings.ToLower(strings.TrimSpace(s))
	switch name {
	case "default":
		return style.Default(), nil
	case "gray", "grey":
		return style.ANSI(8), nil
	}
	if strings.HasPrefix(name, "#") {
		return hexColour(name, v)
	}
	bright := strings.HasPrefix(name, "bright")
	if n, ok := colourNames[strings.TrimPrefix(name, "bright")]; ok {
		if bright {
			n += 8
		}
		return style.ANSI(n), nil
	}
	return style.Color{}, fmt.Errorf("%q is not a colour: want one of %s, bright<name>, gray, "+
		"#rrggbb or default (at %s)", s, colourList(), v.Pos)
}

func hexColour(s string, v qml.SpecValue) (style.Color, error) {
	n, err := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	if len(s) != 7 || err != nil {
		return style.Color{}, fmt.Errorf("%q is not a #rrggbb colour (at %s)", s, v.Pos)
	}
	return style.RGB(uint8(n>>16), uint8(n>>8), uint8(n)), nil
}

func colourList() string {
	names := make([]string, 8)
	for n, i := range colourNames {
		names[i] = n
	}
	return strings.Join(names, ", ")
}

// ---------------------------------------------------------------- per widget
//
// What each widget type makes of its roles. Each returns "nothing set" when the
// declaration set none of them, so a document with no palette keeps golib's own
// look exactly — tokens and all — rather than a palette of unset colours.

var (
	menuRoles   = []role{roleWindow, roleWindowText, roleHighlight, roleHighlightedText, roleAccent}
	frameRoles  = []role{roleWindow, roleWindowText, roleHighlight}
	editorRoles = []role{roleBase, roleText, roleHighlight, roleHighlightedText}
	statusRoles = []role{roleWindow, roleWindowText}
)

// menuStyle dresses a menu bar and every dropdown under it. golib derives the
// looks nobody named — disabled, armed, the popup border — from the two given.
func (p palette) menuStyle() (*widget.MenuStyle, bool) {
	if !p.has(menuRoles...) {
		return nil, false
	}
	def := widget.DefaultMenuStyle()
	surface := def.Surface()
	if p.has(roleWindow, roleWindowText) {
		surface = p.look(roleWindow, roleWindowText)
	}
	selected := def.Selected()
	if p.has(roleHighlight, roleHighlightedText) {
		// Not reversed: the theme chose these colours, and reversing them would
		// paint the other two.
		selected = p.look(roleHighlight, roleHighlightedText).Reverse(false)
	}
	st := widget.NewMenuStyle(surface, selected)
	if c, ok := p[roleAccent]; ok {
		// The colour is added to golib's underline, not swapped for it: the
		// underline is the cue that survives a palette with one colour in it.
		st = st.WithHotkey(style.New().Foreground(c).Underline(true))
	}
	return st, true
}

// frameOptions colour a Frame: its interior and border on window, the border
// line in windowText, and in highlight while focus is inside it.
func (p palette) frameOptions() []widget.BoxOption {
	var opts []widget.BoxOption
	if p.has(roleWindow, roleWindowText) {
		st := style.New()
		if c, ok := p[roleWindow]; ok {
			st = st.Background(c)
		}
		if c, ok := p[roleWindowText]; ok {
			st = st.BorderForeground(c)
		}
		opts = append(opts, widget.WithStyle(st))
	}
	if c, ok := p[roleHighlight]; ok {
		opts = append(opts, widget.WithFocusedStyle(style.New().BorderForeground(c)))
	}
	return opts
}

// editorStyles colour an Editor's text on base, and its selection.
func (p palette) editorStyles() (widget.TextInputStyles, bool) {
	var st widget.TextInputStyles
	if !p.has(editorRoles...) {
		return st, false
	}
	st.Text = p.look(roleBase, roleText)
	if p.has(roleHighlight, roleHighlightedText) {
		st.Selection = p.look(roleHighlight, roleHighlightedText).Reverse(false)
	}
	return st, true
}

// barStyle colours a StatusBar.
func (p palette) barStyle() (style.Style, bool) {
	if !p.has(statusRoles...) {
		return style.Style{}, false
	}
	return p.look(roleWindow, roleWindowText), true
}
