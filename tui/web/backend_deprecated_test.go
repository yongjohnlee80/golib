package web

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// The deprecated exported transport methods still do what they did, through the unexported ones.
func TestDeprecatedTransportMethodsStillWork(t *testing.T) {
	b := New()
	done := make(chan error, 1)
	go func() { done <- b.Start(context.Background()) }()
	if err := b.Attach(hello()); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after Attach")
	}
	if err := b.Submit(tui.KeyEvent{Code: 'a', Text: "a"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if ev := <-b.Events(); ev != (tui.KeyEvent{Code: 'a', Text: "a"}) {
		t.Fatalf("Submit delivered %#v", ev)
	}
	if err := b.Resize(30, 6); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if ev := <-b.Events(); ev != (tui.ResizeEvent{W: 30, H: 6}) {
		t.Fatalf("Resize delivered %#v", ev)
	}
	if !b.PendingFrame() {
		t.Fatal("PendingFrame: no frame waiting after a resize")
	}
	fr, ok := b.NextFrame()
	if !ok {
		t.Fatal("NextFrame: no frame")
	}
	b.AckFrame(fr.Rev)
	b.Detach()
	boom := errors.New("socket gone")
	b.Fail(boom)
	if err := b.Err(); !errors.Is(err, boom) {
		t.Fatalf("Err after Fail = %v, want %v", err, boom)
	}
}
