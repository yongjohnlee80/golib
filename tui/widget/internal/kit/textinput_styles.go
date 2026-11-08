package kit

import "github.com/yongjohnlee80/golib/tui/style"

// MutedPlaceholder is how a field's placeholder looks, and anything else drawn "muted": the muted
// text colour AND faint, as the style package defines muted text (TokenTextMuted is the colour
// only, and by default it is the foreground's). The colour alone drew a placeholder as bright as
// typed text, so it read as a prefilled value. TextInput, TextArea, Editor (its ruler) and Select
// all use it.
func MutedPlaceholder() style.Style { return style.New().Foreground(style.TokenTextMuted).Faint(true) }
