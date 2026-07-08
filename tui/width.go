package tui

import (
	"iter"

	"github.com/yongjohnlee80/golib/tui/internal/grapheme"
)

// WidthPolicy selects the UAX #11 East Asian Ambiguous interpretation
// (ADR-0003 §2.4). Fixed once per App (WithWidthPolicy, ADR-0005); travels
// with the Surface's resolution context — never a global.
type WidthPolicy uint8

const (
	WidthPolicyDefault       WidthPolicy = iota // Ambiguous = 1 (the default everywhere)
	WidthPolicyAmbiguousWide                    // Ambiguous = 2 (CJK legacy contexts)
)

// ambiguousWide translates the policy into tui/internal/grapheme's flag.
func (p WidthPolicy) ambiguousWide() bool { return p == WidthPolicyAmbiguousWide }

// Graphemes yields the grapheme clusters of s, in order (UAX #29 extended
// grapheme clusters). SEGMENTATION ONLY — cluster boundaries are
// policy-independent (UAX #29 does not depend on width). Measure clusters
// via Surface.StringWidth or the functions below (ADR-0003 §2.4).
func Graphemes(s string) iter.Seq[string] { return grapheme.Clusters(s) }

// StringWidth is the display width of s under WidthPolicyDefault —
// explicitly and only. Component code should prefer Surface.StringWidth,
// which applies the App-configured policy (ADR-0003 §2.4, normative).
func StringWidth(s string) int { return grapheme.StringWidth(s, false) }

// StringWidthPolicy is the display width of s under an explicit policy.
func StringWidthPolicy(s string, p WidthPolicy) int {
	return grapheme.StringWidth(s, p.ambiguousWide())
}
