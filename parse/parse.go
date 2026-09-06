// Package parse is the shared core for the library's text-format parsers.
//
// A format lives in its own file here — SQL in sql.go, and others as they are
// added. What they share is this file: a position type, an error identity, a
// scanner, and a single required interface. What they do NOT share is a list of
// features, because formats differ enormously in what they can offer and a
// common interface wide enough for all of them would force every format to
// answer questions it has no answer to.
//
// # How a format declares what it can do
//
// [Parser] has exactly one method, and every format implements it. Anything
// beyond that is an OPTIONAL CAPABILITY: a small interface a format implements
// only if it genuinely provides that behaviour. A caller discovers capabilities
// by asking, with the As-prefixed helpers below:
//
//	if v, ok := parse.AsValidator(p); ok {
//		err = v.Validate(src)
//	}
//
// A format that cannot validate cheaply simply does not implement [Validator],
// and nothing anywhere declares that absence. This is the opposite of a
// capability flag: there is no SupportsValidation() bool to answer, so there is
// no way for the answer to disagree with the implementation. Adding a
// capability later is additive and breaks no existing format.
package parse

import (
	"fmt"
	"io"

	"github.com/yongjohnlee80/golib/errs"
)

// Position is a location in source text.
//
// Offset is what you slice with; Line and Column are what you show a person.
// Both are kept because deriving one from the other after the fact requires
// re-scanning the input, and the scanner already knows.
type Position struct {
	// Offset is the byte offset from the start of the source, starting at 0.
	Offset int
	// Line is the line number, starting at 1.
	Line int
	// Column is the column within the line, starting at 1, counted in RUNES
	// rather than bytes so the number matches what an editor shows.
	Column int
}

// String renders the position as line:column, the form editors and compilers
// use. Offset is omitted because it is for slicing, not for reading.
func (p Position) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Column)
}

// Parser turns source text into a value of type T.
//
// This is the whole required surface. A format that can do more says so by
// implementing one of the capability interfaces below.
type Parser[T any] interface {
	Parse(src []byte) (T, error)
}

// Validator reports whether source is well-formed without building a result.
//
// Implement this only when validation is genuinely cheaper than parsing. A
// format whose Validate would just call Parse and discard the value should
// leave this out and let callers do that themselves, visibly.
type Validator interface {
	Validate(src []byte) error
}

// StreamParser parses from a reader, for sources too large to hold in memory
// or arriving over a connection.
//
// Implement this only when the format can genuinely be consumed incrementally.
// A format that must see the whole input before it can produce anything should
// leave this out rather than read the reader to completion and pretend.
type StreamParser[T any] interface {
	ParseStream(r io.Reader) (T, error)
}

// Splitter divides source into units that can each be parsed on their own —
// the statements of a script, the records of a document.
//
// Splitting is not the same as parsing and is often far cheaper: it needs only
// enough of the grammar to know where a unit ends, which usually means knowing
// what a quoted string and a comment look like. A caller that only needs to
// count or route units should not have to build a full result to do it.
type Splitter interface {
	Split(src []byte) ([][]byte, error)
}

// Named reports the format's name for use in diagnostics, such as "sql".
//
// A parser that does not implement this is still perfectly usable; messages
// about it just say "parse" where they would otherwise say the format.
type Named interface {
	FormatName() string
}

// AsValidator reports whether p can validate without parsing, returning the
// capability if so.
func AsValidator(p any) (Validator, bool) {
	v, ok := p.(Validator)
	return v, ok
}

// AsStreamParser reports whether p can parse incrementally from a reader,
// returning the capability if so.
//
// T must be given explicitly, because it appears nowhere in the argument and
// so cannot be inferred:
//
//	sp, ok := parse.AsStreamParser[[]Statement](p)
func AsStreamParser[T any](p any) (StreamParser[T], bool) {
	sp, ok := p.(StreamParser[T])
	return sp, ok
}

// AsSplitter reports whether p can divide source into independent units,
// returning the capability if so.
func AsSplitter(p any) (Splitter, bool) {
	s, ok := p.(Splitter)
	return s, ok
}

// FormatNameOf returns p's format name, or "parse" when p does not report one.
//
// This returns a usable string rather than a name and a boolean because every
// caller of it is building a message, and a caller building a message wants a
// word, not a decision.
func FormatNameOf(p any) string {
	if n, ok := p.(Named); ok {
		if name := n.FormatName(); name != "" {
			return name
		}
	}
	return "parse"
}

// ErrSyntax means the source is not well-formed: a caller gave text this format
// cannot accept.
//
// It is layered on the shared invalid-argument condition, so a caller that
// treats every bad input the same way can ask the general question and a caller
// that wants to point at a line can ask this one.
var ErrSyntax = fmt.Errorf("(%w: syntax)", errs.ErrInvalidArgument)

// ErrUnterminated means the source ended in the middle of a construct — an
// unclosed quote, comment or bracket.
//
// It is a SIBLING of [ErrSyntax] rather than a refinement of it, because the
// two call for different handling: an interactive caller reading input line by
// line responds to this one by asking for more text, where ErrSyntax means the
// input is wrong and more of it will not help.
var ErrUnterminated = fmt.Errorf("(%w: unterminated)", errs.ErrInvalidArgument)

// SyntaxError says where the source went wrong and what was expected there.
//
// It is a VALUE type with value receivers, so Error can never be reached on a
// nil reference and the zero value is a usable error rather than a crash.
// Recover it with errors.As, and SPELL THE TARGET AS A VALUE — a pointer target
// silently fails to match:
//
//	var se parse.SyntaxError
//	if errors.As(err, &se) { … }   // yes
//
//	var se *parse.SyntaxError
//	if errors.As(err, &se) { … }   // no, never matches
type SyntaxError struct {
	// Format is the format that rejected the source, such as "sql". Empty is
	// allowed and renders as "parse".
	Format string
	// Pos is where the trouble is.
	Pos Position
	// Want describes what would have been valid here, in the words a person
	// would use: "a closing quote", "FROM". Optional.
	Want string
	// Got is the text actually found, already quoted for display if it is a
	// literal. Optional.
	Got string
	// Incomplete says the construct was still open when the source ran out,
	// rather than the source being wrong. It is what separates "you need to
	// type more" from "what you typed cannot work", and it selects which of
	// the two identities this error carries.
	Incomplete bool
}

// Error implements error. The text is prose for a person and may be reworded at
// any time; code compares identity with errors.Is or reads the fields with
// errors.As.
func (e SyntaxError) Error() string {
	format := e.Format
	if format == "" {
		format = "parse"
	}
	s := format + ":" + e.Pos.String()
	switch {
	case e.Want != "" && e.Got != "":
		s += ": want " + e.Want + ", got " + e.Got
	case e.Want != "":
		s += ": want " + e.Want
	case e.Got != "":
		s += ": unexpected " + e.Got
	}
	base := ErrSyntax
	if e.Incomplete {
		base = ErrUnterminated
	}
	return s + " " + base.Error()
}

// Unwrap gives the error its identity, so errors.Is answers true for the right
// one of [ErrSyntax] and [ErrUnterminated], and for the shared invalid-argument
// condition underneath both, without this type having to know about any of
// those questions.
//
// The two are SIBLINGS and deliberately do not answer for each other: a caller
// that gives up on ErrSyntax must not thereby give up on input that was merely
// unfinished.
func (e SyntaxError) Unwrap() error {
	if e.Incomplete {
		return ErrUnterminated
	}
	return ErrSyntax
}
