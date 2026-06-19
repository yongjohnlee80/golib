package logger

// Nop is a [Logger] that discards everything. It is the dao package's default so
// omitting a logger is always safe, nil-free, and allocation-free.
type Nop struct{}

// Log discards the record.
func (Nop) Log(Severity, any) {}
