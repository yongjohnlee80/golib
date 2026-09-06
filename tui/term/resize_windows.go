//go:build windows

package term

import "time"

// resizePollInterval is the Windows resize poll period: no
// SIGWINCH exists, and ReadConsoleInput window events cannot be mixed with
// stream reads on one handle, so the size is polled and diffed.
const resizePollInterval = 250 * time.Millisecond

// startResizeWatcher polls the console size and forwards changes into the
// decode loop, which re-queries fresh truth and emits an ordered ResizeEvent.
// When the probe reported mode-2048 in-band resize, the terminal's own
// reports are the source of truth and the poll is not started.
func (b *Backend) startResizeWatcher() {
	if b.caps.InBandResize {
		return
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		t := time.NewTicker(resizePollInterval)
		defer t.Stop()
		last, _ := b.querySize()
		for {
			select {
			case <-t.C:
				sz, err := b.querySize()
				if err == nil && sz != last {
					last = sz
					select {
					case b.resizeCh <- struct{}{}:
					default:
					}
				}
			case <-b.done:
				return
			}
		}
	}()
}
