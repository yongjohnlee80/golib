package errs_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// The whole reason the function exists: the panicked VALUE must still be
// recoverable on the other side.
func TestRecovered_KeepsTheValueForErrorsAs(t *testing.T) {
	want := errs.Fatal{Op: "tui: Mount", Rule: "tree mutation inside Layout", Detail: "node 7"}
	sentinel := errors.New("tui: recovered panic")

	var got error
	func() {
		defer func() { got = errs.Recovered(sentinel, recover(), "tui: task %d", 7) }()
		panic(want)
	}()

	if got == nil {
		t.Fatal("a recovered panic must produce an error")
	}
	if !errors.Is(got, sentinel) {
		t.Error("the caller's identity must answer")
	}

	var f errs.Fatal
	if !errors.As(got, &f) {
		t.Fatalf("the panicked value must survive; it was rendered into text: %v", got)
	}
	if f != want {
		t.Errorf("recovered %+v, want %+v", f, want)
	}
	if !strings.Contains(got.Error(), "tui: task 7") {
		t.Errorf("the caller's own context must be in the message: %q", got)
	}
}

// A panic value that is not an error has no identity to keep, and must still
// read the same way — the shape of the message cannot depend on what was
// thrown, or a log becomes two formats.
func TestRecovered_NonErrorValueRendersTheSameShape(t *testing.T) {
	sentinel := errors.New("tui: recovered panic")

	var str, wrapped error
	func() {
		defer func() { str = errs.Recovered(sentinel, recover(), "tui: task %d", 7) }()
		panic("controlled")
	}()
	func() {
		defer func() { wrapped = errs.Recovered(sentinel, recover(), "tui: task %d", 7) }()
		panic(errors.New("controlled"))
	}()

	if !errors.Is(str, sentinel) {
		t.Error("a string panic must still carry the caller's identity")
	}
	if !strings.Contains(str.Error(), "controlled") {
		t.Errorf("the panic value must be readable: %q", str)
	}
	if str.Error() != wrapped.Error() {
		t.Errorf("the message shape must not depend on what was thrown:\n string  %q\n error   %q",
			str, wrapped)
	}
}

// Nothing recovered is a real state, not a mistake, so a deferred handler can
// call this unconditionally without asking first.
func TestRecovered_NilIsNil(t *testing.T) {
	sentinel := errors.New("tui: recovered panic")

	var got error
	func() {
		defer func() { got = errs.Recovered(sentinel, recover(), "tui: task %d", 7) }()
		// no panic
	}()
	if got != nil {
		t.Errorf("no panic must produce no error, got %v", got)
	}
}

// The negative control. Without it, every assertion above would pass for a
// function that rendered the value with %v — because the sentinel would still
// answer and the text would still contain the value.
func TestRecovered_TheMistakeItReplacesActuallyLosesTheValue(t *testing.T) {
	want := errs.Fatal{Op: "tui: Mount", Rule: "the rule"}
	sentinel := errors.New("tui: recovered panic")

	flattened := fmt.Errorf("tui: task 7: %v (%w)", want, sentinel)

	if !errors.Is(flattened, sentinel) {
		t.Fatal("the fixture is wrong: the flattened form must still answer the sentinel")
	}
	if !strings.Contains(flattened.Error(), "tui: Mount") {
		t.Fatal("the fixture is wrong: the flattened form must still READ correctly")
	}
	var f errs.Fatal
	if errors.As(flattened, &f) {
		t.Error("the flattened form must NOT be recoverable — if it is, this test " +
			"proves nothing about what Recovered buys")
	}
}
