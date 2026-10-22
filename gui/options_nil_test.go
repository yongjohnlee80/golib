package gui

import "testing"

// A nil option is no option: NewBackend skips it rather than call it.
func TestNewBackendSkipsANilOption(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewBackend with a nil option panicked: %v", r)
		}
	}()
	NewBackend(nil, WithTitle("t"))
}
