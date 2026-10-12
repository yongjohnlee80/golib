//go:build !linux

package gui

// beforeGio is a no-op on non-Linux platforms (macOS, Windows, iOS, Android),
// where Wayland-specific swap-interval workarounds are unnecessary.
func beforeGio() {}
