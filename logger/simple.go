package logger

import "log"

// SimpleLogger is a basic CLI/stdlib logger. It writes each record to the
// standard logger, prefixed with the severity and an optional context. It
// satisfies [Logger].
type SimpleLogger struct {
	// Context is attached to every record, e.g. a request id or component name.
	Context any

	// MinLevel drops records whose severity ranks below it (e.g. set to
	// [SeverityInfo] to suppress Debug in production). The zero value ("")
	// disables the filter. Severities outside the known six are never dropped.
	MinLevel Severity

	// BlockList suppresses specific severities outright (parity with
	// monstercat's Standard logger).
	BlockList []Severity
}

// NewLogger creates a [SimpleLogger] with the given context and no filtering.
func NewLogger(ctx any) Logger {
	return &SimpleLogger{Context: ctx}
}

// Log writes the record to the standard logger unless it is filtered out by
// MinLevel or BlockList.
func (l *SimpleLogger) Log(severity Severity, payload any) {
	if l.suppressed(severity) {
		return
	}
	log.Printf("[%s] {%+v} %+v", severity, l.Context, payload)
}

// suppressed reports whether severity is filtered out by MinLevel or BlockList.
func (l *SimpleLogger) suppressed(severity Severity) bool {
	if InList(l.BlockList, severity) {
		return true
	}
	if l.MinLevel == "" {
		return false
	}
	r, min := rank(severity), rank(l.MinLevel)
	// Only filter when both severities are known; unknown severities pass.
	return r >= 0 && min >= 0 && r < min
}
