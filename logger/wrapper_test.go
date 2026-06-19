package logger

import (
	"strings"
	"testing"
)

func TestContextual_AttachesContext(t *testing.T) {
	t.Parallel()

	r := &recorder{}
	c := NewContextual(r, "reqid=7")
	c.Log(SeverityInfo, "msg")

	if len(r.records) != 1 {
		t.Fatalf("records = %d, want 1", len(r.records))
	}
	if r.records[0].severity != SeverityInfo {
		t.Errorf("severity = %q, want %q", r.records[0].severity, SeverityInfo)
	}
	got, ok := r.records[0].payload.(string)
	if !ok {
		t.Fatalf("payload type = %T, want string", r.records[0].payload)
	}
	if !strings.Contains(got, "reqid=7") || !strings.Contains(got, "msg") {
		t.Errorf("payload = %q, want it to carry both context and message", got)
	}
}
