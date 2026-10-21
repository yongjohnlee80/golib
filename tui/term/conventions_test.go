package term

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// term's sentinels answer errs' shared kinds, so code holding only a tui.Backend can ask them.
func TestTermSentinelsAreErrsKinds(t *testing.T) {
	if !errors.Is(ErrNotTerminal, errs.ErrUnsupported) || !errors.Is(ErrConsoleTooOld, errs.ErrUnsupported) {
		t.Error("ErrNotTerminal and ErrConsoleTooOld must be errs.ErrUnsupported")
	}
	if !errors.Is(ErrClosed, errs.ErrClosed) {
		t.Error("ErrClosed must be errs.ErrClosed")
	}
}

// A nil option is skipped, as every other constructor in golib skips one.
func TestANilOptionIsSkipped(t *testing.T) {
	b := newHarness(strings.NewReader(""), &countingWriter{}, nil, WithEnv(noEnv), nil)
	if b == nil {
		t.Fatal("no backend")
	}
	if _, err := Open(nil, WithTTY(nil, nil), nil); !errors.Is(err, ErrNotTerminal) {
		t.Fatalf("Open with nil options and no tty: %v, want ErrNotTerminal, not a panic", err)
	}
}
