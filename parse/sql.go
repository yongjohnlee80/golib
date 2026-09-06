package parse

import (
	"bytes"
	"io"
	"strings"
)

// SQL splits and inspects SQL text.
//
// It is a LEXER, not a grammar. It knows exactly enough to find where one
// statement ends and the next begins — which means knowing what a string, an
// identifier and a comment look like — and nothing about what any statement
// means. That boundary is deliberate: the moment a splitter starts recognising
// verbs it starts being wrong about dialects, and the caller who needs to know
// what a statement DOES is better served reading the text than trusting a
// half-grammar to have understood it.
//
// The zero value is usable and handles the lexical syntax common to the major
// engines. Set the fields for the extensions a particular engine adds.
type SQL struct {
	// Backticks treats `like this` as a quoted identifier, as MySQL does.
	// Engines that instead read a backtick as ordinary text leave it off.
	Backticks bool
	// DollarQuotes treats $$…$$ and $tag$…$tag$ as string literals, as
	// PostgreSQL does for function bodies. Without it a dollar sign is
	// ordinary text, and a function body full of semicolons would be split
	// into pieces.
	DollarQuotes bool
	// NestedBlockComments allows /* … /* … */ … */ to nest, as PostgreSQL
	// does. Without it the first */ closes the comment.
	NestedBlockComments bool
}

// Statement is one statement from a script, with its source position.
type Statement struct {
	// Text is the statement without its terminating semicolon, trimmed of
	// surrounding whitespace.
	Text string
	// Pos is where the statement's first character sits in the original
	// source, so a caller can report a line number that matches the file.
	Pos Position
	// Verb is the leading keyword, uppercased — "SELECT", "INSERT", "WITH".
	// It is empty when the statement does not begin with a word.
	//
	// This is the ONE piece of meaning taken from the text, and it is taken
	// only from the first token of an already-split statement, so a keyword
	// appearing inside a string or an identifier cannot produce it. It is a
	// routing hint, not a classification: a caller deciding anything that
	// matters should read the statement.
	Verb string
}

// FormatName implements [Named].
func (SQL) FormatName() string { return "sql" }

// Parse splits src into statements and records each one's position and leading
// keyword. Empty statements — a stray semicolon, trailing whitespace — are
// dropped rather than returned as blanks.
func (s SQL) Parse(src []byte) ([]Statement, error) {
	spans, err := s.split(src)
	if err != nil {
		return nil, err
	}
	out := make([]Statement, 0, len(spans))
	for _, sp := range spans {
		text := strings.TrimSpace(string(src[sp.from:sp.to]))
		if text == "" {
			continue
		}
		out = append(out, Statement{Text: text, Pos: sp.pos, Verb: leadingVerb(text)})
	}
	return out, nil
}

// Split implements [Splitter], returning the raw text of each statement.
//
// This is the cheap path for a caller that only needs the pieces: it does the
// same lexical work as Parse and skips building the statement records.
func (s SQL) Split(src []byte) ([][]byte, error) {
	spans, err := s.split(src)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, 0, len(spans))
	for _, sp := range spans {
		text := bytes.TrimSpace(src[sp.from:sp.to])
		if len(text) == 0 {
			continue
		}
		out = append(out, text)
	}
	return out, nil
}

// Validate implements [Validator], reporting whether the source's strings,
// identifiers and comments are all closed.
//
// It is genuinely cheaper than Parse — it allocates nothing and builds no
// result — which is why this capability is implemented here. It says nothing
// about whether any statement is valid SQL, because this type does not know.
func (s SQL) Validate(src []byte) error {
	_, err := s.split(src)
	return err
}

// ParseStream implements [StreamParser] by reading r to completion.
//
// SQL statements are separated by a terminator that can appear inside strings
// and comments, so nothing can be safely emitted before the text containing it
// has been read. This is offered for callers holding a reader, not as a promise
// of incremental work, and the doc comment says so rather than letting the
// interface imply otherwise.
func (s SQL) ParseStream(r io.Reader) ([]Statement, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return s.Parse(src)
}

// span is one statement's extent in the source.
type span struct {
	from, to int
	pos      Position
}

// split walks the source once, tracking the lexical constructs in which a
// semicolon does NOT end a statement, and records the extent of each statement.
func (s SQL) split(src []byte) ([]span, error) {
	sc := NewScanner(src)
	var out []span
	start := 0
	startPos := sc.Pos()
	started := false

	// note where the next statement begins, the first time real text is seen.
	begin := func(at int, pos Position) {
		if !started {
			start, startPos, started = at, pos, true
		}
	}

	for !sc.Done() {
		at := sc.Pos().Offset
		pos := sc.Pos()

		switch {
		case sc.HasPrefix("--"):
			s.skipLineComment(sc)
			continue
		case sc.HasPrefix("/*"):
			if err := s.skipBlockComment(sc); err != nil {
				return nil, err
			}
			continue
		}

		r, ok := sc.Peek()
		if !ok {
			break
		}

		switch {
		case r == '\'':
			begin(at, pos)
			if err := s.skipQuoted(sc, '\'', true); err != nil {
				return nil, err
			}
			continue
		case r == '"':
			begin(at, pos)
			if err := s.skipQuoted(sc, '"', true); err != nil {
				return nil, err
			}
			continue
		case r == '`' && s.Backticks:
			begin(at, pos)
			if err := s.skipQuoted(sc, '`', false); err != nil {
				return nil, err
			}
			continue
		case r == '$' && s.DollarQuotes:
			if tag, isTag := s.dollarTag(sc); isTag {
				begin(at, pos)
				if err := s.skipDollarQuoted(sc, tag); err != nil {
					return nil, err
				}
				continue
			}
		case r == ';':
			sc.Next()
			if started {
				out = append(out, span{from: start, to: at, pos: startPos})
			} else {
				out = append(out, span{from: at, to: at, pos: pos})
			}
			started = false
			continue
		}

		if !isSpace(r) {
			begin(at, pos)
		}
		sc.Next()
	}

	if started {
		out = append(out, span{from: start, to: len(src), pos: startPos})
	}
	return out, nil
}

