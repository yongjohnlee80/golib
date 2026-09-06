package rpc

import (
	"errors"
	"strings"
	"testing"
)

// wireError's job is WITHHOLDING: only an *Error's text is public, and anything
// else reaches the peer as a generic internal error.
//
// An earlier version of this file asserted that the fallback map matched
// errInternal's fields — which is tautological now that both are derived from
// the same value, and a mutation proved it: changing errInternal's message left
// the test green, because it changed both sides at once. It could only have
// caught the drift it was written after the refactor removed.
//
// So it asserts the property that is actually load-bearing and cannot be made
// true by construction: a private error's text does not cross the wire, and the
// code the peer sees is the reserved JSON-RPC value.
func TestWireError_WithholdsPrivateDetail(t *testing.T) {
	private := errors.New("dial tcp 10.0.0.7:5432: password authentication failed for user admin")

	w := wireError(private)

	msg, ok := w["message"].(string)
	if !ok {
		t.Fatalf("message is %T, want a string the peer can read", w["message"])
	}
	for _, leak := range []string{"10.0.0.7", "password", "admin"} {
		if strings.Contains(msg, leak) {
			t.Errorf("the wire message leaks %q from a private error: %q", leak, msg)
		}
	}
	if w["code"] != CodeInternal {
		t.Errorf("code = %v, want the reserved CodeInternal %d — the peer decides "+
			"what to do from this number", w["code"], CodeInternal)
	}

	// The other arm: an *Error IS public, and its text must survive, or callers
	// lose the only errors they are allowed to explain.
	pub := &Error{Code: -32001, Message: "unknown method"}
	if got := wireError(pub); got["message"] != "unknown method" || got["code"] != int64(-32001) {
		t.Errorf("a public *Error must cross intact, got %v", got)
	}
}
