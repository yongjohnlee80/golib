package vfs

import "time"

// PollAfter sets the clock a Poll waits on, so a test sees every wait asked for and ends each one.
func PollAfter(after func(time.Duration) <-chan time.Time) WatchOption {
	return func(c *WatchConfig) { c.after = after }
}
