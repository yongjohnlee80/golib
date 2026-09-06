package sql

import "github.com/yongjohnlee80/golib/parse"

// Number returns the form for a SQL numeric literal:
//
//	digits [ . digits ] [ (e|E) [+|-] digits ]
//	.digits            [ (e|E) [+|-] digits ]
//
// # Why this is not a RunForm
//
// A run's membership predicate is position-aware but not CONTENT-aware: it is
// told a byte and its index, never what came before it. So it cannot answer
// "have I already seen a decimal point", and `1.2.3` would come back as one
// number. Nor can it look forward, which the exponent needs — in `1e` the `e`
// belongs to the number only if digits follow it.
//
// A Form can do both, because End is handed the window rather than one byte at a
// time, and this is exactly the extension the interface exists for: a construct
// the lexer has never heard of, added as a value, with nothing below it edited.
//
// # Where a number stops
//
// `1e` at end of input is the number 1 followed by the word e, because an
// exponent with no digits is not an exponent. Mid-stream the same bytes are
// undecidable — the digits may still arrive — so the form defers rather than
// guessing, and the same input lexes the same way whether it came in one chunk
// or byte by byte.
//
// `1.2.3` is the number 1.2 followed by the number .3: the second dot is not the
// first one's business, and a form that swallowed it would be inventing a
// literal no dialect has.
func Number() parse.Form { return numberForm{} }

type numberForm struct{}

func (numberForm) Kind() parse.Kind { return parse.Number }

func (numberForm) Starts(src []byte) (int, parse.Match) {
	if len(src) == 0 {
		return 0, parse.Incomplete
	}
	if isDigit(src[0]) {
		return 1, parse.Matched
	}
	if src[0] != '.' {
		return 0, parse.NoMatch
	}
	// A dot opens a number only when a digit follows it. With nothing after it
	// yet, that is undecidable rather than a guess — `.5` is a number and `.` on
	// its own is punctuation.
	if len(src) < 2 {
		return 0, parse.Incomplete
	}
	if isDigit(src[1]) {
		return 1, parse.Matched
	}
	return 0, parse.NoMatch
}

func (numberForm) End(src, openedWith []byte, boundary parse.InputBoundary) (int, error) {
	// The opener is one byte: a digit, or the dot of a .5 — in which case the
	// fraction is already open and a second dot is not ours.
	seenDot := len(openedWith) > 0 && openedWith[0] == '.'
	seenExp := false

	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case isDigit(c):
			i++

		case c == '.' && !seenDot && !seenExp:
			seenDot = true
			i++

		case (c == 'e' || c == 'E') && !seenExp:
			// An exponent marker only belongs to the number if a digit follows,
			// after an optional sign. Otherwise the number ended before it.
			j := i + 1
			if j < len(src) && (src[j] == '+' || src[j] == '-') {
				j++
			}
			if j >= len(src) {
				if boundary == parse.MoreInput {
					return 0, parse.ErrNeedMore // the digits may still arrive
				}
				return i, nil // at end of input they never will
			}
			if !isDigit(src[j]) {
				return i, nil // e is the next token's, not ours
			}
			seenExp = true
			i = j + 1

		default:
			return i, nil // the first byte that is not part of a number
		}
	}

	// Every byte in the window belongs to the number, so it may continue.
	if boundary == parse.MoreInput {
		return 0, parse.ErrNeedMore
	}
	return len(src), nil
}
