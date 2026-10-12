package decl

import "time"

// ReloadTicks drives HotReload's polls from c, one poll per value, in place of its ticker: a
// test's own clock, so what happens between two polls is the test's to say.
func ReloadTicks(c <-chan time.Time) HotReloadOption {
	return func(h *hotReload) { h.ticks = c }
}
