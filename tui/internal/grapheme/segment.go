package grapheme

import (
	"iter"
	"unicode/utf8"
)

// gbProp represents a Unicode Grapheme_Cluster_Break property value defined in
// UAX #29 Section 3. It categorizes code points according to their boundary-breaking
// behavior.
//
// In addition to the standard UAX #29 properties, UTS #51's Extended_Pictographic
// property is folded into this enum as a pseudo-property (prExtendedPictographic).
// The generator (gen/gen.go) mathematically verifies that the UAX #29 break property
// set and the UTS #51 Extended_Pictographic set are mutually disjoint across all
// assigned code points.
//
// prAny (0) is the default zero value representing code points with no assigned
// break property ("Other"), subject to rule GB999 (break everywhere else).
type gbProp uint8

const (
	// prAny represents any code point without a specific grapheme break property ("Other").
	prAny gbProp = iota

	// prCR represents U+000D CARRIAGE RETURN (\r).
	prCR

	// prLF represents U+000A LINE FEED (\n).
	prLF

	// prControl represents general category Cc, Cf (excluding specific joiners),
	// Cs, Co, and Cn non-characters.
	prControl

	// prExtend represents combining marks (Mn, Me), variation selectors, and
	// enclosing marks that visually attach to and extend a base character.
	prExtend

	// prZWJ represents U+200D ZERO WIDTH JOINER. Used to sequence multiple emoji
	// into a single compound glyph (e.g. 👨 + ZWJ + 👩 + ZWJ + 👧).
	prZWJ

	// prRegionalIndicator represents Regional Indicator symbols U+1F1E6..U+1F1FF.
	// Two adjacent RIs combine pairwise to form a two-letter country flag (e.g. 🇰 + 🇷 = 🇰🇷).
	prRegionalIndicator

	// prPrepend represents characters that visually bind to the following base character.
	prPrepend

	// prSpacingMark represents spacing combining marks (category Mc) that visually
	// modify the base character while consuming independent display space.
	prSpacingMark

	// prL represents Hangul Choseong (leading consonant jamo).
	prL

	// prV represents Hangul Jungseong (vowel jamo).
	prV

	// prT represents Hangul Jongseong (trailing consonant jamo).
	prT

	// prLV represents Hangul precomposed Syllable-LV (consonant + vowel).
	prLV

	// prLVT represents Hangul precomposed Syllable-LVT (consonant + vowel + trailing).
	prLVT

	// prExtendedPictographic represents UTS #51 pictorial symbols and emoji.
	prExtendedPictographic
)

// gbRange represents a contiguous span of Unicode runes [lo, hi] that share
// the same Grapheme_Cluster_Break property value prop.
//
// Stored as a flat, sorted array in tables.go, gbRange structures eliminate
// pointer indirection and maximize CPU L1 cache line locality during binary search.
type gbRange struct {
	lo, hi rune
	prop   gbProp
}

// runeRange represents a contiguous span of Unicode runes [lo, hi] possessing
// a boolean classification property (e.g. wide, zero-width, ambiguous).
type runeRange struct {
	lo, hi rune
}

// gbLookup returns r's Grapheme_Cluster_Break property by performing an O(log N)
// binary search over the sorted gbRanges table.
//
// Invariants:
//   - Zero heap allocations.
//   - If r does not match any entry in gbRanges, prAny ("Other") is returned.
//   - Midpoint calculation uses uint(lo+hi) >> 1 to prevent integer overflow.
func gbLookup(r rune) gbProp {
	lo, hi := 0, len(gbRanges)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		switch rg := gbRanges[m]; {
		case r < rg.lo:
			hi = m
		case r > rg.hi:
			lo = m + 1
		default:
			return rg.prop
		}
	}
	return prAny
}

// inRanges reports whether rune r falls within any inclusive span [lo, hi]
// in the sorted slice t. It performs a zero-allocation binary search in O(log N) time.
func inRanges(r rune, t []runeRange) bool {
	lo, hi := 0, len(t)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		switch rg := t[m]; {
		case r < rg.lo:
			hi = m
		case r > rg.hi:
			lo = m + 1
		default:
			return true
		}
	}
	return false
}

