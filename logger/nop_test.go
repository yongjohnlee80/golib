package logger

import "testing"

func TestNop(t *testing.T) {
	t.Parallel()

	// Nop must satisfy Logger and never panic, at any severity.
	var l Logger = Nop{}
	for _, s := range []Severity{SeverityDebug, SeverityInfo, SeverityCritical, Severity("Bogus")} {
		l.Log(s, "ignored")
	}
}

func TestNop_AllocFree(t *testing.T) {
	allocs := testing.AllocsPerRun(100, func() {
		Nop{}.Log(SeverityInfo, "x")
	})
	if allocs != 0 {
		t.Errorf("Nop.Log allocs = %v, want 0", allocs)
	}
}
