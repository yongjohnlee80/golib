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

// mergeErr combines err and payload into a single payload for the error-bearing
// level helpers. When err is nil it returns payload unchanged; otherwise it wraps
// err (so the chain is preserved by errors.Is/As at the call site that builds it)
// and renders the pair as a string, matching ddex-sftp's helper behavior.
func mergeErr(err error, payload any) any {
	if err == nil {
		return payload
	}
	return fmt.Errorf("%w: %+v", err, payload).Error()
}