// skipLineComment consumes through the end of the line. A line comment that
// runs to the end of the source is closed by the source ending, not an error.
func (s SQL) skipLineComment(sc *Scanner) {
	for {
		r, ok := sc.Next()
		if !ok || r == '\n' {
			return
		}
	}
}

// skipBlockComment consumes a /* … */ comment, nesting when the dialect does.
func (s SQL) skipBlockComment(sc *Scanner) error {
	openedAt := sc.Pos()
	sc.Take("/*")
	depth := 1
	for depth > 0 {
		if sc.Done() {
			return SyntaxError{
				Format: "sql", Pos: openedAt,
				Want: "*/ to close the block comment started here",
				Got:  "end of input", Incomplete: true,
			}
		}
		if s.NestedBlockComments && sc.HasPrefix("/*") {
			sc.Take("/*")
			depth++
			continue
		}
		if sc.HasPrefix("*/") {
			sc.Take("*/")
			depth--
			continue
		}
		sc.Next()
	}
	return nil
}

// skipQuoted consumes a quoted run. doubled says whether the quote character
// repeated inside the run is an escaped quote rather than the end of it, which
// is how SQL escapes quotes in strings and in delimited identifiers.
//
// A backslash is NOT treated as an escape. Standard SQL does not give it that
// meaning, and reading it as an escape would mis-split every statement using a
// Windows path or a regular expression that happens to end in a backslash.
func (s SQL) skipQuoted(sc *Scanner, quote rune, doubled bool) error {
	openedAt := sc.Pos()
	sc.Next()
	for {
		r, ok := sc.Next()
		if !ok {
			return SyntaxError{
				Format: "sql", Pos: openedAt,
				Want: "a closing " + string(quote) + " for the quoted text started here",
				Got:  "end of input", Incomplete: true,
			}
		}
		if r != quote {
			continue
		}
		if doubled {
			if next, ok := sc.Peek(); ok && next == quote {
				sc.Next()
				continue
			}
		}
		return nil
	}
}

// dollarTag reports whether the cursor sits on a dollar-quote opener such as
// $$ or $body$, returning the full delimiter. Nothing is consumed either way:
// a lone dollar sign is ordinary text and must stay readable as such.
func (s SQL) dollarTag(sc *Scanner) (string, bool) {
	var b strings.Builder
	b.WriteByte('$')
	for i := 1; ; i++ {
		r, ok := sc.PeekAt(i)
		if !ok {
			return "", false
		}
		if r == '$' {
			b.WriteByte('$')
			return b.String(), true
		}
		if !isTagRune(r, i == 1) {
			return "", false
		}
		b.WriteRune(r)
	}
}

// skipDollarQuoted consumes a dollar-quoted run up to its matching delimiter.
func (s SQL) skipDollarQuoted(sc *Scanner, tag string) error {
	openedAt := sc.Pos()
	for range tag {
		sc.Next()
	}
	for {
		if sc.Done() {
			return SyntaxError{
				Format: "sql", Pos: openedAt,
				Want: "a closing " + tag + " for the quoted text started here",
				Got:  "end of input", Incomplete: true,
			}
		}
		if sc.Take(tag) {
			return nil
		}
		sc.Next()
	}
}

// leadingVerb returns the uppercased first word of an already-split statement,
// skipping any leading comments and opening parenthesis.
//
// It reads only the first token, so a keyword inside a string or a column name
// cannot become the verb.
func leadingVerb(text string) string {
	rest := strings.TrimSpace(text)
	for {
		switch {
		case strings.HasPrefix(rest, "--"):
			if i := strings.IndexByte(rest, '\n'); i >= 0 {
				rest = strings.TrimSpace(rest[i+1:])
				continue
			}
			return ""
		case strings.HasPrefix(rest, "/*"):
			if i := strings.Index(rest, "*/"); i >= 0 {
				rest = strings.TrimSpace(rest[i+2:])
				continue
			}
			return ""
		}
		break
	}
	end := 0
	for end < len(rest) && isWordByte(rest[end]) {
		end++
	}
	return strings.ToUpper(rest[:end])
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v'
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '_'
}

// isTagRune reports whether r may appear in a dollar-quote tag. A tag may not
// start with a digit, matching the rule for an unquoted identifier.
func isTagRune(r rune, first bool) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		return true
	case r >= '0' && r <= '9':
		return !first
	}
	return false
}
