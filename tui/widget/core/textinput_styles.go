package core

import (
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// THE TEXT WIDGETS' LOOKS: the style slots TextInput, TextArea and the Editor share, their defaults,
// and the one merge of an option's slots over them.

// TextInputStyles are the TextInput style hooks. Zero
// fields inherit the defaults.
type TextInputStyles struct {
	Text        style.Style // value text (default: theme foreground)
	Placeholder style.Style // default: kit.MutedPlaceholder, TokenTextMuted and faint
	Selection   style.Style // default: inverted, no accent
	Error       style.Style // value text in the failed-validation state
}

// Over is s laid over base, slot by slot: each set property of s wins, and base fills the rest. The
// one merge of the text widgets' looks (TextInput, TextArea, Editor); a text field of your own
// styled by TextInputStyles merges a caller's styles over its own with it.
func (s TextInputStyles) Over(base TextInputStyles) TextInputStyles {
	return TextInputStyles{
		Text:        s.Text.Inherit(base.Text),
		Placeholder: s.Placeholder.Inherit(base.Placeholder),
		Selection:   s.Selection.Inherit(base.Selection),
		Error:       s.Error.Inherit(base.Error),
	}
}

// DefaultTextInputStyles are a text widget's looks before any option: TextInput's, TextArea's and
// the Editor's alike. A text field of your own starts from them, to look like golib's.
func DefaultTextInputStyles() TextInputStyles {
	return TextInputStyles{
		Placeholder: kit.MutedPlaceholder(),
		Selection:   style.New().Reverse(true),
		Error:       style.New().Foreground(style.TokenError),
	}
}
