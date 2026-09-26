package decl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/highlight"
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
//	button, buttonText           a button nobody is on
//	inactive.highlight, …Text    the selected row of a pane not in use
//	mid, light                   a pane's frame, without and with the keyboard
//
// SYNTAX ROLES are the same kind of thing for source text: one per
// highlight.Style — KSyntaxHighlighting's — written `syntax.keyword`,
// `syntax.comment`. Qt has no such group; it is golib's, and it is a palette
// group so that it is set ONCE, where a theme is applied, and every
// highlighter under it — an Editor's, a FileDialog's preview — wears it.
//
//	Window {
//	    syntax.keyword: Theme.syntax.keyword
//	    syntax.comment: Theme.syntax.comment
//	}
//
// Roles PROPAGATE — a node wears its own over its parent's (propagate.go) —
// and each type takes the ones it has a look for; the reading, the colour
// syntax and the looks they combine into are written once, below.

// Role is one QPalette colour role, spelled as a document writes it after
// `palette.`: "window", "inactive.highlight".
type Role string

// The roles, for a [Type] whose [Type.Restyle] wears a palette.
const (
	RoleWindow                  = roleWindow
	RoleWindowText              = roleWindowText
	RoleBase                    = roleBase
	RoleText                    = roleText
	RoleHighlight               = roleHighlight
	RoleHighlightedText         = roleHighlightedText
	RoleAccent                  = roleAccent
	RoleButton                  = roleButton
	RoleButtonText              = roleButtonText
	RoleInactiveHighlight       = roleInactiveHighlight
	RoleInactiveHighlightedText = roleInactiveHighlightText
	RoleMid                     = roleMid
	RoleLight                   = roleLight
)

// Palette is the effective palette a node wears — its own roles over its
// parent's — as a [Type.Restyle] receives it. Read-only.
type Palette struct{ p palette }

// Color is the colour of a role, and whether any node on the way set it.
func (p Palette) Color(r Role) (style.Color, bool) {
	c, ok := p.p[r]
	return c, ok
}

// Look is a background role and a foreground role as one style, each left
// unset when no node set it — so a style merged under it shows through, and
// an empty palette is golib's own look.
func (p Palette) Look(bg, fg Role) style.Style { return p.p.look(bg, fg) }

const (
	roleWindow          Role = "window"
	roleWindowText      Role = "windowText"
	roleBase            Role = "base"
	roleText            Role = "text"
	roleHighlight       Role = "highlight"
	roleHighlightedText Role = "highlightedText"
	roleAccent          Role = "accent"
	roleButton          Role = "button"
	roleButtonText      Role = "buttonText"
	// The INACTIVE group: Qt's colours for a part without the keyboard. Here,
	// the cursor row of a list whose pane is not the one in use.
	roleInactiveHighlight     Role = "inactive.highlight"
	roleInactiveHighlightText Role = "inactive.highlightedText"
	// mid and light frame a pane: mid without the keyboard, light with it.
	roleMid   Role = "mid"
	roleLight Role = "light"
)

// SyntaxRole is the role that colours one highlight style: `syntax.keyword`.
func SyntaxRole(s highlight.Style) Role { return Role(syntaxPrefix + s.String()) }

const syntaxPrefix = "syntax."

// palette is the roles one declaration set.
type palette map[Role]style.Color

// prop is the property a document writes for a role: `palette.window`, or a
// syntax role as it is spelt, `syntax.keyword`.
func (r Role) prop() string {
	if strings.HasPrefix(string(r), syntaxPrefix) {
		return string(r)
	}
	return "palette." + string(r)
}

// syntaxStyles are the syntax roles as the looks a highlighter paints: each
// one set, a foreground; each unset, zero — the text's own.
func (p palette) syntaxStyles() widget.SyntaxStyles {
	var st widget.SyntaxStyles
	for i := range highlight.Styles {
		if c, ok := p[SyntaxRole(highlight.Style(i))]; ok {
			st[i] = style.New().Foreground(c)
		}
	}
	return st
}

// has reports whether any of the roles was set.
func (p palette) has(roles ...Role) bool {
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
func (p palette) look(bg, fg Role) style.Style {
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
	if c, ok := v.Obj.(style.Color); v.Kind == qml.SpecValueObject && ok {
		return c, nil
	}
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
	menuRoles   = []Role{roleWindow, roleWindowText, roleHighlight, roleHighlightedText, roleAccent}
	editorRoles = []Role{roleBase, roleText, roleHighlight, roleHighlightedText}
	statusRoles = []Role{roleWindow, roleWindowText}
)

// browserStyles dress a FileDialog's browser: its panes on base, the cursor on
// highlight while the list has the keyboard and on the inactive highlight once
// it has not, and each pane framed in mid, or light while it is in use.
func (p palette) browserStyles() (widget.FilePaneStyles, bool) {
	// Every role the looks below read: a palette setting any of them — by
	// inheritance, say, only the Window's highlight — dresses the browser.
	if !p.has(roleBase, roleText, roleHighlight, roleHighlightedText, roleInactiveHighlight,
		roleInactiveHighlightText, roleMid, roleLight, roleWindow, roleWindowText) {
		return widget.FilePaneStyles{}, false
	}
	return widget.FilePaneStyles{
		Surface:       p.look(roleBase, roleText),
		Cursor:        p.look(roleHighlight, roleHighlightedText),
		CursorBlurred: p.look(roleInactiveHighlight, roleInactiveHighlightText),
		Border:        p.look(roleBase, roleMid),
		FocusedBorder: p.look(roleBase, roleLight),
		// The gap between the panes is the CARD's, so it takes the window.
		Gap: p.look(roleWindow, roleWindowText),
	}, true
}

// dialogStyles dress a Dialog: the card on window, its buttons on button, and
// the focused one on highlight. Either is nil when its roles were not set.
func (p palette) dialogStyles() (*widget.ModalStyle, *widget.ButtonStyle) {
	var card *widget.ModalStyle
	if p.has(roleWindow, roleWindowText) {
		look := p.look(roleWindow, roleWindowText)
		card = widget.NewModalStyle(look, look.Bold(true))
	}
	var buttons *widget.ButtonStyle
	if p.has(roleButton, roleButtonText, roleHighlight, roleHighlightedText) {
		// Not reversed, for the reason the menu's selection is not: the theme
		// chose both pairs.
		buttons = widget.NewButtonStyle(
			p.look(roleButton, roleButtonText).Reverse(false),
			p.look(roleHighlight, roleHighlightedText).Reverse(false).Bold(true))
	}
	return card, buttons
}

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
