// Unicode Grapheme Segmentation & Display Width Architecture
//
// Terminal UIs render onto a discrete 2D grid of character cells.
// Correct layout requires solving two distinct Unicode problems:
//
//  1. Grapheme Cluster Segmentation (UAX #29):
//     Grouping code points into user-perceived characters (e.g. base characters +
//     combining accents, emoji skin tone modifiers, Zero-Width-Joiner ZWJ sequences,
//     and regional indicator flag pairs).
//     *Segmentation is completely width-independent.*
//
//  2. Terminal Cell Measurement (UAX #11 & UTS #51):
//     Determining whether each segmented grapheme cluster occupies 0, 1, or 2
//     horizontal terminal columns.
//
//	Single-Cell (Width 1)     Double-Cell (Width 2)      Zero-Cell (Width 0)
//	      ┌─────┐                   ┌───────────┐
//	      │ 'A' │                   │   '世'    │          (Combining marks,
//	      └─────┘                   └───────────┘           controls, ZWJ, etc.)
//	   Column x (1 cell)          Columns x, x+1 (2 cells)   Occupies 0 cells
//
// East Asian Ambiguous Width (UAX #11):
//
// A subset of Unicode characters (e.g. Cyrillic letters, Greek letters, certain
// box-drawing characters, arrows, and symbols like '※' or '★') are classified
// as "Ambiguous" (East Asian Width class A):
//   - In Western/modern UTF-8 terminals, they occupy 1 terminal cell.
//   - In legacy CJK terminals (and certain Japanese/Korean fonts), they occupy
//     2 terminal cells (fullwidth).
//
// Architectural Invariants:
//
//  1. App-Wide Single Policy: An App's WidthPolicy is configured ONCE at startup
//     via WithWidthPolicy (defaulting to WidthPolicyDefault). It is never a mutable
//     global variable.
//  2. Layout / Render Consistency: Measurement and surface rendering MUST always
//     agree. If a component measures a string using Width 1 but the terminal buffer
//     renders it as Width 2, every subsequent cell in that row becomes corrupted
//     and misaligned.
//  3. Normative Measurement Rule:
//     - Inside Render(): ALWAYS measure strings using Surface.StringWidth(s).
//     - Inside Layout() / HandleEvent(): ALWAYS measure strings using Context.StringWidth(s).
//     - Package-level tui.StringWidth(s) is strictly a fallback utility locked to
//       WidthPolicyDefault.
//
// Usage Examples:
//
// Example 1: Configuring an application for legacy CJK environments:
//
//	app := tui.NewApp(root,
//	    tui.WithBackend(termBackend),
//	    tui.WithWidthPolicy(tui.WidthPolicyAmbiguousWide),
//	)
//
// Example 2: Measuring and iterating graphemes in a component:
//
//	// In Layout or event handling (outside Render):
//	width := ctx.StringWidth("Hello, 世界! 🚀")
//
//	// In Render:
//	lineWidth := s.StringWidth(line)
//
//	// Iterating user-perceived grapheme clusters:
//	for cluster := range tui.Graphemes("e\u0301 (é)") {
//	    // cluster == "é"
//	}

package tui

import (
	"iter"

	"github.com/yongjohnlee80/golib/tui/internal/grapheme"
)

// WidthPolicy selects the UAX #11 East Asian Ambiguous character interpretation.
//
// Fixed once per App via WithWidthPolicy; travels with the Surface's and
// Context's resolution context — never stored as a mutable package global.
type WidthPolicy uint8

const (
	// WidthPolicyDefault interprets East Asian Ambiguous characters as narrow
	// (1 terminal column). This is the standard behavior in modern Linux, macOS,
	// and Windows terminal emulators.
	WidthPolicyDefault WidthPolicy = iota

	// WidthPolicyAmbiguousWide interprets East Asian Ambiguous characters as wide
	// (2 terminal columns). Required when targeting legacy East Asian (CJK)
	// terminal configurations and localization fonts.
	WidthPolicyAmbiguousWide
)

// ambiguousWide translates the policy into tui/internal/grapheme's boolean flag.
func (p WidthPolicy) ambiguousWide() bool { return p == WidthPolicyAmbiguousWide }

// Graphemes yields the extended grapheme clusters of s in order (Unicode UAX #29).
//
// SEGMENTATION ONLY: Cluster boundaries are strictly standard-compliant and
// policy-independent (UAX #29 segmentation does not vary with width policy).
// Base characters, combining marks, ZWJ emoji sequences (👨‍👩‍👧), and national
// flag pairs (🇰🇷) are yielded as intact substrings.
//
// To measure the display width of the yielded clusters, pass them to
// Context.StringWidth, Surface.StringWidth, or StringWidthPolicy.
func Graphemes(s string) iter.Seq[string] { return grapheme.Clusters(s) }

// StringWidth returns the display width of s in terminal columns under
// WidthPolicyDefault (East Asian Ambiguous = 1 column).
//
// NOTE: Components inside an active App should prefer Context.StringWidth(s)
// in Layout/event handlers and Surface.StringWidth(s) in Render. Calling
// StringWidth directly bypasses any custom WithWidthPolicy configured on the
// App, risking measurement drift if WidthPolicyAmbiguousWide was chosen.
func StringWidth(s string) int { return grapheme.StringWidth(s, false) }

// StringWidthPolicy returns the display width of s under the explicitly provided
// WidthPolicy p.
func StringWidthPolicy(s string, p WidthPolicy) int {
	return grapheme.StringWidth(s, p.ambiguousWide())
}
