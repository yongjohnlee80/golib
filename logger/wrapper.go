package logger

import "fmt"

// Contextual wraps a [Logger] and attaches a fixed context to every record, so
// dao logs can carry {dao, request-id, ...} without the call site repeating it
// (parity with ddex-sftp's Wrapper and monstercat's Contextual).
type Contextual struct {
	Logger
	// Context is rendered alongside each payload.
	Context any
}

// NewContextual wraps l so every record it logs is prefixed with ctx.
func NewContextual(l Logger, ctx any) *Contextual {
	return &Contextual{Logger: l, Context: ctx}
}

// Log attaches the context to payload and forwards to the wrapped logger.
func (w *Contextual) Log(severity Severity, payload any) {
	w.Logger.Log(severity, fmt.Sprintf("{%+v} %+v", w.Context, payload))
}
