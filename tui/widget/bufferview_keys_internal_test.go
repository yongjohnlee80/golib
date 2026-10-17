package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldBufferViewKey is BufferView's key decision before commands, verbatim but for its effects, which
// are named. Its copy needed a mounted Context, which the oracle grants: an unmounted view declines
// both before and after.
func oldBufferViewKey(e tui.KeyEvent) string {
	if e.Kind == tui.KeyRelease {
		return ""
	}
	switch e.Code {
	case tui.KeyUp:
		return "up"
	case tui.KeyDown:
		return "down"
	case tui.KeyPageUp:
		return "pageUp"
	case tui.KeyPageDown:
		return "pageDown"
	case tui.KeyHome:
		return "top"
	case tui.KeyEnd:
		return "bottom"
	case 'y':
		if e.Mods.Chord() == 0 {
			return "copy"
		}
	}
	return ""
}

func TestBufferViewKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	v := &BufferView{}
	keyGridDiff(t, "BufferView", oldBufferViewKey,
		func(e tui.KeyEvent) string {
			cmd, ok := v.Context().KeyCommand(e, bufferViewCommands...)
			if !ok {
				return ""
			}
			return strings.TrimPrefix(cmd.ID, "tui.bufferview.")
		},
		hyperOrMeta)
}
