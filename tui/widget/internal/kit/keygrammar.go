package kit

import (
	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
)

// CountDigit is the digit a count is typed with: a digit key with no modifier held.
func CountDigit(k tui.KeyEvent) (int, bool) {
	s, ok := tuikey.Of(k.Code, k.Mods, k.Text)
	if !ok || s.Mods != 0 || s.Code < '0' || s.Code > '9' {
		return 0, false
	}
	return int(s.Code - '0'), true
}

// AddCount adds digit d to the count at *n, capped at limit so no count overflows. A 0 starts no
// count (it is a command of its own). It reports whether d went into the count.
func AddCount(n *int, d, limit int) bool {
	if d == 0 && *n == 0 {
		return false
	}
	*n = min(*n*10+d, limit)
	return true
}
