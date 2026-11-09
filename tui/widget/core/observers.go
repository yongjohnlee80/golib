package widget

import "slices"

// observers are callbacks told of a change, in the order they registered: a window's
// (WindowCore.Observe) and the minimized windows' (MinimizedWindows.Observe). Order is the
// registration's, every time, so what one observer does is never seen by another in a different
// sequence from one change to the next.
type observers struct {
	next uint64
	list []observer
}

type observer struct {
	id uint64
	fn func()
}

// add registers fn, returning what cancels it; a nil fn registers nothing.
func (o *observers) add(fn func()) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	o.next++
	id := o.next
	o.list = append(o.list, observer{id, fn})
	return func() { o.list = slices.DeleteFunc(o.list, func(ob observer) bool { return ob.id == id }) }
}

// notify calls each observer registered now, in order. One that registers or cancels another while
// being told changes the next notification, not this one.
func (o *observers) notify() {
	for _, ob := range slices.Clone(o.list) {
		ob.fn()
	}
}
