// Package qr encodes text as a QR code (Model 2): every version from 1 to 40, the four error
// correction levels, and the numeric, alphanumeric and byte modes. It draws nothing: a Code is
// the grid of modules, which a caller renders in whatever medium it has, as cells of a terminal,
// pixels or SVG.
package qr

import (
	"github.com/yongjohnlee80/golib/errs"
)

// Level is how much of the code may be lost or misread and still decode.
type Level uint8

const (
	// L recovers about 7% of the codewords.
	L Level = iota
	// M recovers about 15%.
	M
	// Q recovers about 25%.
	Q
	// H recovers about 30%.
	H
)

// Valid reports whether l is one of the four levels.
func (l Level) Valid() bool { return l <= H }

func (l Level) String() string {
	switch l {
	case L:
		return "L"
	case M:
		return "M"
	case Q:
		return "Q"
	case H:
		return "H"
	}
	return "Level(?)"
}

// formatBits are the two bits a level writes into the format information. They are not the
// levels' order: the symbol says M as 00 and L as 01.
func (l Level) formatBits() int {
	return [...]int{L: 1, M: 0, Q: 3, H: 2}[l]
}

var (
	// ErrLevel is a Level that is not one of L, M, Q and H.
	ErrLevel = errs.Sentinel(errs.ErrInvalidArgument, "qr: unknown error correction level")
	// ErrTooLong is data that does not fit in a version 40 code at the level asked for.
	ErrTooLong = errs.Sentinel(errs.ErrInvalidArgument, "qr: data too long for a QR code at this level")
)

// Code is an encoded QR code: Size × Size modules, without the quiet zone around it, which a
// renderer adds (four modules of the light colour on every side is what scanners expect).
type Code struct {
	// Size is the number of modules on a side: 17 + 4 × Version.
	Size int
	// Version is from 1 to 40, the smallest that holds the data at Level.
	Version int
	// Level is the error correction level the code was made at.
	Level Level
	// Mask is the data mask, from 0 to 7, chosen as the one that leaves the fewest patterns a
	// scanner could misread.
	Mask int

	dark []bool // row-major: the module at (x, y) is dark[y*Size+x]
}

// Black reports whether the module at column x, row y is dark. Outside the code it is false: the
// quiet zone is light.
func (c *Code) Black(x, y int) bool {
	if x < 0 || y < 0 || x >= c.Size || y >= c.Size {
		return false
	}
	return c.dark[y*c.Size+x]
}

// Encode makes the QR code of text at level, in the smallest version that holds it. Text of
// digits alone is encoded in numeric mode; text of digits, upper-case letters and the characters
// space $ % * + - . / : in alphanumeric mode; anything else as its UTF-8 bytes, in byte mode,
// which scanners read as UTF-8.
func Encode(text string, level Level) (*Code, error) {
	return encode([]byte(text), modeFor(text), level, 0, -1)
}

// EncodeBytes makes the QR code of data at level, in byte mode, in the smallest version that
// holds it.
func EncodeBytes(data []byte, level Level) (*Code, error) {
	return encode(data, byteMode, level, 0, -1)
}

// encode encodes data in mode at level. A version of 0 is the smallest that holds it, and a mask
// of -1 the one with the lowest penalty; the tests fix both to compare a code module for module
// with another encoder's.
func encode(data []byte, m mode, level Level, version, mask int) (*Code, error) {
	if !level.Valid() {
		return nil, ErrLevel
	}
	if version == 0 {
		version = smallestVersion(len(data), m, level)
		if version == 0 {
			return nil, ErrTooLong
		}
	}
	if bitLength(len(data), m, version) > 8*dataCodewords(version, level) {
		return nil, ErrTooLong
	}
	codewords := addErrorCorrection(dataBits(data, m, version, level), version, level)
	g := newGrid(version)
	g.drawFunctionPatterns()
	g.placeCodewords(codewords)
	if mask < 0 {
		mask = g.bestMask(level)
	}
	g.applyMask(mask)
	g.drawFormat(level, mask)
	return &Code{Size: g.size, Version: version, Level: level, Mask: mask, dark: g.dark}, nil
}
