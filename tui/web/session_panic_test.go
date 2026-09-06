package web

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
)

// The session's own comment promises the panic record "carries the value". It
// did not: %v rendered the value into text while the message still read
// correctly, so a consumer reading RunErr could see THAT a panic happened and
// never recover WHAT it carried.
//
// This is the third site of that shape, after tui/app.go and tui/tasks.go, and
// the one where it mattered most — RunErr is exported, so the loss reached a
// consumer rather than a log line.
func TestSessionPanicRecord_CarriesTheValue(t *testing.T) {
	want := errs.Fatal{Op: "app: render", Rule: "store closed under the session"}

	// Build the record the way the recover path does.
	got := errs.Recovered(tui.ErrPanic, any(want), "web: the application panicked")

	if got == nil {
		t.Fatal("a recovered panic must produce an error")
	}
	// The identity a consumer already branches on, unchanged.
	if !errors.Is(got, tui.ErrPanic) {
		t.Error("must satisfy tui.ErrPanic — the same question App.Run answers " +
			"under PanicReturn, so a consumer need not ask two")
	}
	// The half that was being destroyed.
	var f errs.Fatal
	if !errors.As(got, &f) {
		t.Fatalf("the panicked value must be recoverable, got %v", got)
	}
	if f != want {
		t.Errorf("recovered %+v, want %+v", f, want)
	}
	// And it still reads correctly for a person.
	if !strings.Contains(got.Error(), "the application panicked") {
		t.Errorf("the message must still say what happened: %q", got)
	}
}

// The negative control: the spelling this replaced still reads correctly and
// still answers the identity, which is exactly why the loss was invisible.
func TestSessionPanicRecord_TheOldSpellingLostTheValue(t *testing.T) {
	want := errs.Fatal{Op: "app: render", Rule: "store closed under the session"}
	flattened := errors.Join(tui.ErrPanic, errors.New("web: the application panicked: "+want.Error()))

	if !errors.Is(flattened, tui.ErrPanic) {
		t.Fatal("the fixture is wrong: the old spelling must still answer the identity")
	}
	if !strings.Contains(flattened.Error(), "store closed under the session") {
		t.Fatal("the fixture is wrong: the old spelling must still READ correctly")
	}
	var f errs.Fatal
	if errors.As(flattened, &f) {
		t.Error("the flattened form must NOT be recoverable — if it is, this test " +
			"proves nothing about what the fix bought")
	}
}
