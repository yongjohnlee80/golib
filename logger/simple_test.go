package logger

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// captureLog redirects the standard logger to a buffer for the duration of fn,
// stripping flags so output is deterministic. Not parallel-safe (mutates the
// global standard logger), so its callers do not call t.Parallel.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	oldOut, oldFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldOut)
		log.SetFlags(oldFlags)
	}()
	fn()
	return buf.String()
}

func TestSimpleLogger_Format(t *testing.T) {
	out := captureLog(t, func() {
		NewLogger("ctx").Log(SeverityInfo, "hello")
	})
	if want := "[Info] {ctx} hello\n"; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
}

func TestSimpleLogger_MinLevel(t *testing.T) {
	out := captureLog(t, func() {
		l := &SimpleLogger{MinLevel: SeverityWarning}
		l.Log(SeverityInfo, "drop-me")  // below MinLevel
		l.Log(SeverityError, "keep-me") // at/above MinLevel
	})
	if strings.Contains(out, "drop-me") {
		t.Errorf("Info logged despite MinLevel=Warning: %q", out)
	}
	if !strings.Contains(out, "keep-me") {
		t.Errorf("Error dropped despite MinLevel=Warning: %q", out)
	}
}

func TestSimpleLogger_MinLevelEmptyLogsAll(t *testing.T) {
	out := captureLog(t, func() {
		(&SimpleLogger{}).Log(SeverityDebug, "debug-line")
	})
	if !strings.Contains(out, "debug-line") {
		t.Errorf("empty MinLevel should log everything, got %q", out)
	}
}

func TestSimpleLogger_BlockList(t *testing.T) {
	out := captureLog(t, func() {
		l := &SimpleLogger{BlockList: []Severity{SeverityDebug}}
		l.Log(SeverityDebug, "blocked")
		l.Log(SeverityInfo, "allowed")
	})
	if strings.Contains(out, "blocked") {
		t.Errorf("blocked severity was logged: %q", out)
	}
	if !strings.Contains(out, "allowed") {
		t.Errorf("allowed severity was dropped: %q", out)
	}
}

func TestNew_WithWriterIsInjectable(t *testing.T) {
	t.Parallel() // no global state: the writer is injected
	var buf bytes.Buffer
	l := New(WithWriter(&buf), WithContext("api"), WithMinLevel(SeverityInfo))

	Debug(l, "dropped")
	Info(l, "kept")

	out := buf.String()
	if strings.Contains(out, "dropped") {
		t.Error("MinLevel filter did not drop debug")
	}
	if !strings.Contains(out, "kept") || !strings.Contains(out, "{api}") {
		t.Errorf("output = %q", out)
	}
}
