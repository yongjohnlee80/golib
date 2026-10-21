package web

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui/term"
)

// Both backends' "stopped" errors are errs.ErrClosed, so code holding only a tui.Backend can ask
// whether a Flush or Size error means stopped.
func TestBothBackendsStoppedErrorsAreErrClosed(t *testing.T) {
	for name, err := range map[string]error{"web.ErrStopped": ErrStopped, "term.ErrClosed": term.ErrClosed} {
		if !errors.Is(err, errs.ErrClosed) {
			t.Errorf("%s is not an errs.ErrClosed: %v", name, err)
		}
	}
}

// Every HandoffReason is declared once, in order, with a name of its own: a value added by hand
// in another file could collide and still compile.
func TestHandoffReasonsAreOneEnum(t *testing.T) {
	want := []string{"reattached-existing", "attach-failed", "expired", "session-ended", "login-failed"}
	for i, r := range []HandoffReason{ReattachedExisting, AttachFailed, Expired, SessionEnded, LoginFailed} {
		if int(r) != i || r.String() != want[i] {
			t.Errorf("reason %d is %d %q, want %d %q", i, r, r, i, want[i])
		}
	}
	if HandoffReason(len(want)).String() != "unknown" {
		t.Error("a reason past the last has a name")
	}
}

// randomToken is n random bytes, URL-safe and unpadded, and two are not alike.
func TestRandomTokenIsURLSafeAndRandom(t *testing.T) {
	a, b := randomToken(16), randomToken(16)
	if len(a) != 22 || a == b {
		t.Errorf("tokens %q and %q: want 22 characters (16 bytes, unpadded) and distinct", a, b)
	}
	for _, c := range a {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			t.Errorf("token %q holds %q, not URL-safe", a, c)
		}
	}
}
