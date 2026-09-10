package tui

// Cell represents a single terminal grid position.
//
// # Architectural Principles
//
//  1. Complete Grapheme Clusters: Content holds exactly one complete UTF-8 grapheme
//     cluster — never a partial cluster, and never multiple clusters. This reflects the
//     modern terminal consensus (tcell v3, vaxis, bubbletea v2) preventing multi-byte
//     or combining rune rendering corruption.
//  2. Pre-Measured Display Width: Width caches the monospace column span (1 or 2)
//     measured at write time under the active WidthPolicy. A continuation cell
//     (the right half of a wide character) has Width == 0 and Content == "".
//  3. Pre-Resolved Attributes: Attrs carries the final style payload already downsampled
//     to the terminal's ColorProfile with SGR attribute bits set. Neither the frame
//     differ nor the terminal emitter ever needs to consult a Theme.
//  4. Comparable Struct Equality (==): Cell contains only comparable fields (string,
//     uint8, CellAttrs). Consequently, dirty checking during frame diffing is a single
//     Go struct comparison: `curr[i] == last[i]`.
type Cell struct {
	Content string    // one complete grapheme cluster; "" on wide continuation cells
	Width   uint8     // display column width: 1 or 2; 0 indicates a continuation cell
	Attrs   CellAttrs // resolved style payload (colors and SGR attribute bits)
}

// Continuation reports whether c is the right half of a wide cell.
func (c Cell) Continuation() bool { return c.Width == 0 }

// CellAttrs is the resolved, output-form style payload of a Cell: packed
// fg/bg colors already downsampled to the terminal's ColorProfile, plus the
// SGR attribute bits. It is produced by the package's style resolver
// (resolve.go) and consumed by the tui/term emitter; it is a small
// comparable value so Cell equality stays one ==.
type CellAttrs struct {
	FG, BG CellColor
	Mask   AttrMask
}

// CellColorKind enumerates the output forms a resolved color can take.
// Unlike style.Color there is no token or adaptive kind: resolution has
// already happened.
type CellColorKind uint8

const (
	CellColorDefault CellColorKind = iota // terminal default fg/bg (SGR 39/49)
	CellColorANSI                         // ANSI-16 palette index (SGR 30-37/90-97 forms)
	CellColorANSI256                      // ANSI-256 palette index (SGR 38;5/48;5)
	CellColorRGB                          // truecolor (SGR 38;2/48;2)
)

// CellColor is one resolved color in output form. The zero value is the
// terminal default.
type CellColor struct {
	Kind    CellColorKind
	Index   uint8 // palette index for CellColorANSI / CellColorANSI256
	R, G, B uint8 // components for CellColorRGB
}

// AttrMask is the resolved SGR attribute bitset of a cell.
type AttrMask uint16

const (
	AttrBold AttrMask = 1 << iota
	AttrFaint
	AttrItalic
	AttrUnderline
	AttrBlink
	AttrReverse
	AttrStrikethrough
)
