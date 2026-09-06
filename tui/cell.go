package tui

// Cell is one terminal grid cell. Content holds a COMPLETE grapheme cluster —
// never a partial one, never more than one. This is the tcell v3 / vaxis /
// bubbletea-v2 convergence (
// https://mitchellh.com/writing/grapheme-clusters-in-terminals).
//
// Width is cached at write time (measured once by Surface.SetCell, under the
// Surface's width policy), not recomputed per frame. Attrs is the style
// payload already resolved to output form: Surfaces resolve
// style.Style through the theme + capability context at paint time and stamp
// the concrete CellAttrs on the cell, so the diff, the flush path, and the
// tui/term emitter never consult a Theme. Cell is comparable, and cell
// equality — the entire dirty test — is one ==
type Cell struct {
	Content string    // one grapheme cluster; "" on a wide-cell continuation
	Width   uint8     // display columns: 1 or 2; 0 marks a continuation cell
	Attrs   CellAttrs // resolved style payload
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
