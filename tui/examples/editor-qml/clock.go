package main

import (
	"sync"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// clock is a decl.Provider for App.clock.
//
// It delivers the current time synchronously when subscribed — the contract
// that closes the gap between reading a value and listening for changes — and
// then once per tick from its own goroutine, each delivery carrying a strictly
// increasing version so a late one cannot overwrite a newer one.
type clock struct {
	now  func() time.Time
	tick time.Duration

	mu      sync.Mutex
	version uint64
	stop    chan struct{}
}

// startClock subscribes the tree to the clock. Its ticks come from its own
// goroutine and reach the tree through the scheduler the tree was built with.
func (h *Host) startClock(now func() time.Time, tick time.Duration) error {
	h.clock = newClock(now, tick)
	return h.tree.Subscribe(h.clock)
}

func newClock(now func() time.Time, tick time.Duration) *clock {
	if now == nil {
		now = time.Now
	}
	if tick <= 0 {
		tick = time.Second
	}
	return &clock{now: now, tick: tick}
}

func (c *clock) value() map[string]qml.SpecValue {
	return map[string]qml.SpecValue{"App.clock": str(c.now().Format("15:04:05"))}
}

func (c *clock) next() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.version++
	return c.version
}

// Subscribe implements decl.Provider.
func (c *clock) Subscribe(fn func(decl.Update)) ([]string, func() error, error) {
	fn(decl.Update{Version: c.next(), Values: c.value()})

	c.stop = make(chan struct{})
	t := time.NewTicker(c.tick)
	go func(stop chan struct{}) {
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				fn(decl.Update{Version: c.next(), Values: c.value()})
			}
		}
	}(c.stop)

	var once sync.Once
	cancel := func() error {
		once.Do(func() { close(c.stop) })
		return nil
	}
	return []string{"App.clock"}, cancel, nil
}
