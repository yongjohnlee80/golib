package logger

import "fmt"

// Logger logs a payload at a severity. This is the surface the dao package emits
// to. It is intentionally identical in shape to monstercat/golib/logger.Logger so
// the two interoperate via a trivial adapter (see [Adapt]).
type Logger interface {
	Log(severity Severity, payload any)
}

// Debug logs payload at [SeverityDebug].
func Debug(l Logger, payload any) { l.Log(SeverityDebug, payload) }

// Info logs payload at [SeverityInfo].
func Info(l Logger, payload any) { l.Log(SeverityInfo, payload) }

// Notice logs payload at [SeverityNotice].
func Notice(l Logger, payload any) { l.Log(SeverityNotice, payload) }

// Warning logs err and payload at [SeverityWarning]. A nil err logs payload alone.
func Warning(l Logger, err error, payload any) { l.Log(SeverityWarning, mergeErr(err, payload)) }

// Error logs err and payload at [SeverityError]. A nil err logs payload alone.
func Error(l Logger, err error, payload any) { l.Log(SeverityError, mergeErr(err, payload)) }

// Critical logs err and payload at [SeverityCritical]. A nil err logs payload alone.
func Critical(l Logger, err error, payload any) { l.Log(SeverityCritical, mergeErr(err, payload)) }

// Entry pairs an error with its payload for the error-bearing level helpers.
// It implements error and unwraps to Err, so sinks (and tests) can inspect the
// original chain with errors.Is/As, and structured backends (the slog bridge)
// can destructure the pair instead of parsing a flattened string.
type Entry struct {
	Err     error
	Payload any
}

// Error renders the entry as "err: payload" (or just the error when there is
// no payload). fmt verbs %v/%s/%+v use this via the error interface.
func (e Entry) Error() string {
	if e.Payload == nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("%v: %+v", e.Err, e.Payload)
}

// Unwrap exposes the underlying error to errors.Is / errors.As.
func (e Entry) Unwrap() error { return e.Err }

// mergeErr combines err and payload into a single payload for the error-bearing
// level helpers. When err is nil it returns payload unchanged; otherwise it
// returns an [Entry], which renders like the old flattened string but keeps the
// error chain intact for errors.Is/As and structured sinks.
func mergeErr(err error, payload any) any {
	if err == nil {
		return payload
	}
	return Entry{Err: err, Payload: payload}
}
