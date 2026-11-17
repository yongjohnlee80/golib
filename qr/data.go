package qr

import "strings"

// mode is how the data's characters become bits.
type mode uint8

const (
	numericMode      mode = iota // three digits in 10 bits
	alphanumericMode             // two characters of alphanumeric in 11 bits
	byteMode                     // a byte in 8 bits
)

// alphanumeric is the alphanumeric mode's character set, each character's value its index.
const alphanumeric = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ $%*+-./:"

// modeFor is the most compact single mode that encodes all of text.
func modeFor(text string) mode {
	m := numericMode
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c >= '0' && c <= '9':
		case strings.IndexByte(alphanumeric, c) >= 0:
			m = max(m, alphanumericMode)
		default:
			return byteMode
		}
	}
	return m
}

// indicator is the mode's four-bit indicator.
func (m mode) indicator() uint32 {
	return [...]uint32{numericMode: 0b0001, alphanumericMode: 0b0010, byteMode: 0b0100}[m]
}

// countBits is the width of the character count for the mode at a version: it grows twice, at
// versions 10 and 27.
func (m mode) countBits(version int) int {
	widths := [...][3]int{
		numericMode:      {10, 12, 14},
		alphanumericMode: {9, 11, 13},
		byteMode:         {8, 16, 16},
	}[m]
	switch {
	case version <= 9:
		return widths[0]
	case version <= 26:
		return widths[1]
	}
	return widths[2]
}

// bitLength is the length in bits of n characters in mode m at a version, with the mode
// indicator and the character count, before the terminator and padding.
func bitLength(n int, m mode, version int) int {
	bits := 4 + m.countBits(version)
	switch m {
	case numericMode:
		bits += 10*(n/3) + [...]int{0, 4, 7}[n%3]
	case alphanumericMode:
		bits += 11*(n/2) + 6*(n%2)
	default:
		bits += 8 * n
	}
	return bits
}

// smallestVersion is the smallest version that holds n characters of mode m at a level, or 0 when
// none does.
func smallestVersion(n int, m mode, level Level) int {
	for v := 1; v <= 40; v++ {
		if bitLength(n, m, v) <= 8*dataCodewords(v, level) {
			return v
		}
	}
	return 0
}

// bitBuffer collects bits, the first written the most significant.
type bitBuffer struct {
	bytes []byte
	n     int // bits written
}

// write appends the low width bits of v, most significant first.
func (b *bitBuffer) write(v uint32, width int) {
	for i := width - 1; i >= 0; i-- {
		if b.n%8 == 0 {
			b.bytes = append(b.bytes, 0)
		}
		if v>>uint(i)&1 == 1 {
			b.bytes[b.n/8] |= 0x80 >> uint(b.n%8)
		}
		b.n++
	}
}

// dataBits is the data codewords of a code: the segment (mode indicator, character count and the
// characters), a terminator of up to four zero bits, zero bits to the next byte, and the two pad
// bytes in turn until the version's data capacity is full. The caller has checked that it fits.
func dataBits(data []byte, m mode, version int, level Level) []byte {
	var b bitBuffer
	b.write(m.indicator(), 4)
	b.write(uint32(len(data)), m.countBits(version))
	switch m {
	case numericMode:
		for i := 0; i < len(data); i += 3 {
			group := data[i:min(i+3, len(data))]
			var v uint32
			for _, d := range group {
				v = v*10 + uint32(d-'0')
			}
			b.write(v, [...]int{0, 4, 7, 10}[len(group)])
		}
	case alphanumericMode:
		for i := 0; i < len(data); i += 2 {
			v := uint32(strings.IndexByte(alphanumeric, data[i]))
			if i+1 < len(data) {
				b.write(v*45+uint32(strings.IndexByte(alphanumeric, data[i+1])), 11)
			} else {
				b.write(v, 6)
			}
		}
	default:
		for _, c := range data {
			b.write(uint32(c), 8)
		}
	}
	capacity := 8 * dataCodewords(version, level)
	b.write(0, min(4, capacity-b.n))
	b.write(0, (8-b.n%8)%8)
	for pad := uint32(0xEC); b.n < capacity; pad ^= 0xEC ^ 0x11 {
		b.write(pad, 8)
	}
	return b.bytes
}
