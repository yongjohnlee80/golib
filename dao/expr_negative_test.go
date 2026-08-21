package dao

import (
	"os/exec"
	"strings"
	"testing"
)

// TestExpr_AltRejectionsAreCompileErrors is the negative half of ADR-0016 §2.3.
// The Alt constraint's whole point is that a bad fallback fails at BUILD time,
// which no ordinary test can observe — so this one compiles a fixture that must
// not build and asserts each rejection by name.
//
// The fixture lives under testdata/ so the go tool skips it for every package
// pattern; only this test names it explicitly. It costs one toolchain
// invocation, which is why it skips under -short.
func TestExpr_AltRejectionsAreCompileErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the toolchain-invoking negative-typing check in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}

	out, err := exec.Command("go", "build", "-o", "/dev/null", "./testdata/negative/").CombinedOutput()
	if err == nil {
		t.Fatalf("testdata/negative built successfully — the Alt constraint has been widened:\n%s", out)
	}

	// Every case must be rejected, and each for the right reason.
	for _, want := range []string{
		"float64 does not satisfy dao.Alt",
		"bool does not satisfy dao.Alt",
		"artistField does not satisfy dao.Alt", // a ~string field enum: the tilde trap
		"struct{} does not satisfy dao.Alt",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("compiler output does not reject %q:\n%s", want, out)
		}
	}
}
