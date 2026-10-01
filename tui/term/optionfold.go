package term

import "github.com/yongjohnlee80/golib/tui"

// optionLetters maps the character a US-layout Mac keyboard types for Option+letter back to that
// letter. A Mac terminal whose Option key is not set to send Meta/Esc+ delivers the composed
// character (Option+F types "ƒ") with no Alt bit, so Alt shortcuts never fire; [WithOptionFold]
// turns these back into Alt+letter.
//
// Five letters are dead keys on that layout (Option+E, I, N, U and `): they type nothing until the
// next key, so they cannot be folded and are absent here.
var optionLetters = map[rune]rune{
	'å': 'a', '∫': 'b', 'ç': 'c', '∂': 'd', 'ƒ': 'f', '©': 'g', '˙': 'h', '∆': 'j', '˚': 'k',
	'¬': 'l', 'µ': 'm', 'ø': 'o', 'π': 'p', 'œ': 'q', '®': 'r', 'ß': 's', '†': 't', '√': 'v',
	'∑': 'w', '≈': 'x', '¥': 'y', 'Ω': 'z',
}

// foldOption returns ev as Alt+letter when it is an unmodified key whose character Option+letter
// types on a US Mac keyboard, and ev unchanged otherwise.
func foldOption(ev tui.KeyEvent) tui.KeyEvent {
	if ev.Mods != 0 {
		return ev
	}
	l, ok := optionLetters[ev.Code]
	if !ok {
		return ev
	}
	ev.Code, ev.Mods, ev.Text = l, tui.ModAlt, ""
	ev.Base, ev.Shifted = 0, 0
	return ev
}
