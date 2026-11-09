package core

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// oldEscCloses is the Float's and the popup's Esc test before commands, verbatim.
func oldEscCloses(e tui.KeyEvent) string {
	if e.Kind != tui.KeyRelease && e.Code == tui.KeyEscape && e.Mods.Chord() == 0 {
		return "close"
	}
	return ""
}

func TestFloatAndPopupEscMatchTheOldTestWithNoMap(t *testing.T) {
	l, p := &floatLayer{}, &PopupLayer{}
	widgettest.KeyGridDiff(t, "Float", oldEscCloses, func(e tui.KeyEvent) string {
		_, ok := l.Context().KeyCommand(e, FloatClose)
		return map[bool]string{true: "close"}[ok]
	}, widgettest.HyperOrMeta)
	widgettest.KeyGridDiff(t, "popup", oldEscCloses, func(e tui.KeyEvent) string {
		_, ok := p.Context().KeyCommand(e, PopupClose)
		return map[bool]string{true: "close"}[ok]
	}, widgettest.HyperOrMeta)
}
