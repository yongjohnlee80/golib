package sql

import (
	"github.com/yongjohnlee80/golib/parse"

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
// One opt-in reads words, because the engine itself does: TriggerBodies, where
// SQLite decides whether a CREATE TRIGGER is complete by five keywords and
// nothing else (sqlite3_complete). The splitter reads those five, exactly as
// SQLite reads them, and no others.
//
// The zero value is usable and handles the lexical syntax common to the major
// engines. Build one with [New] and the options for the extensions a
// particular engine adds; the fields below are the older way to set them.
type SQL struct {
	// Backticks treats `like this` as a quoted identifier, as MySQL does.
	// Engines that instead read a backtick as ordinary text leave it off.
	//
	// Deprecated: configure with [Backticks] and [New]. The field remains for existing
	// callers, which are moving to the options.
	Backticks bool
	// DollarQuotes treats $$…$$ and $tag$…$tag$ as string literals, as
	// PostgreSQL does for function bodies. Without it a dollar sign is
	// ordinary text, and a function body full of semicolons would be split
	// into pieces.
	//
	// Deprecated: configure with [DollarQuotes] and [New]. The field remains for existing
	// callers, which are moving to the options.
	DollarQuotes bool
	// NestedBlockComments allows /* … /* … */ … */ to nest, as PostgreSQL
	// does. Without it the first */ closes the comment.
	//
	// Deprecated: configure with [NestedBlockComments] and [New]. The field remains for
	// existing callers, which are moving to the options.
	NestedBlockComments bool
	// EStringEscapes treats a string written E'…' as one in which a backslash
	// escapes the next character, as PostgreSQL does.
	//
	// The opt-in is the PREFIX, not the construct: an ordinary '…' keeps the
	// standard reading in which a backslash is just a character, so a Windows
	// path or a regular expression ending in one still closes where it should.
	// Turning this on changes only strings that asked for it by carrying the E.
	//
	// Deprecated: configure with [EStringEscapes] and [New]. The field remains for existing
	// callers, which are moving to the options.
	EStringEscapes bool
	// TriggerBodies reads CREATE [TEMP] TRIGGER … BEGIN … END as one
	// statement, as SQLite does: the semicolons that end the statements of a
	// trigger's body do not end the CREATE TRIGGER. Without it such a trigger
	// is split into pieces no engine accepts.
	//
	// The reading is sqlite3_complete()'s, token for token (SQLite's
	// complete.c): after CREATE, an optional TEMP or TEMPORARY, and TRIGGER,
	// a semicolon ends the statement only when END is the token before it and
	// a semicolon came before that END. Words match without regard to ASCII
	// case, as SQLite compares keywords (TRıGGER is not TRIGGER); a string, a
	// quoted identifier or a comment is never one of these words.
	//
	// Deprecated: configure with [TriggerBodies] and [New]. The field remains for existing
	// callers, which are moving to the options.
	TriggerBodies bool
}

// Statement is one statement from a script, with its source position.
type Statement struct {
	// Text is the statement without its terminating semicolon, trimmed of
	// surrounding whitespace.
	Text string
	// Pos is where the statement's first character sits in the original
	// source, so a caller can report a line number that matches the file.
	Pos parse.Position
	// Verb is the leading keyword, uppercased — "SELECT", "INSERT", "WITH".
	// It is empty when the statement does not begin with a word.
	//
	// This is the one piece of meaning a Statement carries, and it is taken
	// only from the first token of an already-split statement, so a keyword
	// appearing inside a string or an identifier cannot produce it. It is a
	// routing hint, not a classification: a caller deciding anything that
	// matters should read the statement.
	Verb string
}

// FormatName implements [parse.Named].
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
// Split is Parse without the record building: the SAME single walk, with
// cheaper output. They are one grammar and cannot drift apart, which is worth
// saying because two exported entry points usually mean two implementations.
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
	pos      parse.Position
}

