package style

import "slices"

// SHARED VOCABULARIES — the text attributes and the cursor shapes, written once. A Style packs its
// attribute values in an AttrMask; tui's resolved cells (tui.CellAttrs.Mask), vt's cells (vt.Attr)
// and the web client's painter (handed the bits by the page) use the same bits, so a new attribute
// is one constant here. tui.AttrMask, tui.CursorShape, vt.Attr and vt.CursorShape alias these
// types; style sits below tui, so the home is here.

// AttrMask is a set of text attributes, one bit each.
type AttrMask uint16

const (
	AttrBold          AttrMask = 1 << iota // SGR 1
	AttrFaint                              // SGR 2
	AttrItalic                             // SGR 3
	AttrUnderline                          // SGR 4
	AttrBlink                              // SGR 5
	AttrReverse                            // SGR 7
	AttrStrikethrough                      // SGR 9
	AttrInvisible                          // SGR 8; a terminal's cells carry it, a Style has no setter for it
)

// AttrName is one attribute bit and its name.
type AttrName struct {
	Name string
	Bit  AttrMask
}

// attrNames is the catalog AttrNames copies; nothing outside this package reaches it.
var attrNames = [...]AttrName{
	{"bold", AttrBold}, {"faint", AttrFaint}, {"italic", AttrItalic}, {"underline", AttrUnderline},
	{"blink", AttrBlink}, {"reverse", AttrReverse}, {"strikethrough", AttrStrikethrough},
	{"invisible", AttrInvisible},
}

// AttrNames names each attribute bit, in bit order: the names the web client is handed with the
// values. It is a copy, so a caller's change reaches no other reader.
func AttrNames() []AttrName { return slices.Clone(attrNames[:]) }

// CursorShape selects the hardware cursor glyph (DECSCUSR).
type CursorShape uint8

const (
	CursorShapeDefault   CursorShape = iota // the terminal's configured default (DECSCUSR 0)
	CursorShapeBlock                        // DECSCUSR 1, 2
	CursorShapeUnderline                    // DECSCUSR 3, 4
	CursorShapeBar                          // DECSCUSR 5, 6
)

// Attrs is the attributes the style sets on: each one set and true. An attribute it leaves
// unset, or sets false, is not in the mask.
func (s Style) Attrs() AttrMask {
	var m AttrMask
	for _, k := range attrProps {
		if s.isSet(k) && s.attrs&attrBit(k) != 0 {
			m |= attrBit(k)
		}
	}
	return m
}

// WithAttrs sets on every attribute in m that a Style carries (all but AttrInvisible), leaving
// the others as they were.
func (s Style) WithAttrs(m AttrMask) Style {
	for _, k := range attrProps {
		if bit := attrBit(k); m&bit != 0 {
			s = s.setAttr(k, bit, true)
		}
	}
	return s
}

// attrProps are the attribute properties a Style carries.
var attrProps = []propKey{propBold, propFaint, propItalic, propUnderline, propBlink, propReverse, propStrikethrough}
