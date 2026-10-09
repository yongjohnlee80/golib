package indent

// Operation is the editing gesture for which indentation is requested.
type Operation uint8

const (
	// Newline splits the current line at Column.
	Newline Operation = iota
	// OpenBelow opens a new line following the current line.
	OpenBelow
	// OpenAbove opens a new line preceding the current line.
	OpenAbove
	// Closing aligns a completed closing token on the current line.
	Closing
)

// Request describes one editing decision. Column counts bytes, not graphemes.
// PreviousState is opaque and belongs to the selected source instance.
type Request struct {
	Line          string
	Column        int
	Operation     Operation
	Unit          string
	PreviousState int
	StateKnown    bool
}

// Decision chooses whitespace without changing any source text. SplitClosing
// makes a blank body line followed by an already present matching closer.
type Decision struct {
	Prefix        string
	SplitClosing  bool
	ClosingPrefix string
}

// Policy supplies indentation for a source language. False declines adjustment.
// A policy must not interpret structural markers with an unknown incoming state.
type Policy interface {
	Indent(Request) (Decision, bool)
}

// PolicyFunc implements Policy as a function.
type PolicyFunc func(Request) (Decision, bool)

// Indent implements Policy.
func (f PolicyFunc) Indent(r Request) (Decision, bool) { return f(r) }

// Whitespace reports whether a string consists exclusively of spaces and tabs.
func Whitespace(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}

// Leading is a line's existing space/tab prefix.
func Leading(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i]
}
