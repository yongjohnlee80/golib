package errs_test

import (
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/errs"
)

// A package defines its specific condition by wrapping the shared one, and a
// call site adds context. A caller can then ask either question.
func Example_layered() {
	// package tui declares this once:
	errBackendStopped := fmt.Errorf("(%w: backend is stopped)", errs.ErrClosed)

	// a call site adds where it happened:
	err := fmt.Errorf("term: write to /dev/tty: %w", errBackendStopped)

	fmt.Println(err)
	fmt.Println("specific:", errors.Is(err, errBackendStopped))
	fmt.Println("general: ", errors.Is(err, errs.ErrClosed))
	// Output:
	// term: write to /dev/tty: (closed: backend is stopped)
	// specific: true
	// general:  true
}
