package embed

import (
	"context"
	"testing"
)

// A nil option is no option: NewOllama skips it, as every constructor in golib does, and
// the options after it still apply.
func TestNewOllamaSkipsANilOption(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewOllama with a nil option panicked: %v", r)
		}
	}()
	// No server answers: NewOllama applies its options, then fails to reach one.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	applied := false
	_, _ = NewOllama(ctx, "http://127.0.0.1:1", "", "m", nil, nil, func(*Ollama) { applied = true })
	if !applied {
		t.Error("the option after the nil was not applied")
	}
}
