package term

import (
	"strings"
	"testing"
)

// Bell writes one BEL, alone; a stopped backend refuses.
func TestBellWritesOneBEL(t *testing.T) {
	w := &countingWriter{}
	b := newHarness(strings.NewReader(""), w, WithEnv(noEnv))
	if err := b.Bell(); err != nil {
		t.Fatal(err)
	}
	if w.String() != "\a" {
		t.Fatalf("wrote %q, want one BEL", w.String())
	}
	_ = b.Stop()
	if err := b.Bell(); err != ErrClosed {
		t.Fatalf("Bell after Stop = %v, want ErrClosed", err)
	}
}
