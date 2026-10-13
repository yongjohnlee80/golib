package decl

import (
	"strconv"
	"time"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
)

// ReloadTicks drives HotReload's polls from c, one poll per value, in place of its ticker, and
// tells done once each has been looked at and acted on: a test's own clock, so what happens
// between two polls is the test's to say.
func ReloadTicks(c <-chan time.Time, done chan<- struct{}) HotReloadOption {
	return func(h *hotReload) { h.ticks, h.polled = c, done }
}

// HostKeysOfForTest is hostKeysOf over a QML string literal ("F11, Ctrl+Shift+M").
func HostKeysOfForTest(lit string) (func(tui.KeyEvent) bool, error) {
	raw, err := strconv.Unquote(lit)
	if err != nil {
		return nil, err
	}
	return hostKeysOf(qml.SpecValue{Kind: qml.SpecValueString, Raw: raw})
}

// ModeKeyOfForTest is modeKeyOf over a QML string literal ("Ctrl+T").
func ModeKeyOfForTest(lit string) (func(tui.KeyEvent) bool, error) {
	raw, err := strconv.Unquote(lit)
	if err != nil {
		return nil, err
	}
	return modeKeyOf(qml.SpecValue{Kind: qml.SpecValueString, Raw: raw})
}