// Clusters returns an iterator yielding the extended grapheme clusters of s in order,
// conforming to UAX #29 rules GB1–GB13 and GB999 at the pinned Unicode version.
//
// # Execution & Yield Contract
//
// Clusters conforms to Go 1.23+ range-over-func conventions (iter.Seq[string]):
//
//	for cluster := range grapheme.Clusters(text) {
//	    // cluster is a contiguous substring of text
//	}
//
// Guarantees:
//   - Zero Heap Allocations: Each yielded string is a subslice of s (s[:n]).
//     No string copies or intermediate slice headers are allocated on the heap.
//   - Round-Trip Fidelity: Concatenating all yielded clusters reproduces the
//     exact byte content of the input string: strings.Join(slices.Collect(Clusters(s)), "") == s.
//   - Malformed UTF-8 Safety: Invalid UTF-8 byte sequences are yielded as
//     individual single-byte clusters without panicking or entering an infinite loop.
//   - Early Termination: If the yield callback returns false, iteration halts
//     immediately with zero residual state.
//
// # Grapheme Cluster Boundary Examples
//
//	Input String             Clusters Yielded             Break Rules Applied
//	-----------------------  ---------------------------  -----------------------------
//	"Hello"                  "H", "e", "l", "l", "o"      GB999 (standard Latin breaks)
//	"\r\n"                   "\r\n"                       GB3 (CR × LF joins)
//	"e\u0301"                "é"                          GB9 (Extend absorbs into 'e')
//	"각" (Hangul L+V+T)     "각"                        GB6, GB7 (Hangul Jamo joins)
//	"🇰🇷" (RI + RI)           "🇰🇷"                         GB12/13 (RI pair forms flag)
//	"👩‍👩‍👦" (Family emoji)    "👩‍👩‍👦"                       GB11 (ExtPict + ZWJ chain)
func Clusters(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for len(s) > 0 {
			n := clusterLen(s)
			if !yield(s[:n]) {
				return
			}
			s = s[n:]
		}
	}
}

// clusterLen returns the byte length of the first extended grapheme cluster in s.
// The input string s must be non-empty (len(s) > 0).
//
// # Fast-Path Architecture
//
// Processing text in a TUI happens on the hot rendering path (every frame, every cell).
// Over 95% of characters in code and prose are pure ASCII (< 0x80). clusterLen
// leverages this property via an inline fast path:
//
//	           s[0] < 0x80 ?
//	             /        \
//	           Yes         No (Non-ASCII) ───► Full UAX #29 Engine
//	           /
//	     len(s) == 1 ?
//	       /      \
//	     Yes       No
//	     /           \
//	Return 1     s[0]=='\r' && s[1]=='\n' ?
//	               /        \
//	             Yes         No
//	             /             \
//	         Return 2        s[1] < 0x80 ?
//	                           /        \
//	                         Yes         No ──► Non-ASCII follows (combining mark/VS?)
//	                         /
//	                     Return 1
//
// Why this works: In UAX #29, the ONLY join between two ASCII code points is
// CR LF (GB3). Every other ASCII-ASCII pair breaks unconditionally by GB4, GB5,
// or GB999 because no ASCII code point has property Extend, ZWJ, SpacingMark,
// or Prepend. When non-ASCII follows an ASCII character, the character might be
// a combining mark (e.g. 'e' followed by U+0301) or a variation selector, so
// the algorithm falls through to the full rule set.
//
// # Cluster-Local State Machine
//
// For multi-rune clusters, clusterLen maintains two pieces of state:
//   - riOdd (bool): Tracks whether an odd number of Regional Indicator runes
//     have appeared in an unbroken sequence. Regional indicators pair up strictly
//     two-by-two from the left (GB12/GB13).
//   - pict (uint8): A 3-state automaton tracking UTS #51 emoji ZWJ sequences (GB11):
//     State 0: Initial / idle.
//     State 1: ExtPict Extend* (An emoji base followed by zero or more modifiers/skins).
//     State 2: ExtPict Extend* ZWJ (An emoji base + modifiers followed by ZWJ; ready to join).
//
// Both GB11 and GB12/GB13 conditions are strictly scoped to the running cluster.
// Initializing them fresh at the start of each cluster boundary is mathematically
// exact according to the UAX #29 specification.
func clusterLen(s string) int {
	// ASCII fast path: immediate resolution for pure ASCII text.
	if c := s[0]; c < utf8.RuneSelf {
		if len(s) == 1 {
			return 1
		}
		if c == '\r' && s[1] == '\n' {
			return 2
		}
		if s[1] < utf8.RuneSelf {
			return 1
		}
		// Non-ASCII byte follows: it may join to the preceding ASCII character
		// (e.g. a combining mark after a letter, or VS16 after a digit).
	}

	r, i := utf8.DecodeRuneInString(s)
	prev := gbLookup(r)

	// Cluster-local state machine.
	riOdd := prev == prRegionalIndicator
	var pict uint8
	if prev == prExtendedPictographic {
		pict = 1
	}

	for i < len(s) {
		r, sz := utf8.DecodeRuneInString(s[i:])
		cur := gbLookup(r)
		if boundary(prev, cur, riOdd, pict) {
			return i
		}
		if cur == prRegionalIndicator {
			riOdd = !riOdd
		} else {
			riOdd = false
		}
		switch {
		case cur == prExtendedPictographic:
			pict = 1
		case pict == 1 && cur == prExtend:
			// Retain State 1: ExtPict Extend*
		case pict == 1 && cur == prZWJ:
			pict = 2
		default:
			pict = 0
		}
		prev = cur
		i += sz
	}
	return len(s)
}

