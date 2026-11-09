package widget

import (
	"slices"
	"testing"
)

// TestObserversHearInTheOrderTheyCame: a window's observers, and the minimized windows', are told of
// a change in the order they registered, every time, and a cancelled one is not told (shell-D10).
func TestObserversHearInTheOrderTheyCame(t *testing.T) {
	c := NewWindowCore()
	m := &MinimizedWindows{}
	for _, reg := range []struct {
		name    string
		observe func(func()) func()
		notify  func()
	}{
		{"WindowCore", c.Observe, c.notifyObservers},
		{"MinimizedWindows", m.Observe, m.notifyWindowEntries},
	} {
		var heard []int
		var cancels []func()
		for i := range 6 {
			cancels = append(cancels, reg.observe(func() { heard = append(heard, i) }))
		}
		cancels[2]()
		for range 20 {
			heard = heard[:0]
			reg.notify()
			if want := []int{0, 1, 3, 4, 5}; !slices.Equal(heard, want) {
				t.Fatalf("%s told its observers in the order %v, want %v", reg.name, heard, want)
			}
		}
	}
}
