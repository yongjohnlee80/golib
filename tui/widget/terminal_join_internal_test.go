package widget

import (
	"context"
	"errors"
	"testing"
	"time"
)

// joinShutdowns is done only once every result is, with their errors joined, and at once with none.
func TestJoinShutdownsWaitsForEachAndJoinsTheirErrors(t *testing.T) {
	result := func() *TerminalShutdownResult {
		return &TerminalShutdownResult{completion: terminalCompletion{done: make(chan struct{})}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := joinShutdowns(nil).Wait(ctx); err != nil {
		t.Fatalf("no results: %v", err)
	}
	a, b := result(), result()
	joined := joinShutdowns([]*TerminalShutdownResult{a, b})
	failed := errors.New("b stalled")
	close(a.completion.done)
	select {
	case <-joined.Done():
		t.Fatal("the join was done before its second result")
	case <-time.After(50 * time.Millisecond):
	}
	b.completion.err = failed
	close(b.completion.done)
	if err := joined.Wait(ctx); !errors.Is(err, failed) {
		t.Errorf("the join's error is %v, want b's", err)
	}
}

// NewTerminal skips a nil option, as every other constructor here does.
func TestNewTerminalSkipsANilOption(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewTerminal(nil) panicked: %v", r)
		}
	}()
	if NewTerminal(nil) == nil {
		t.Fatal("no terminal")
	}
}
