package logger

// Adapt turns a logging function into a [Logger]. Use it to bridge any external
// logger (for example monstercat/golib/logger.Logger) without golib depending on
// it — the consumer supplies the closure and owns the external import.
func Adapt(fn func(severity Severity, payload any)) Logger {
	return adapterFunc(fn)
}

// adapterFunc is a Logger backed by a plain function.
type adapterFunc func(severity Severity, payload any)

// Log calls the underlying function.
func (f adapterFunc) Log(severity Severity, payload any) { f(severity, payload) }
