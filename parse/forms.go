package parse

import "bytes"

// prefixMatch is the shared opener test, and the reason the three forms below
// agree at a window edge.
//
//	src = "/"   open = "/*"   ->  Incomplete: "/*" is still possible
//	src = "/x"  open = "/*"   ->  NoMatch:    it is not, and never will be
//	src = "/*"  open = "/*"   ->  Matched(2)
//
// The middle case is what makes this worth sharing: "shorter than the opener"
// is not the test — "shorter AND consistent so far" is.
func prefixMatch(src []byte, open string) (int, Match) {
	if len(src) >= len(open) {
		if string(src[:len(open)]) == open {
			return len(open), Matched
		}
		return 0, NoMatch
	}
	if string(src) == open[:len(src)] {
		return 0, Incomplete
	}
	return 0, NoMatch
}

// LineComment recognises a comment that runs to the end of the line.
//
// The newline is NOT part of the comment: it is the next token's problem, and
// consuming it here would lose a line break that a formatter needs.
func LineComment(open string) Form {
	mustNotBeEmpty("LineComment", open)
	return lineComment{open: open}
}

type lineComment struct{ open string }

func (f lineComment) Kind() Kind { return Comment }

func (f lineComment) Starts(src []byte) (int, Match) { return prefixMatch(src, f.open) }

func (f lineComment) End(src, openedWith []byte, boundary InputBoundary) (int, error) {
	if i := bytes.IndexByte(src, '\n'); i >= 0 {
		return i, nil
	}
	if boundary == EndOfInput {
		// A COMMENT MAY END AT EOF. This is the case that makes the boundary
		// parameter necessary: the same absence of a terminator is completion
		// here and an error for a quote.
		return len(src), nil
	}
	return 0, ErrNeedMore
}

// BlockComment recognises a delimited comment, optionally nesting.
//
// Nesting is a parameter rather than two constructors because the scan is the
// same walk either way, and a caller that gets it wrong gets a comment that
// ends early rather than a compile error — worth one visible argument at the
// call site.
func BlockComment(open, close string, nests bool) Form {
	mustNotBeEmpty("BlockComment", open, close)
	return blockComment{open: open, close: close, nests: nests}
}

type blockComment struct {
	open, close string
	nests       bool
}

func (f blockComment) Kind() Kind { return Comment }

func (f blockComment) Starts(src []byte) (int, Match) { return prefixMatch(src, f.open) }

func (f blockComment) End(src, openedWith []byte, boundary InputBoundary) (int, error) {
	depth := 1
	for i := 0; i < len(src); {
		switch {
		case f.nests && hasPrefixAt(src, i, f.open):
			depth++
			i += len(f.open)
		case hasPrefixAt(src, i, f.close):
			depth--
			i += len(f.close)
			if depth == 0 {
				return i, nil
			}
		default:
			i++
		}
	}
	// A PARTIAL DELIMITER AT THE EDGE is not an absent one. Stopping at
	// len(src) would report "no terminator" for input whose terminator is
	// split across the window boundary.
	if boundary == MoreInput {
		return 0, ErrNeedMore
	}
	return 0, &UnterminatedError{Kind: Comment, Open: string(openedWith)}
}

// QuoteOpts configures how a quoted literal escapes its own terminator. Both
// mechanisms are generic shapes, not dialects: SQL doubles, C backslashes, and
// plenty of formats do one, both or neither.
type QuoteOpts struct {
	// Doubling: the close delimiter written twice is a literal occurrence of
	// it, not the end.
	Doubling bool
	// Escape, when non-zero, is a byte that makes the NEXT byte literal.
	Escape byte
}

// QuoteForm recognises a delimited literal.
func QuoteForm(open, close string, o QuoteOpts) Form {
	mustNotBeEmpty("QuoteForm", open, close)
	return quoteForm{open: open, close: close, opts: o}
}

type quoteForm struct {
	open, close string
	opts        QuoteOpts
}

func (f quoteForm) Kind() Kind { return String }

func (f quoteForm) Starts(src []byte) (int, Match) { return prefixMatch(src, f.open) }

func (f quoteForm) End(src, openedWith []byte, boundary InputBoundary) (int, error) {
	for i := 0; i < len(src); {
		if f.opts.Escape != 0 && src[i] == f.opts.Escape {
			if i+1 >= len(src) {
				// The escaped byte has not arrived. At EOF it never will, and
				// a trailing escape cannot close anything.
				break
			}
			i += 2
			continue
		}
		if !hasPrefixAt(src, i, f.close) {
			i++
			continue
		}
		if f.opts.Doubling {
			j := i + len(f.close)
			if hasPrefixAt(src, j, f.close) {
				i = j + len(f.close) // an escaped occurrence, not the end
				continue
			}
			if j+len(f.close) > len(src) && boundary == MoreInput {
				// AMBIGUOUS AT THE EDGE: this close may be the terminator, or
				// the first half of a doubled one. Deciding now is a coin flip.
				return 0, ErrNeedMore
			}
		}
		return i + len(f.close), nil
	}
	if boundary == MoreInput {
		return 0, ErrNeedMore
	}
	return 0, &UnterminatedError{Kind: String, Open: string(openedWith)}
}

func hasPrefixAt(src []byte, i int, s string) bool {
	return i >= 0 && i+len(s) <= len(src) && string(src[i:i+len(s)]) == s
}

// mustNotBeEmpty rejects a delimiter that cannot work. An empty opener matches
// everywhere and consumes nothing, which is an infinite loop at the first
// offset; an empty closer terminates immediately. Both are programming errors
// at construction, and both are silent corruption if allowed through.
func mustNotBeEmpty(who string, parts ...string) {
	for _, p := range parts {
		if p == "" {
			panic("parse: " + who + ": delimiters must not be empty")
		}
	}
}
