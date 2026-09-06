//go:build !windows

package term

import (
	"os"
	"os/signal"
	"syscall"
)

// startResizeWatcher forwards SIGWINCH into the decode loop, which re-queries
// the fresh size and emits an ordered ResizeEvent. SIGWINCH
// stays armed even when mode-2048 in-band reports are active — the harmless
// duplication is absorbed by the App intake's latest-wins policy.
func (b *Backend) startResizeWatcher() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer signal.Stop(ch)
		for {
			select {
			case <-ch:
				// Non-blocking: a pending notification already guarantees a
				// fresh re-query (signals themselves coalesce in the kernel).
				select {
				case b.resizeCh <- struct{}{}:
				default:
				}
			case <-b.done:
				return
			}
		}
	}()
}
