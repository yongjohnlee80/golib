package logger

import "testing"

func TestAdapt(t *testing.T) {
	t.Parallel()

	var gotS Severity
	var gotP any
	l := Adapt(func(s Severity, p any) { gotS, gotP = s, p })
	l.Log(SeverityNotice, "hi")

	if gotS != SeverityNotice {
		t.Errorf("severity = %q, want %q", gotS, SeverityNotice)
	}
	if gotP != "hi" {
		t.Errorf("payload = %v, want %q", gotP, "hi")
	}
}

// extSeverity / extLogger stand in for an external logger with its OWN distinct
// Severity string type (e.g. monstercat/golib/logger). The point of this test is
// that golib bridges to it via Adapt without importing it, and the string cast is
// lossless because the severity values are identical.
type extSeverity string

type extLogger struct {
	got []extSeverity
}

func (e *extLogger) Log(s extSeverity, _ any) { e.got = append(e.got, s) }

func TestAdapt_BridgesExternalLogger(t *testing.T) {
	t.Parallel()

	ext := &extLogger{}
	bridged := Adapt(func(s Severity, p any) { ext.Log(extSeverity(s), p) })

	bridged.Log(SeverityError, "boom")

	if len(ext.got) != 1 || ext.got[0] != extSeverity("Error") {
		t.Errorf("external logger got %v, want [Error] (lossless cast)", ext.got)
	}
}
