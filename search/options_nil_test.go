package search

import "testing"

// A nil option is no option: NewEngine skips it, as every constructor in golib does, and
// the options after it still apply.
func TestNewEngineSkipsANilOption(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewEngine with a nil option panicked: %v", r)
		}
	}()
	applied := false
	NewEngine[key, lexView](lexStore{&fake{}}, nil, func(*options) { applied = true })
	if !applied {
		t.Error("the option after the nil was not applied")
	}
}
