package grapheme

import "unicode/utf8"

// Variation selectors that switch a base character between text and emoji
// presentation (UTS #51).
const (
	vs15 = 0xFE0E // VARIATION SELECTOR-15: text presentation, narrow
	vs16 = 0xFE0F // VARIATION SELECTOR-16: emoji presentation, wide
)

// Regional Indicator symbols (U+1F1E6..U+1F1FF); a pair forms a flag.
const (
	riFirst = 0x1F1E6
	riLast  = 0x1F1FF
)

// ClusterWidth returns the terminal cell width (0, 1, or 2) of a single
// grapheme cluster, as produced by Clusters.
//
// The cluster's width is the width of its first non-zero-width rune —
// 0 for C0/C1 controls, combining marks (Mn/Me), and default-ignorable code
// points (ZWJ, ZWNJ, variation selectors, …); 2 for East Asian Wide/Fullwidth
// (UAX #11) and Emoji_Presentation runes; 1 otherwise — with three overrides:
// a Regional Indicator pair (flag) is 2, a VS16 (U+FE0F) in the cluster
// forces 2, and a VS15 (U+FE0E) forces 1. A cluster containing only
// zero-width runes (e.g. a lone ZWJ or a bare CR LF) is 0.
//
// ambiguousWide selects East Asian Ambiguous = 2 (legacy CJK contexts)
// instead of the default 1.
func ClusterWidth(cluster string, ambiguousWide bool) int {
	base := 0
	ri := 0
	var vs rune
	for _, r := range cluster {
		if r == vs15 || r == vs16 {
			vs = r
			continue
		}
		if riFirst <= r && r <= riLast {
			ri++
		}
		if base == 0 {
			base = runeWidth(r, ambiguousWide)
		}
	}
	switch {
	case vs == vs16:
		return 2
	case vs == vs15:
		return 1
	case ri >= 2:
		return 2
	default:
		return base
	}
}

// StringWidth returns the total terminal cell width of s: the sum of
// ClusterWidth over Clusters(s). ambiguousWide selects East Asian
// Ambiguous = 2 (legacy CJK contexts) instead of the default 1.
func StringWidth(s string, ambiguousWide bool) int {
	w := 0
	for len(s) > 0 {
		// Printable-ASCII fast path: a complete width-1 cluster whenever
		// the next byte is ASCII too (mirrors clusterLen's fast path; a
		// following non-ASCII rune could be a combining mark or VS).
		if c := s[0]; 0x20 <= c && c < 0x7F && (len(s) == 1 || s[1] < utf8.RuneSelf) {
			w++
			s = s[1:]
			continue
		}
		n := clusterLen(s)
		w += ClusterWidth(s[:n], ambiguousWide)
		s = s[n:]
	}
	return w
}

// runeWidth returns the display width (0, 1, or 2) of a single rune under
// the wcwidth model of ADR-0003 §2.7.
func runeWidth(r rune, ambiguousWide bool) int {
	switch {
	case r < 0x20:
		return 0 // C0 controls (and NUL)
	case r < 0x7F:
		return 1 // printable ASCII
	case r < 0xA0:
		return 0 // DEL and C1 controls
	case inRanges(r, zeroWidthRanges):
		return 0 // Mn, Me, default-ignorables (checked before wide: many are also EAW A/W)
	case inRanges(r, wideRanges), inRanges(r, emojiPresentationRanges):
		return 2
	case ambiguousWide && inRanges(r, ambiguousRanges):
		return 2
	default:
		return 1
	}
}
