package vector

import (
	"encoding/binary"
	"math"
	"math/bits"
)

// Normalize is v scaled to length 1, in a new slice: the dot product of two normalized vectors is
// their cosine. A zero vector stays zero.
func Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	out := make([]float32, len(v))
	if sum == 0 {
		return out
	}
	n := math.Sqrt(sum)
	for i, x := range v {
		out[i] = float32(float64(x) / n)
	}
	return out
}

// Dot is the dot product of a and b over their common length, summed in float64.
func Dot(a, b []float32) float64 {
	var s float64
	for i := range min(len(a), len(b)) {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

// SignBits is v's 1-bit code: bit i is set when dimension i is positive. Two vectors whose codes
// differ in few bits point roughly the same way, so the code ranks candidates cheaply before their
// float vectors are compared.
func SignBits(v []float32) []uint64 {
	out := make([]uint64, (len(v)+63)/64)
	for i, x := range v {
		if x > 0 {
			out[i/64] |= 1 << (i % 64)
		}
	}
	return out
}

// Hamming is the number of bits in which a and b differ, over their common length.
func Hamming(a, b []uint64) int {
	d := 0
	for i := range min(len(a), len(b)) {
		d += bits.OnesCount64(a[i] ^ b[i])
	}
	return d
}

// EncodeBits is a code as little-endian bytes, 8 per word.
func EncodeBits(ws []uint64) []byte {
	b := make([]byte, 8*len(ws))
	for i, w := range ws {
		binary.LittleEndian.PutUint64(b[8*i:], w)
	}
	return b
}

// DecodeBits reads EncodeBits' bytes; a trailing partial word is ignored.
func DecodeBits(b []byte) []uint64 {
	out := make([]uint64, len(b)/8)
	for i := range out {
		out[i] = binary.LittleEndian.Uint64(b[8*i:])
	}
	return out
}

// EncodeFloats is a vector as little-endian IEEE 754 bytes, 4 per dimension.
func EncodeFloats(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

// DecodeFloats reads EncodeFloats' bytes; a trailing partial dimension is ignored.
func DecodeFloats(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}