// split walks the source once, tracking the lexical constructs in which a
// semicolon does NOT end a statement, and records the extent of each statement.
func (s SQL) split(src []byte) ([]span, error) {
	sc := parse.NewScanner(src)
	var out []span
	start := 0
	startPos := sc.Pos()
	started := false

	// note where the next statement begins, the first time real text is seen.
	begin := func(at int, pos parse.Position) {
		if !started {
			start, startPos, started = at, pos, true
		}
	}

	// trig is where the statement stands in sqlite3_complete's reading, for
	// TriggerBodies; inBody says a semicolon inside a trigger's body was seen,
	// so the statement is open until its END;.
	trig, inBody := trigStart, false
	feed := func(tok trigToken) {
		if s.TriggerBodies {
			trig = trigNext[trig][tok]
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
			feed(trigOther)
			if err := s.skipQuoted(sc, '\'', true, false); err != nil {
				return nil, err
			}
			continue
		case r == '"':
			begin(at, pos)
			feed(trigOther)
			if err := s.skipQuoted(sc, '"', true, false); err != nil {
				return nil, err
			}
			continue
		case r == '`' && s.Backticks:
			begin(at, pos)
			feed(trigOther)
			if err := s.skipQuoted(sc, '`', false, false); err != nil {
				return nil, err
			}
			continue
		case (r == 'E' || r == 'e') && s.EStringEscapes && s.startsEString(src, at):
			begin(at, pos)
			feed(trigOther)
			sc.Next()
			if err := s.skipQuoted(sc, '\'', true, true); err != nil {
				return nil, err
			}
			continue
		case r == '$' && s.DollarQuotes:
			if tag, isTag := s.dollarTag(sc); isTag {
				begin(at, pos)
				feed(trigOther)
				if err := s.skipDollarQuoted(sc, tag); err != nil {
					return nil, err
				}
				continue
			}
		case r == ';':
			sc.Next()
			if s.TriggerBodies && trigNext[trig][trigSemi] == trigBodySemi {
				// a statement of the trigger's body ended, not the trigger
				trig, inBody = trigBodySemi, true
				continue
			}
			if started {
				out = append(out, span{from: start, to: at, pos: startPos})
			} else {
				out = append(out, span{from: at, to: at, pos: pos})
			}
			started = false
			trig, inBody = trigStart, false
			continue
		case s.TriggerBodies && isSQLiteIDRune(r):
			begin(at, pos)
			var w strings.Builder
			for !sc.Done() {
				c, _ := sc.Peek()
				if !isSQLiteIDRune(c) {
					break
				}
				w.WriteRune(c)
				sc.Next()
			}
			feed(trigWord(w.String()))
			continue
		}

		if !isSpace(r) {
			begin(at, pos)
			feed(trigOther)
		}
		sc.Next()
	}

	if s.TriggerBodies && inBody && trig != trigEnd {
		return nil, parse.SyntaxError{
			Format: "sql", Pos: startPos,
			Want: "END; to close the body of the trigger created here",
			Got:  "end of input", Incomplete: true,
		}
	}
	if started {
		out = append(out, span{from: start, to: len(src), pos: startPos})
	}
	return out, nil
}

// The states and tokens of sqlite3_complete (SQLite's complete.c), for
// TriggerBodies. The table is that file's, row for row; whitespace and
// comments are its WS token, which changes no state here and so is not fed.
type trigState uint8

const (
	trigInvalid trigState = iota
	trigStart
	trigNormal
	trigExplain
	trigCreate
	trigTrigger
	trigBodySemi
	trigEnd
)

type trigToken uint8

const (
	trigSemi trigToken = iota
	trigWS
	trigOther
	trigExplainTok
	trigCreateTok
	trigTempTok
	trigTriggerTok
	trigEndTok
)

var trigNext = [8][8]trigState{
	//                  SEMI          WS            OTHER        EXPLAIN      CREATE       TEMP         TRIGGER      END
	trigInvalid:  {trigStart, trigInvalid, trigNormal, trigExplain, trigCreate, trigNormal, trigNormal, trigNormal},
	trigStart:    {trigStart, trigStart, trigNormal, trigExplain, trigCreate, trigNormal, trigNormal, trigNormal},
	trigNormal:   {trigStart, trigNormal, trigNormal, trigNormal, trigNormal, trigNormal, trigNormal, trigNormal},
	trigExplain:  {trigStart, trigExplain, trigExplain, trigNormal, trigCreate, trigNormal, trigNormal, trigNormal},
	trigCreate:   {trigStart, trigCreate, trigNormal, trigNormal, trigNormal, trigCreate, trigTrigger, trigNormal},
	trigTrigger:  {trigBodySemi, trigTrigger, trigTrigger, trigTrigger, trigTrigger, trigTrigger, trigTrigger, trigTrigger},
	trigBodySemi: {trigBodySemi, trigBodySemi, trigTrigger, trigTrigger, trigTrigger, trigTrigger, trigTrigger, trigEnd},
	trigEnd:      {trigStart, trigEnd, trigTrigger, trigTrigger, trigTrigger, trigTrigger, trigTrigger, trigTrigger},
}

