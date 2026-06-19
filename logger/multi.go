package logger

// Multi forwards each record to every wrapped [Logger] (parity with monstercat's
// Multi). A nil entry is skipped.
type Multi struct {
	Loggers []Logger
}

// NewMulti creates a [Multi] over the given loggers.
func NewMulti(loggers ...Logger) *Multi {
	return &Multi{Loggers: loggers}
}

// Log forwards severity and payload to each wrapped logger in order.
func (m *Multi) Log(severity Severity, payload any) {
	for _, l := range m.Loggers {
		if l != nil {
			l.Log(severity, payload)
		}
	}
}
