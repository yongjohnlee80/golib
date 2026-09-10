// Package grapheme provides Unicode grapheme cluster segmentation (UAX #29
// extended grapheme clusters) and terminal display-width measurement
// (UAX #11 East Asian Width plus UTS #51 emoji data) with zero dependencies,
// backed by generated, committed tables.
//
// It is the foundational Unicode layer beneath golib/tui's cell buffer and
// rendering engine: tui.Graphemes, tui.StringWidth, tui.StringWidthPolicy, and
// Surface.StringWidth are thin wrappers over Clusters, ClusterWidth, and
// StringWidth.
//
// # The Problem: Bytes vs. Runes vs. Graphemes vs. Terminal Cells
//
// In terminal user interfaces, strings cannot be laid out or sliced using Go's
// standard byte length (len(s)) or rune count (utf8.RuneCountInString). A terminal
// screen is a discrete two-dimensional grid of character cells:
//
//	       0     1     2     3     4     5     6     7     (Column index)
//	    ┌─────┬─────┬─────┬─────┬─────┬─────┬─────┬─────┐
//	y=0 │ 'H' │ 'e' │ 'l' │ 'l' │ 'o' │ ' ' │     │     │  (Standard 1-cell ASCII)
//	    ├─────┼─────┼─────┴─────┼─────┼─────┼─────┼─────┤
//	y=1 │ 'e' │ '́'  │    '世'    │    '界'    │ '!' │     │  (é=1 cell; CJK=2 cells each)
//	    ├─────┴─────┼───────────┴─────┬─────┼─────┼─────┤
//	y=2 │   '🇯🇵'    │      '👩‍👩‍👦'      │ 'x' │     │     │  (Flag=2 cells; Emoji ZWJ=2 cells)
//	    └───────────┴─────────────────┴─────┴─────┴─────┘
//
// Misunderstanding the distinctions between Unicode representations leads directly
// to screen tearing, misaligned borders in Box widgets, truncated multi-byte
// sequences, and cursor drift in Editor widgets:
//
//  1. Bytes (len(s)): The raw UTF-8 octet count. Slicing on byte boundaries can
//     split a multi-byte sequence, corrupting UTF-8 and emitting replacement
//     characters (�).
//
//  2. Runes (code points): Slicing on rune boundaries prevents invalid UTF-8,
//     but splits user-perceived characters. For example, "e\u0301" (é) is two
//     runes ('e' and combining acute accent U+0301). Slicing between them leaves
//     a dangling accent mark that visually attaches to whatever character follows.
//
//  3. Grapheme Clusters (UAX #29): The true "user-perceived character".
//     Clusters groups base characters with their modifying combining marks,
//     zero-width joiners (ZWJ), variation selectors, and flag pairs into an
//     atomic, unsplittable visual unit.
//
//  4. Terminal Display Cells (UAX #11 + UTS #51): How many physical monospace
//     cells the cluster occupies on the terminal screen.
//     - 0 cells: combining marks, control characters, default-ignorable code points.
//     - 1 cell:  standard Latin, ASCII, halfwidth Katakana, European alphabets.
//     - 2 cells: East Asian Wide/Fullwidth (CJK kanji/hanja/katakana), emoji
//       presentation sequences, and Regional Indicator flag pairs.
//
// # Representation Mapping
//
//	Input String      Bytes  Runes  Clusters  Cells (Width)  Visual Result
//	----------------  -----  -----  --------  -------------  -------------
//	"A"               1      1      1         1              [A]
//	"é" (composed)    2      1      1         1              [é]
//	"e\u0301" (decom) 3      2      1         1              [é]  (combining mark)
//	"世" (CJK)        3      1      1         2              [ 世 ] (wide)
//	"🇯🇵" (Flag)       8      2      1         2              [ 🇯🇵 ] (RI pair)
//	"❤️" (Heart+VS16) 6      2      1         2              [ ❤️ ] (emoji VS)
//	"👨‍👩‍👧" (Family)    18     5      1         2              [ 👨‍👩‍👧 ] (ZWJ chain)
//
// # Architecture & Zero-Allocation Design
//
// To achieve maximum throughput in high-frame-rate TUI animations and large
// text buffers, grapheme is designed with strict performance invariants:
//
//   - Zero Heap Allocations: Clusters returns an iter.Seq[string] (Go 1.23+
//     range-over-func). Strings yielded are direct subslices of the original
//     buffer without copying or allocating slice headers.
//   - Fast Paths: Pure ASCII text and ASCII-ASCII transitions (which make up
//     >95% of standard code and prose) bypass the Unicode property tables
//     entirely via inline byte checks in clusterLen and StringWidth.
//   - Compact Tables: Lookup tables in tables.go are dense, sorted arrays of
//     rune ranges. Binary searches (gbLookup, inRanges) execute in O(log N)
//     cache-friendly operations with zero interface indirection.
//
// # Segmentation (UAX #29)
//
// Clusters implements the UAX #29 extended grapheme cluster rules GB1–GB13
// and GB999: CR LF joining (GB3), control breaking (GB4/GB5), Hangul jamo
// composition (GB6–GB8), Extend/ZWJ/SpacingMark absorption (GB9/GB9a),
// Prepend (GB9b), Extended_Pictographic ZWJ sequences — emoji joins — (GB11),
// and Regional Indicator pairing — flags — (GB12/GB13). Conformance is
// defined as passing every case of the pinned Unicode version's
// GraphemeBreakTest.txt, which the test suite runs from gen/testdata.
//
// Rule GB9c (Indic conjunct clusters via the InCB property) was introduced in
// Unicode 15.1 and is intentionally NOT implemented: this package pins
// Unicode 15.0.0 to match the Go 1.25 standard library's unicode.Version, and
// GB9c does not exist in the 15.0.0 rule set (15.0.0's GraphemeBreakTest.txt
// contains no conjunct cases). Implement GB9c when the pin moves to ≥ 15.1.0;
// the InCB data lives in DerivedCoreProperties.txt, which the generator
// already mirrors.
//
// # Width (UAX #11 + UTS #51)
//
// The width model is wcwidth-shaped, per cluster: each rune
// is 0, 1, or 2 columns — 0 for C0/C1 controls, combining marks (Mn/Me), and
// default-ignorable code points (which include ZWJ, ZWNJ, and the variation
// selectors); 2 for East Asian Wide/Fullwidth and Emoji_Presentation; 1
// otherwise. A cluster's width is the width of its first non-zero-width
// rune, with overrides: a Regional Indicator pair (flag) is 2, a variation
// selector VS16 (U+FE0F) forces 2, and VS15 (U+FE0E) forces 1. East Asian
// Ambiguous measures 1 by default and 2 when ambiguousWide is set (legacy
// CJK contexts); the public policy surface is tui.WidthPolicy.
//
// # Table Provenance and Refresh Procedure
//
// tables.go is generated by gen/gen.go from Unicode Character Database files
// pinned at the version named in the go:generate line below and mirrored
// under gen/testdata/ (EastAsianWidth.txt, GraphemeBreakProperty.txt,
// emoji-data.txt, DerivedCoreProperties.txt, DerivedGeneralCategory.txt,
// GraphemeBreakTest.txt). The generated header records the SHA-256 of every
// input, so tables are reproducible offline and auditable. No network is
// needed at build, generate, or test time.
//
// Refresh procedure (routine PR on Unicode releases, ~annual):
//
//  1. Bump the version in the go:generate line below.
//  2. go generate ./tui/internal/grapheme (add -download to the gen
//     invocation, or run: go run ./gen -unicode <ver> -download from this
//     directory, to refresh the gen/testdata mirror).
//  3. If the new version is ≥ 15.1.0, implement GB9c (see above) — the
//     conformance suite will fail loudly on the new GraphemeBreakTest.txt
//     conjunct cases until it exists.
//  4. go test ./tui/internal/grapheme/... — conformance and width tests
//     must pass.
//  5. Commit tables.go together with the refreshed mirror, naming the
//     version bump in the commit message.
package grapheme

//go:generate go run ./gen -unicode 15.0.0
