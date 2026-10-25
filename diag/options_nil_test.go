package diag

import (
	"context"
	"testing"
)

// A nil option is no option: Start skips it, as every constructor in golib does, and
// the options after it still apply.
func TestStartSkipsANilOption(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Start with a nil option panicked: %v", r)
		}
	}()
	applied := false
	m, err := Start(context.Background(), shortDir(t), nil, func(*config) { applied = true })
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if !applied {
		t.Error("the option after the nil was not applied")
	}
}
