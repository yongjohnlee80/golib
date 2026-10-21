package auth

import (
	"errors"
	"strings"
	"testing"
)

// auditDetail records an error that asserts a safe text by that text, de-controlled and cut to
// maxAuditDetail; any other error by its type alone; and nothing for no error.
func TestAuditDetailRecordsOnlyWhatIsSafe(t *testing.T) {
	if got := auditDetail(nil); got != "" {
		t.Errorf("auditDetail(nil) = %q, want empty", got)
	}
	if got := auditDetail(errors.New("token s3cret")); got != "opaque error of type *errors.errorString" {
		t.Errorf("an unasserted error recorded %q, want its type only", got)
	}
	long := Reason("auth: a\nforged line " + strings.Repeat("x", 300))
	got := auditDetail(long)
	if strings.ContainsAny(got, "\n") {
		t.Errorf("a control character survived: %q", got)
	}
	if !strings.HasPrefix(got, "auth: a?forged line ") || len(got) != maxAuditDetail+len("…") || !strings.HasSuffix(got, "…") {
		t.Errorf("a long safe detail recorded %d bytes (%q…), want %d and an ellipsis", len(got), got[:30], maxAuditDetail)
	}
}