// boundary reports whether an extended grapheme cluster break boundary exists
// between the preceding rune (property prev) and the current rune (property cur).
//
// The rules correspond directly to UAX #29 Section 3.1.1 (Unicode 15.0.0):
//
//	Rule      Condition                                 Action  Description
//	--------  ----------------------------------------  ------  ------------------------------------
//	GB3       CR × LF                                   Join    Do not break between CR and LF
//	GB4       (Control | CR | LF) ÷                     Break   Break after controls
//	GB5       ÷ (Control | CR | LF)                     Break   Break before controls
//	GB6       L × (L | V | LV | LVT)                    Join    Hangul Choseong joins vowel/syllable
//	GB7       (LV | V) × (V | T)                        Join    Hangul Jungseong joins vowel/tail
//	GB8       (LVT | T) × T                             Join    Hangul Jongseong joins tail
//	GB9       × (Extend | ZWJ)                          Join    Absorb combining marks and joiners
//	GB9a      × SpacingMark                             Join    Absorb spacing marks
//	GB9b      Prepend ×                                 Join    Do not break after prepend marks
//	GB11      ExtPict Extend* ZWJ × ExtPict             Join    Emoji ZWJ sequences
//	GB12/13   RI × RI (pairwise from left)              Join    Regional Indicator flag pairs
//	GB999     Any ÷ Any                                 Break   Break everywhere else
func boundary(prev, cur gbProp, riOdd bool, pict uint8) bool {
	switch {
	case prev == prCR && cur == prLF:
		return false // GB3: CR × LF
	case prev == prControl || prev == prCR || prev == prLF:
		return true // GB4: (Control | CR | LF) ÷
	case cur == prControl || cur == prCR || cur == prLF:
		return true // GB5: ÷ (Control | CR | LF)
	case prev == prL && (cur == prL || cur == prV || cur == prLV || cur == prLVT):
		return false // GB6: L × (L | V | LV | LVT)
	case (prev == prLV || prev == prV) && (cur == prV || cur == prT):
		return false // GB7: (LV | V) × (V | T)
	case (prev == prLVT || prev == prT) && cur == prT:
		return false // GB8: (LVT | T) × T
	case cur == prExtend || cur == prZWJ:
		return false // GB9: × (Extend | ZWJ)
	case cur == prSpacingMark:
		return false // GB9a: × SpacingMark
	case prev == prPrepend:
		return false // GB9b: Prepend ×
	case prev == prZWJ && cur == prExtendedPictographic && pict == 2:
		return false // GB11: ExtPict Extend* ZWJ × ExtPict
	case prev == prRegionalIndicator && cur == prRegionalIndicator && riOdd:
		return false // GB12/GB13: RI × RI, pairwise from the left
	default:
		return true // GB999: break everywhere else
	}
}
