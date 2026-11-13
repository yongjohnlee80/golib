package diff

import "strings"

// Ending is a text's line ending.
type Ending uint8

const (
	// LF is "\n", and a text with no line break at all.
	LF Ending = iota
	// CRLF is "\r\n".
	CRLF
	// CR is a lone "\r".
	CR
	// Mixed is more than one of them: each line's own is not kept, and a Join writes the first one
	// the text had.
	Mixed
)

// String is the ending as it is written: "\n", "\r\n" or "\r"; Mixed has none of its own.
func (e Ending) String() string {
	switch e {
	case CRLF:
		return "\r\n"
	case CR:
		return "\r"
	case Mixed:
		return ""
	}
	return "\n"
}

// Text is a text split into lines, without their endings, and how it ended them.
type Text struct {
	Lines []string
	// Ending is the text's line ending; FinalNewline whether its last line ends with one.
	Ending       Ending
	FinalNewline bool
	// first is the first ending a Mixed text had: what Join writes for it.
	first Ending
}

// Split is s as lines, its ending and final newline kept. An empty text has no lines; a text of
// one line break has one, empty.
func Split(s string) Text {
	t := Text{Lines: []string{}}
	seen := map[Ending]bool{}
	start := 0
	for i := 0; i < len(s); i++ {
		var e Ending
		var n int
		switch {
		case s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n':
			e, n = CRLF, 2
		case s[i] == '\r':
			e, n = CR, 1
		case s[i] == '\n':
			e, n = LF, 1
		default:
			continue
		}
		if len(seen) == 0 {
			t.first = e
		}
		seen[e] = true
		t.Lines = append(t.Lines, s[start:i])
		i += n - 1
		start = i + 1
	}
	if start < len(s) {
		t.Lines = append(t.Lines, s[start:])
	} else if len(s) > 0 {
		t.FinalNewline = true
	}
	switch len(seen) {
	case 0:
		t.Ending = LF
	case 1:
		t.Ending = t.first
	default:
		t.Ending = Mixed
	}
	return t
}

// Join is lines as text with t's ending and final newline: a Mixed text's first ending. With no
// lines it is empty.
func (t Text) Join(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	e := t.Ending
	if e == Mixed {
		e = t.first
	}
	sep := e.String()
	out := strings.Join(lines, sep)
	if t.FinalNewline {
		out += sep
	}
	return out
}

// String is t whole again: Join of its own lines.
func (t Text) String() string { return t.Join(t.Lines) }
