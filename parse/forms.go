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
	// Kind is the token kind this construct produces. The zero value means
	// String, which is what a quoted literal usually is — but the SAME shape is
	// a dialect's quoted IDENTIFIER, and the core has no way to tell the two
	// apart. So the kind is the caller's to state, as it already is for a run or
	// a literal set, rather than something the core decided for everyone.
	Kind Kind
}

// QuoteForm recognises a delimited literal.
func QuoteForm(open, close string, o QuoteOpts) Form {
	mustNotBeEmpty("QuoteForm", open, close)
	if o.Kind == Invalid {
		o.Kind = String
	}
	return quoteForm{open: open, close: close, opts: o}
}

type quoteForm struct {
	open, close string
	opts        QuoteOpts
}

func (f quoteForm) Kind() Kind { return f.opts.Kind }

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
			// AMBIGUOUS ONLY WHILE A DOUBLING IS STILL POSSIBLE. What follows
			// the closer must be a PROPER PREFIX of it — the empty string
			// included — for the question to be open at all:
			//
			//	close = "END"      after the closer    a second END is
			//	                                       ...
			//	                   ""                  still possible -> defer
			//	                   "EN"                still possible -> defer
			//	                   "z"                 ruled out      -> final
			//
			// Testing only "are there fewer bytes left than the closer needs"
			// defers on every short remainder, including one whose first byte
			// already disagrees. That stalls a live stream on input that has
			// decided itself, and makes the answer depend on the boundary for
			// bytes that do not.
			if boundary == MoreInput && isProperPrefix(src[j:], f.close) {
				return 0, ErrNeedMore
			}
		}
		return i + len(f.close), nil
	}
	if boundary == MoreInput {
		return 0, ErrNeedMore
	}
	return 0, &UnterminatedError{Kind: f.opts.Kind, Open: string(openedWith)}
}

// isProperPrefix reports whether got is a prefix of want and SHORTER than it.
// The empty slice qualifies: nothing has arrived yet, so anything is still
// possible. A got as long as want is not open — it either matched or did not.
func isProperPrefix(got []byte, want string) bool {
	return len(got) < len(want) && string(got) == want[:len(got)]
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

// DelimitedOpts constrains the TAG of a delimited form.
//
// Without a constraint the shape is too permissive to be useful: `$1$` would
// open a delimited literal in a dialect where `$1` is a parameter. With
// PostgreSQL's tag charset written into this package, the core would have named
// a dialect. So the rule is supplied by the leaf, and the core only applies it.
type DelimitedOpts struct {
	// TagByte reports whether b is legal at index (0-based, within the tag).
	//
	// POSITION-AWARE, because a single func(byte) bool cannot express the
	// ordinary rule: a leading digit is illegal while a trailing one is fine,
	// so it could not both reject `$1$` and accept `$a1$`.
	//
	// It joins the purity contract: it is called again on the same bytes as the
	// window widens, and must answer identically every time. nil accepts every
	// byte, which is a decision, not a default — see AllowEmpty.
	TagByte func(index int, b byte) bool

	// AllowEmpty permits the zero-length tag, `$$` in the familiar shape.
	AllowEmpty bool

	// MaxTagBytes bounds the tag. Zero means unbounded HERE, leaving the scan's
	// own delimiter limit as the only bound — the trade the ADR states, and one
	// a caller should make on purpose rather than inherit.
	MaxTagBytes int

	// Kind is the token kind this construct produces. The zero value means
	// String, for the same reason it does on a quoted literal: the shape does not
	// say whether a dialect means a literal or an identifier by it.
	Kind Kind
}

// DelimitedForm recognises the tag-carrying shape `<prefix>TAG<suffix> … the
// same again`, of which PostgreSQL's dollar quoting is one configuration. The
// core never names that dialect: the tag rule arrives from the caller.
func DelimitedForm(prefix, suffix byte, o DelimitedOpts) Form {
	if o.MaxTagBytes < 0 {
		panic("parse: DelimitedForm: MaxTagBytes must not be negative")
	}
	if o.Kind == Invalid {
		o.Kind = String
	}
	return delimitedForm{prefix: prefix, suffix: suffix, opts: o}
}

type delimitedForm struct {
	prefix, suffix byte
	opts           DelimitedOpts
}

func (f delimitedForm) Kind() Kind { return f.opts.Kind }

func (f delimitedForm) Starts(src []byte) (int, Match) {
	if len(src) == 0 {
		return 0, Incomplete
	}
	if src[0] != f.prefix {
		return 0, NoMatch
	}
	for i := 1; i < len(src); i++ {
		if src[i] == f.suffix {
			if i == 1 && !f.opts.AllowEmpty {
				return 0, NoMatch
			}
			return i + 1, Matched
		}
		// A REJECTED BYTE IS NoMatch, NOT AN UNTERMINATED CONSTRUCT. The prefix
		// belongs to some later form — an operator, a parameter marker — and
		// claiming it here would take a token away from a form that can lex it.
		if f.opts.TagByte != nil && !f.opts.TagByte(i-1, src[i]) {
			return 0, NoMatch
		}
		if f.opts.MaxTagBytes > 0 && i > f.opts.MaxTagBytes {
			return 0, NoMatch
		}
	}
	// Ran out of window inside a tag that is still legal. No second bound check
	// belongs here: the in-loop one fires as soon as the tag exceeds its limit,
	// which is strictly before the loop can end this way, so a check here would
	// be unreachable. (It was written, and a control that failed to redden is
	// what showed it never ran.)
	return 0, Incomplete
}

func (f delimitedForm) End(src, openedWith []byte, boundary InputBoundary) (int, error) {
	if len(openedWith) == 0 {
		// Nothing to look for. Reported rather than scanned forever.
		return 0, contractf(f, "End called with an empty opener")
	}
	if i := bytes.Index(src, openedWith); i >= 0 {
		return i + len(openedWith), nil
	}
	if boundary == MoreInput {
		return 0, ErrNeedMore
	}
	return 0, &UnterminatedError{Kind: f.opts.Kind, Open: string(openedWith)}
}
