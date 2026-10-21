package widget

import "github.com/yongjohnlee80/golib/tui/style"

// THE TEXT WIDGETS' LOOKS: the style slots TextInput, TextArea and the Editor share, their defaults,
// and the one merge of an option's slots over them.

// TextInputStyles are the TextInput style hooks. Zero
// fields inherit the defaults.
type TextInputStyles struct {
	Text        style.Style // value text (default: theme foreground)
	Placeholder style.Style // default: mutedPlaceholder, TokenTextMuted and faint
	Selection   style.Style // default: inverted, no accent
	Error       style.Style // value text in the failed-validation state
}

// mutedPlaceholder is how a field's placeholder looks, and anything else drawn "muted": the muted
// text colour AND faint, as the style package defines muted text (TokenTextMuted is the colour
// only, and by default it is the foreground's). The colour alone drew a placeholder as bright as
// typed text, so it read as a prefilled value. TextInput, TextArea, Editor (its ruler) and Select
// all use it.
func mutedPlaceholder() style.Style { return style.New().Foreground(style.TokenTextMuted).Faint(true) }

// over is s laid over base, slot by slot: each set property of s wins, and base fills the rest. The
// one merge of the text widgets' looks (TextInput, TextArea, Editor).
func (s TextInputStyles) over(base TextInputStyles) TextInputStyles {
	return TextInputStyles{
		Text:        s.Text.Inherit(base.Text),
		Placeholder: s.Placeholder.Inherit(base.Placeholder),
		Selection:   s.Selection.Inherit(base.Selection),
		Error:       s.Error.Inherit(base.Error),
	}
}

// defaultTextInputStyles are a text widget's looks before any option: TextInput's, TextArea's and
// the Editor's alike.
func defaultTextInputStyles() TextInputStyles {
	return TextInputStyles{
		Placeholder: mutedPlaceholder(),
		Selection:   style.New().Reverse(true),
		Error:       style.New().Foreground(style.TokenError),
	}
}
