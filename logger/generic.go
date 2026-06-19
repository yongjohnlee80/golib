package logger

// GenericLogger mirrors ddex-sftp's logger.Logger[SeverityType]. It is provided
// for callers that want to parameterize the severity type. The dao package uses
// the non-generic [Logger] so its Schema/DAO signatures do not grow an extra type
// parameter.
type GenericLogger[S any] interface {
	Log(severity S, payload any)
}
