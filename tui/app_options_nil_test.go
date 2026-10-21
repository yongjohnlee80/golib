package tui

import "testing"

// A nil option is skipped, as every other constructor in golib skips one.
func TestANilAppOptionIsSkipped(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a nil option panicked: %v", r)
		}
	}()
	if NewApp(&probe{name: "root"}, nil, WithBackend(NewTestBackend(2, 1)), nil) == nil {
		t.Fatal("no App")
	}
}
