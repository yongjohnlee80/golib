//go:build !linux

package gui

// beforeGio has nothing to do outside Linux: only Gio's Wayland window has the swap-interval
// problem swapinterval_linux.go works around.
func beforeGio() {}
