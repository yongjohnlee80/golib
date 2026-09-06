package parse

// Kind says what a run of bytes IS, never what it means.
//
// The distinction is the whole reason a keyword set does not appear in this
// package. `SELECT` is a Word here; that it is a verb in one dialect, a column
// name in another, and reserved in a third is a judgement the layer above
// makes, with the verbatim bytes still in front of it. A lexer that decided
// would have taken a dialect's position and destroyed the text a grammar tree
// needs to see.
type Kind uint8

const (
	Invalid    Kind = iota // the zero value is never a real token
	Word                   // a bare run of identifier bytes; NOT a keyword
	Ident                  // a quoted or otherwise delimited identifier
	String                 // a literal
	Number                 // a numeric literal
	Operator               // a symbolic operator
	Punct                  // structural punctuation
	Comment                // trivia, EMITTED not dropped
	Space                  // trivia, EMITTED not dropped
	Terminator             // a statement separator
	EOF                    // end of input; a real token with a real position
)

var kindNames = [...]string{
	Invalid:    "Invalid",
	Word:       "Word",
	Ident:      "Ident",
	String:     "String",
	Number:     "Number",
	Operator:   "Operator",
	Punct:      "Punct",
	Comment:    "Comment",
	Space:      "Space",
	Terminator: "Terminator",
	EOF:        "EOF",
}

// String names the kind. Out-of-range values report themselves rather than
// panicking: a Kind arrives from a caller-written Form, so it is data, not an
// invariant of this package.
func (k Kind) String() string {
	if int(k) < len(kindNames) && kindNames[k] != "" {
		return kindNames[k]
	}
	return "Kind(" + itoa(uint8(k)) + ")"
}

func itoa(v uint8) string {
	if v == 0 {
		return "0"
	}
	var b [3]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = '0' + v%10
		v /= 10
	}
	return string(b[i:])
}
