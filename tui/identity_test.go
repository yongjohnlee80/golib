package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// Using a backend after it has stopped is the shared CLOSED condition, and a
// caller must be able to recognise it without reading the sentence. All three
// refusals previously returned anonymous errors.
func TestTestBackend_UseAfterStopIsClosed(t *testing.T) {
	cases := map[string]func(b *TestBackend) error{
		"WriteClipboard": func(b *TestBackend) error { return b.WriteClipboard([]byte("x")) },
		"Inject":         func(b *TestBackend) error { return b.Inject() },
		"Start":          func(b *TestBackend) error { return b.Start(context.Background()) },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			b := NewTestBackend(10, 4)
			if err := b.Start(context.Background()); err != nil {
				t.Fatalf("the fixture is wrong: Start: %v", err)
			}
			if err := b.Stop(); err != nil {
				t.Fatalf("the fixture is wrong: Stop: %v", err)
			}

			err := call(b)
			if err == nil {
				t.Fatal("want an error after Stop, got nil")
			}
			if !errors.Is(err, errs.ErrClosed) {
				t.Errorf("must satisfy ErrClosed, got %v", err)
			}
		})
	}
}

// Running an App twice is a broken precondition, not a closed resource — the
// two must stay distinguishable, or a caller retrying on ErrClosed would retry
// something that will never succeed.
func TestApp_RunTwiceIsAPrecondition(t *testing.T) {
	a := NewApp(&probe{name: "identity"}, WithBackend(NewTestBackend(10, 4)))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = a.Run(ctx)

	err := a.Run(context.Background())
	if err == nil {
		t.Fatal("want an error on the second Run, got nil")
	}
	if !errors.Is(err, errs.ErrPrecondition) {
		t.Errorf("must satisfy ErrPrecondition, got %v", err)
	}
	if errors.Is(err, errs.ErrClosed) {
		t.Error("must NOT satisfy ErrClosed: a caller that retries on a closed " +
			"resource would retry a call that can never succeed")
	}
}
