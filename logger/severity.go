package logger

import (
	"fmt"
	"strings"
)

// Severity classifies a log record. The values match monstercat/golib/logger.Severity
// and ddex-sftp's logger, so records bridge between them losslessly.
type Severity string

const (
	// SeverityDebug is for local development only. When debug logging is off
	// (the default), loggers should ignore records at this severity.
	SeverityDebug Severity = "Debug"

	// SeverityInfo carries useful information about the state or result of an
	// action — typically the request/response status of a served request.
	SeverityInfo Severity = "Info"

	// SeverityNotice is for information that should stand out from other lines:
	// helpful, non-redundant messages.
	SeverityNotice Severity = "Notice"

	// SeverityWarning reports an error that was encountered and recovered from,
	// usually with a preventative signal for possible future errors.
	SeverityWarning Severity = "Warning"

	// SeverityError is for any error that was encountered.
	SeverityError Severity = "Error"

	// SeverityCritical is for system-level failures (file system, out of memory,
	// database, or bootstrap errors) that prevent the application from running.
	SeverityCritical Severity = "Critical"
)

// severityOrder ranks the six known severities from least (Debug) to most
// (Critical) severe. It backs [SimpleLogger]'s MinLevel filtering.
var severityOrder = map[Severity]int{
	SeverityDebug:    0,
	SeverityInfo:     1,
	SeverityNotice:   2,
	SeverityWarning:  3,
	SeverityError:    4,
	SeverityCritical: 5,
}

// rank returns the ordinal of s, or -1 if s is not one of the six known
// severities. Unknown severities rank -1 so they are never dropped by a
// MinLevel filter.
func rank(s Severity) int {
	if r, ok := severityOrder[s]; ok {
		return r
	}
	return -1
}

// ParseSeverity is the severity named s, one of the six known ones in any case
// ("debug", "Warning", "ERROR"), surrounding spaces ignored: for a log-level flag
// or setting, which [WithMinLevel] then applies. Any other name is an error that
// lists the six.
func ParseSeverity(s string) (Severity, error) {
	t := strings.TrimSpace(s)
	for _, known := range []Severity{SeverityDebug, SeverityInfo, SeverityNotice, SeverityWarning, SeverityError, SeverityCritical} {
		if strings.EqualFold(t, string(known)) {
			return known, nil
		}
	}
	return "", fmt.Errorf("logger: unknown severity %q: want debug, info, notice, warning, error or critical", s)
}

// InList reports whether needle is present in haystack (parity with
// monstercat's helper of the same name).
func InList(haystack []Severity, needle Severity) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
