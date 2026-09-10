package grapheme

import "unicode/utf8"

// Variation selectors that switch a base character between text and emoji
// presentation (UTS #51 §5).
const (
	// vs15 (U+FE0E) VARIATION SELECTOR-15 requests text presentation (narrow, 1 column).
	// Example: U+2602 (umbrella '☂') + VS15 -> '☂︎' rendered as monochrome text (1 column).
	vs15 = 0xFE0E

	// vs16 (U+FE0F) VARIATION SELECTOR-16 requests emoji presentation (wide, 2 columns).
	// Example: U+2602 (umbrella '☂') + VS16 -> '☂️' rendered as colorful emoji (2 columns).
	vs16 = 0xFE0F
)

// Regional Indicator symbols (U+1F1E6..U+1F1FF).
// A sequence of two Regional Indicator characters represents a national flag (ISO 3166-1).
const (
	riFirst = 0x1F1E6
	riLast  = 0x1F1FF
)

// ClusterWidth returns the terminal cell display width (0, 1, or 2 columns)
// of a single extended grapheme cluster, as produced by Clusters.
//
// # Terminal Cell Layout Model
//
// Modern terminal emulators treat the screen as a grid of discrete cells.
// Characters occupy 0, 1, or 2 cells:
//
//	Single-Cell (Width 1)     Double-Cell (Width 2)      Zero-Cell (Width 0)
//	      ┌─────┐                   ┌───────────┐
//	      │ 'A' │                   │   '世'    │          (Combining marks,
//	      └─────┘                   └───────────┘           controls, ZWJ, etc.)
//	   Column x (1 cell)          Columns x, x+1 (2 cells)   Occupies 0 cells
//
// # Measurement Rules & Precedence
//
// The cluster width calculation proceeds in three stages:
//
//  1. Base Width: Scan the cluster runes to find the first non-zero-width rune.
//     - 0 columns: C0/C1 controls, combining marks (Mn/Me), and default-ignorable
//       code points (ZWJ, ZWNJ, variation selectors).
//     - 2 columns: East Asian Wide/Fullwidth (UAX #11) and Emoji_Presentation runes.
//     - 1 column: All other printable code points.
//
//  2. Overrides:
//     - VS16 (U+FE0F): If present, forces the cluster width to 2 columns (emoji presentation).
//     - VS15 (U+FE0E): If present, forces the cluster width to 1 column (text presentation).
//     - Regional Indicators: If the cluster contains 2 or more RI runes (a flag pair,
//       such as 🇰 + 🇷 = 🇰🇷), the width is forced to 2 columns.
//
//  3. Degenerate Zero-Width: If the cluster contains only zero-width runes
//     (e.g., a lone ZWJ, a bare combining mark, or a bare CR LF), 0 is returned.
//
// # Ambiguous Width Policy
//
// ambiguousWide controls the measurement of East Asian Ambiguous characters (UAX #11):
//   - false (default): Measures 1 column (standard Western/UTF-8 terminal behavior).
//   - true: Measures 2 columns (legacy CJK terminal environments).
//
// The public configuration policy is exposed as tui.WidthPolicy in the parent package.
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

// StringWidth returns the total terminal cell display width of string s.
// It is equivalent to summing ClusterWidth over all clusters in Clusters(s),
// but executes significantly faster via an inline ASCII fast path.
//
// ambiguousWide selects East Asian Ambiguous = 2 (legacy CJK contexts)
// instead of the default 1.
//
// # Fast-Path Optimization
//
// For printable ASCII characters (0x20 <= c < 0x7F), StringWidth increments
// the width by 1 and advances without invoking clusterLen, provided the next
// byte is also ASCII (or at end-of-string).
//
// Checking the subsequent byte is mandatory: if a non-ASCII byte follows an
// ASCII character, that non-ASCII byte could be a combining mark (e.g. U+0301)
// or a variation selector that attaches to the ASCII character and alters the
// cluster boundary or width.
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

// runeWidth returns the display width (0, 1, or 2 columns) of a single rune under
// the wcwidth model originated by Markus Kuhn, extended with UAX #11 East Asian
// Width and UTS #51 Emoji Presentation semantics.
//
// # Evaluation Order and Precedence Invariants
//
// Precedence order in the switch statement below is critical for correct measurement:
//
//  1. C0 Controls (r < 0x20): 0 columns. Includes NUL, BS, TAB, LF, CR.
//  2. Printable ASCII (0x20 <= r < 0x7F): 1 column. Standard Western characters.
//  3. DEL & C1 Controls (0x7F <= r < 0xA0): 0 columns. Terminal control codes.
//  4. Zero-Width Ranges (inRanges(r, zeroWidthRanges)): 0 columns.
//     Includes Combining Nonspacing Marks (Mn), Combining Enclosing Marks (Me),
//     and default-ignorable code points (ZWJ, ZWNJ, variation selectors).
//     INVARIANT: zeroWidthRanges MUST be tested before wideRanges because several
//     combining marks and symbols are also classified as East Asian Ambiguous or Wide.
//  5. Wide & Emoji Ranges: 2 columns.
//     Includes East Asian Wide (W), Fullwidth (F), and default Emoji_Presentation.
//  6. Ambiguous Ranges (ambiguousWide && inRanges(r, ambiguousRanges)): 2 columns.
//     Includes East Asian Ambiguous (A) characters when legacy CJK mode is active.
//  7. Default: 1 column. All remaining unclassified Unicode runes.
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
