package lexical

import "testing"

// A nil option is no option: New skips it, as every constructor in golib does, and
// the options after it still apply.
func TestNewSkipsANilOption(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("New with a nil option panicked: %v", r)
		}
	}()
	applied := false
	New(nil, func(*configuration) { applied = true })
	if !applied {
		t.Error("the option after the nil was not applied")
	}
}
