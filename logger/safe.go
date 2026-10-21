package logger

import "strings"

// Safe makes an untrusted value fit for a log record: at most max bytes, then "…", and every
// control character (below 0x20, and DEL) as '?', so a value an attacker chose cannot forge a
// line or a terminal escape in the log. The cut is by bytes; a rune it splits shows as U+FFFD.
func Safe(s string, max int) string {
	if len(s) > max {
		s = s[:max] + "…"
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}
