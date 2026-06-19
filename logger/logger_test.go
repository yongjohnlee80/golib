package logger

import (
	"errors"
	"strings"
	"testing"
)

// record captures one logged record for assertions.
type record struct {
	severity Severity
	payload  any
}

// recorder is a test [Logger] that remembers every record it receives.
type recorder struct {
	records []record
}

func (r *recorder) Log(s Severity, p any) {
	r.records = append(r.records, record{severity: s, payload: p})
}

func TestLevelHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(l Logger)
		want Severity
	}{
		{"Debug", func(l Logger) { Debug(l, "p") }, SeverityDebug},
		{"Info", func(l Logger) { Info(l, "p") }, SeverityInfo},
		{"Notice", func(l Logger) { Notice(l, "p") }, SeverityNotice},
		{"Warning", func(l Logger) { Warning(l, nil, "p") }, SeverityWarning},
		{"Error", func(l Logger) { Error(l, nil, "p") }, SeverityError},
		{"Critical", func(l Logger) { Critical(l, nil, "p") }, SeverityCritical},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &recorder{}
			tt.call(r)
			if len(r.records) != 1 {
				t.Fatalf("records = %d, want 1", len(r.records))
			}
			if r.records[0].severity != tt.want {
				t.Errorf("severity = %q, want %q", r.records[0].severity, tt.want)
			}
		})
	}
}

func TestErrorHelpers_WrapErr(t *testing.T) {
	t.Parallel()

	err := errors.New("boom")
	r := &recorder{}
	Error(r, err, "ctx")

	got, ok := r.records[0].payload.(string)
	if !ok {
		t.Fatalf("payload type = %T, want string", r.records[0].payload)
	}
	if !strings.Contains(got, "boom") || !strings.Contains(got, "ctx") {
		t.Errorf("payload = %q, want it to contain both the error and the payload", got)
	}
}

func TestErrorHelpers_NilErrPassesPayload(t *testing.T) {
	t.Parallel()

	r := &recorder{}
	Warning(r, nil, "just-payload")
	if r.records[0].payload != "just-payload" {
		t.Errorf("payload = %v, want %q", r.records[0].payload, "just-payload")
	}
}
