// Package logger provides a small, zero-dependency, level-based logging hook.
//
// The package exists so other golib packages — notably the dao package — can
// accept optional, toggleable logging without pulling in an external logging
// framework. Its only import is the standard library log package.
//
// # The interface
//
// [Logger] is intentionally shape-identical to monstercat/golib/logger.Logger:
//
//	Log(severity Severity, payload any)
//
// [Severity] is a string with the same six values monstercat and ddex-sftp use
// (Debug, Info, Notice, Warning, Error, Critical), so log records bridge between
// the two vocabularies losslessly.
//
// # Implementations
//
//   - [Nop]: discards everything; the safe, allocation-free default.
//   - [SimpleLogger]: writes to the standard logger with severity, context, and
//     optional MinLevel / BlockList filtering.
//   - [Multi]: fans a record out to several loggers.
//   - [Contextual]: wraps a logger and attaches a fixed context to every record.
//
// # Bridging an external logger
//
// golib never imports monstercat. [Adapt] turns a plain function into a [Logger],
// so a consumer that imports both packages can bridge its logger in three lines:
//
//	func bridge(l mclog.Logger) logger.Logger {
//		return logger.Adapt(func(s logger.Severity, p any) { l.Log(mclog.Severity(s), p) })
//	}
//
// # Level helpers
//
// [Debug], [Info], [Notice], [Warning], [Error], and [Critical] are ergonomic
// call-site helpers that route a payload to the matching severity.
//
// # Generic variant
//
// [GenericLogger] mirrors ddex-sftp's parameterized logger for callers that want
// a custom severity type. The dao package uses the non-generic [Logger].
package logger
