package grapheme

import (
	"iter"
	"unicode/utf8"
)

// gbProp is a Grapheme_Cluster_Break property value (UAX #29), with
// Extended_Pictographic (UTS #51) folded in as a pseudo-property — the two
// sets are disjoint, which the table generator verifies. prAny is the
// zero value for code points carrying no property ("Other" / GB999).
type gbProp uint8

const (
	prAny gbProp = iota
	prCR
	prLF
	prControl
	prExtend
	prZWJ
	prRegionalIndicator
	prPrepend
	prSpacingMark
	prL
	prV
	prT
	prLV
	prLVT
	prExtendedPictographic
)

// gbRange is one row of the generated Grapheme_Cluster_Break table:
// runes in [lo, hi] carry property prop.
type gbRange struct {
	lo, hi rune
	prop   gbProp
}

// runeRange is one row of a generated boolean property table:
// runes in [lo, hi] have the property.
type runeRange struct {
	lo, hi rune
}

// gbLookup returns r's Grapheme_Cluster_Break property by binary search over
// the generated table, or prAny if r carries none.
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

// inRanges reports whether r falls in any range of the sorted table t.
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

// Clusters yields the grapheme clusters of s in order (UAX #29 extended
// grapheme clusters, rules GB1–GB13/GB999 at the pinned Unicode version;
// see the package documentation for the GB9c caveat). The yielded strings
// are contiguous substrings of s: concatenating them reproduces s exactly,
// and iteration allocates nothing. Invalid UTF-8 bytes are yielded as
// single-byte clusters.
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

// clusterLen returns the length in bytes of the first grapheme cluster of s.
// s must be non-empty.
func clusterLen(s string) int {
	// ASCII fast path. An ASCII byte followed by another ASCII byte is
	// always a complete cluster — the only ASCII-ASCII join is CR LF
	// (GB3); every other pair breaks by GB4, GB5, or GB999 (no ASCII code
	// point is Extend, ZWJ, SpacingMark, or Prepend).
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
		// Non-ASCII follows: it may join (e.g. a combining mark after a
		// letter, VS16 after a digit). Fall through to the full rules.
	}

	r, i := utf8.DecodeRuneInString(s)
	prev := gbLookup(r)

	// Cluster-local rule state. Both GB11 and GB12/13 conditions are
	// scoped to the running cluster, so starting fresh at each cluster
	// boundary is exact, not an approximation.
	riOdd := prev == prRegionalIndicator // odd run of Regional Indicators ends at prev
	var pict uint8                       // GB11: 1 = ExtPict Extend* ends at prev; 2 = that followed by ZWJ
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
			// still ExtPict Extend*
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

// boundary reports whether an extended grapheme cluster boundary exists
// between a rune with property prev and a following rune with property cur
// (UAX #29 rules GB3–GB13, GB999; GB1/GB2 are the implicit sot/eot breaks
// handled by the caller). riOdd and pict carry the cluster-local state for
// GB12/13 and GB11 respectively.
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
