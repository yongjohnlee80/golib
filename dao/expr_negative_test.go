package dao

import (
	"os/exec"
	"strings"
	"testing"
)

// TestExpr_CoalesceRejectionsAreCompileErrors is the negative half of the
// expression surface: what must NOT compile.
//
// Coalesce takes two Expr values, so every bad fallback is a BUILD error — which
// no ordinary test can observe, so this one compiles a fixture that must not
// build and asserts each rejection by name. This is what replaced a runtime
// panic: the string overload accepted `Coalesce(col, "it's")` and blew up at
// render time; now it does not compile.
//
// The fixture lives under testdata/ so the go tool skips it for every package
// pattern; only this test names it explicitly. It costs one toolchain
// invocation, which is why it skips under -short.
func TestExpr_CoalesceRejectionsAreCompileErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the toolchain-invoking negative-typing check in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}

	out, err := exec.Command("go", "build", "-o", "/dev/null", "./testdata/negative/").CombinedOutput()
	if err == nil {
		t.Fatalf("testdata/negative built successfully — Coalesce has been widened past Expr:\n%s", out)
	}

	// Every case must be rejected, and each for the right reason.
	for _, want := range []string{
		// Each names the TYPE that was offered, so a widening shows up as a
		// missing rejection rather than a silently different error.
		"cannot use \"n/a\"",
		"cannot use 0",
		"cannot use 0.5",
		"cannot use true",
		"cannot use aName",
		"cannot use struct{}{}",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("compiler output does not reject %q:\n%s", want, out)
		}
	}
}
