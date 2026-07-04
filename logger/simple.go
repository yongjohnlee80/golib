package logger

import (
	"io"
	"log"
)

// SimpleLogger is a basic CLI/stdlib logger. It writes each record to an
// injectable writer (default: the process-standard logger), prefixed with the
// severity and an optional context. It satisfies [Logger]. Construct with
// [New]; the exported fields remain settable for monstercat-parity call sites.
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

	// out is the record sink; nil means the global stdlib logger (log.Printf).
	out *log.Logger
}

// Option configures a [SimpleLogger] built by [New].
type Option func(*SimpleLogger)

// WithContext attaches ctx (e.g. a component name) to every record.
func WithContext(ctx any) Option { return func(l *SimpleLogger) { l.Context = ctx } }

// WithMinLevel drops records ranking below s.
func WithMinLevel(s Severity) Option { return func(l *SimpleLogger) { l.MinLevel = s } }

// WithBlockList suppresses the given severities outright.
func WithBlockList(s ...Severity) Option { return func(l *SimpleLogger) { l.BlockList = s } }

// WithWriter directs records to w instead of the global stdlib logger, making
// the logger testable and composable (no global log.SetOutput needed).
func WithWriter(w io.Writer) Option {
	return func(l *SimpleLogger) { l.out = log.New(w, "", log.LstdFlags) }
}

// New builds a [SimpleLogger] from functional options. With no options it
// writes to the global stdlib logger with no filtering.
func New(opts ...Option) *SimpleLogger {
	l := &SimpleLogger{}
	for _, opt := range opts {
		if opt != nil {
			opt(l)
		}
	}
	return l
}

// NewLogger creates a [SimpleLogger] with the given context and no filtering.
// Kept for monstercat-parity; [New] with options is the canonical constructor.
func NewLogger(ctx any) *SimpleLogger {
	return &SimpleLogger{Context: ctx}
}

// Log writes the record to the configured writer (or the standard logger)
// unless it is filtered out by MinLevel or BlockList.
func (l *SimpleLogger) Log(severity Severity, payload any) {
	if l.suppressed(severity) {
		return
	}
	if l.out != nil {
		l.out.Printf("[%s] {%+v} %+v", severity, l.Context, payload)
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