// trigWord is the token a word is. The fold is ASCII only, as SQLite's
// keyword comparison is: strings.ToUpper would read TRıGGER (dotless ı) and
// TEMſ (long s) as keywords, which SQLite does not.
func trigWord(w string) trigToken {
	switch asciiUpper(w) {
	case "EXPLAIN":
		return trigExplainTok
	case "CREATE":
		return trigCreateTok
	case "TEMP", "TEMPORARY":
		return trigTempTok
	case "TRIGGER":
		return trigTriggerTok
	case "END":
		return trigEndTok
	}
	return trigOther
}

// asciiUpper upper-cases the ASCII letters of w and leaves every other rune as
// it is.
func asciiUpper(w string) string {
	b := []byte(w)
	for i, c := range b {
		if 'a' <= c && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

// isSQLiteIDRune is SQLite's IdChar: a letter, a digit, '_', '$', or any
// rune past ASCII.
func isSQLiteIDRune(r rune) bool {
	return r == '_' || r == '$' || r >= 0x80 ||
		('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
}

// skipLineComment consumes through the end of the line. A line comment that
// runs to the end of the source is closed by the source ending, not an error.
func (s SQL) skipLineComment(sc *parse.Scanner) {
	for {
		r, ok := sc.Next()
		if !ok || r == '\n' {
			return
		}
	}
}

// skipBlockComment consumes a /* … */ comment, nesting when the dialect does.
func (s SQL) skipBlockComment(sc *parse.Scanner) error {
	openedAt := sc.Pos()
	sc.Take("/*")
	depth := 1
	for depth > 0 {
		if sc.Done() {
			return parse.SyntaxError{
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

// skipQuoted consumes a quoted run.
//
// doubled says whether the quote character repeated inside the run is an
// escaped quote rather than the end of it, which is how SQL escapes quotes in
// strings and in delimited identifiers.
//
// backslash says whether a backslash escapes the character after it. It is off
// for ordinary strings, because standard SQL gives a backslash no such meaning
// and reading it as an escape would mis-split every statement containing a
// Windows path or a regular expression that ends in one. It is on only for a
// string the source marked with an E prefix, which is the engine's own opt-in.
func (s SQL) skipQuoted(sc *parse.Scanner, quote rune, doubled, backslash bool) error {
	openedAt := sc.Pos()
	sc.Next()
	for {
		r, ok := sc.Next()
		if backslash && r == '\\' && ok {
			// The escape consumes whatever follows, so an escaped quote cannot
			// end the run. At end of input there is nothing to consume and the
			// loop falls through to report the unterminated string.
			if _, more := sc.Next(); more {
				continue
			}
		}
		if !ok {
			return parse.SyntaxError{
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
func (s SQL) dollarTag(sc *parse.Scanner) (string, bool) {
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
func (s SQL) skipDollarQuoted(sc *parse.Scanner, tag string) error {
	openedAt := sc.Pos()
	for range tag {
		sc.Next()
	}
	for {
		if sc.Done() {
			return parse.SyntaxError{
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

// startsEString reports whether an E at offset at begins an E'…' string rather
// than sitting inside an identifier.
//
// The check that the preceding byte is not part of a word is what keeps a
// column named "value" from having its trailing e read as a string prefix when
// the next character is a quote.
func (s SQL) startsEString(src []byte, at int) bool {
	if at+1 >= len(src) || src[at+1] != '\'' {
		return false
	}
	if at > 0 && (isWordByte(src[at-1]) || src[at-1] >= '0' && src[at-1] <= '9') {
		return false
	}
	return true
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
