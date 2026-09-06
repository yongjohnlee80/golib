package partial

import (
	"errors"
	"strings"
)

// ValidationError reports bind-time problems a client can fix: malformed
// JSON, a type mismatch on a field, an invalid $clear shape, or the
// value-AND-clear ambiguity. It is errors.As-able and imports no application
// error types.
type ValidationError struct {
	Fields []FieldError
}

// FieldError is one field-scoped validation problem. Field is the canonical
// field name, or "." for a document-level problem (malformed JSON, wrong
// root type).
type FieldError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	if len(e.Fields) == 0 {
		return "partial: validation failed"
	}
	var b strings.Builder
	b.WriteString("partial: validation failed: ")
	for i, f := range e.Fields {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(f.Field)
		b.WriteString(": ")
		b.WriteString(f.Reason)
	}
	return b.String()
}

// docError builds a document-level ValidationError (field ".").
func docError(reason string) *ValidationError {
	return &ValidationError{Fields: []FieldError{{Field: ".", Reason: reason}}}
}

// ErrUnknownField is the sticky mutator error for a name that is not a field
// of T. It is errors.Is-able. Unlike wire-derived keys (ignored at bind),
// mutator names are developer-authored, so a miss is a bug that must surface.
var ErrUnknownField = errors.New("partial: unknown field")
