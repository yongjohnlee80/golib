package parse

import "strings"

// RunForm and SetForm are the forms for the kinds between the delimited ones —
// words, numbers, whitespace, operators, punctuation. They exist so the lexer
// still walks ONE list: a leaf that ends its form list so every byte is claimed
// makes total coverage a property of the list rather than a second mechanism the
// lexer carries. The character classes and the literals come from the caller, so
// the core still names no dialect.

// RunForm recognises a maximal run of bytes for which member reports true: a
// word, a number, a whitespace gap. member is POSITION-AWARE — it is told the
// byte's index within the run — because the ordinary rule is: a word's first
// byte is a letter and the rest are letters or digits, which a plain
// func(byte) bool cannot express. A caller whose class does not depend on
// position ignores the index.
//
// The run has no terminator of its own; it ends at the first byte member
// refuses, and that byte is the next token's, not consumed. So it splits across
// the Form interface the way LineComment does: Starts claims the first byte,
// which is all that can be decided without a boundary, and End walks the rest —
// which HAS the boundary, so a run of member bytes reaching the window edge
// defers under MoreInput and completes at EndOfInput. A run never reports itself
// unterminated: reaching end of input is where a word ends, not an error.
//
// The run index is CONTINUOUS across the two calls: Starts consumed index 0, so
// End resolves its own bytes at len(openedWith)+i. The opener's bytes go unused,
// but its WIDTH is what places the rest of the run.
//
// A member that is true only at index 0 is the exact-one-byte form — the honest
// fallback for total coverage. A member that is true for every byte is NOT: with
// no refusing byte the maximal run swallows the whole remainder as one token.
func RunForm(k Kind, member func(index int, b byte) bool) Form {
	if member == nil {
		panic("parse: RunForm: member must not be nil")
	}
	return runForm{kind: k, member: member}
}

type runForm struct {
	kind   Kind
	member func(int, byte) bool
}

func (f runForm) Kind() Kind { return f.kind }

func (f runForm) Starts(src []byte) (int, Match) {
	if len(src) == 0 {
		return 0, Incomplete
	}
	if f.member(0, src[0]) {
		return 1, Matched
	}
	return 0, NoMatch
}

func (f runForm) End(src, openedWith []byte, boundary InputBoundary) (int, error) {
	base := len(openedWith) // the opener occupied run indices [0, base)
	for i := 0; i < len(src); i++ {
		if !f.member(base+i, src[i]) {
			return i, nil // the first refused byte; the run ends before it
		}
	}
	if boundary == EndOfInput {
		return len(src), nil // the run ends at end of input, not unterminated
	}
	// Every byte in the window belongs to the run, so it may still continue.
	return 0, ErrNeedMore
}

// SetForm recognises the longest of a fixed set of literals — the shape for a
// dialect's operators and punctuation, where `<=` must beat `<` and `--` must
// beat `-`. It walks the literals as a trie, and the split between Starts and End
// is what makes a shared prefix chunk-invariant.
//
// Starts answers Incomplete while the observed bytes are still on a path toward a
// literal but have reached none, and NoMatch once they leave every path. When
// they reach the FIRST literal on that path — the shortest one that is a prefix
// of the window — it returns that literal's WIDTH, and keeps returning it however
// much more arrives. That answer is a stable opener, not a commitment to the
// final token: End may still extend it.
//
// End resolves the longest completed descendant of the opener, and defers with
// ErrNeedMore only while a longer descendant is still possible and more input may
// arrive. So `-` defers mid-stream, completes as `-` at end of input, and becomes
// `--` when the second byte arrives — without Starts ever having to withdraw an
// answer, which is what a two-valued Starts could not manage.
func SetForm(k Kind, lits ...string) Form {
	if len(lits) == 0 {
		panic("parse: SetForm: at least one literal is required")
	}
	for _, s := range lits {
		if s == "" {
			panic("parse: SetForm: an empty literal matches everywhere and consumes nothing")
		}
	}
	return setForm{kind: k, lits: lits}
}

type setForm struct {
	kind Kind
	lits []string
}

func (f setForm) Kind() Kind { return f.kind }

func (f setForm) Starts(src []byte) (int, Match) {
	// The first terminal on the observed path: the SHORTEST literal that is a
	// prefix of the window. Taking the shortest is what keeps the answer stable —
	// a longer window can only add descendants, never a shorter ancestor.
	first := -1
	for _, lit := range f.lits {
		if len(lit) <= len(src) && string(src[:len(lit)]) == lit {
			if first < 0 || len(lit) < first {
				first = len(lit)
			}
		}
	}
	if first >= 0 {
		return first, Matched
	}
	// No terminal reached yet. Still on a path toward one?
	for _, lit := range f.lits {
		if len(lit) > len(src) && lit[:len(src)] == string(src) {
			return 0, Incomplete
		}
	}
	return 0, NoMatch
}

func (f setForm) End(src, openedWith []byte, boundary InputBoundary) (int, error) {
	op := string(openedWith)
	longest := len(op) // the opener is itself a completed literal
	extendable := false
	for _, lit := range f.lits {
		if !strings.HasPrefix(lit, op) {
			continue // not a descendant of the opener's node
		}
		tail := lit[len(op):]
		switch {
		case len(tail) <= len(src):
			if string(src[:len(tail)]) == tail && len(lit) > longest {
				longest = len(lit)
			}
		case tail[:len(src)] == string(src):
			// A longer descendant is still consistent with what has arrived.
			extendable = true
		}
	}
	if extendable && boundary == MoreInput {
		return 0, ErrNeedMore
	}
	return longest - len(op), nil
}
