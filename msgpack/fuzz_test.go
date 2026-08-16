package msgpack

import (
	"bytes"
	"math"
	"testing"
)

// equalValue compares two decoded values semantically. Byte-comparing
// re-encodings is invalid: Go map iteration order randomizes pair order on
// the wire. NaN compares equal to NaN (round-trip preservation, not IEEE
// comparison semantics).
func equalValue(a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && (x == y || (math.IsNaN(x) && math.IsNaN(y)))
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equalValue(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, present := y[k]
			if !present || !equalValue(v, w) {
				return false
			}
		}
		return true
	case []byte:
		y, ok := b.([]byte)
		return ok && bytes.Equal(x, y)
	case Ext:
		y, ok := b.(Ext)
		return ok && x.Type == y.Type && bytes.Equal(x.Data, y.Data)
	default:
		return a == b
	}
}

// FuzzDecode enforces the R4 contract: no input panics the decoder, and any
// successfully decoded value re-encodes and decodes back to an equivalent
// value (encode∘decode stability).
func FuzzDecode(f *testing.F) {
	seeds := [][]byte{
		{0xc0}, {0xc2}, {0xc3}, {0x00}, {0x7f}, {0xe0}, {0xff},
		{0xcc, 0xff}, {0xcd, 0xff, 0xff}, {0xce, 0xff, 0xff, 0xff, 0xff},
		{0xcf, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		{0xd0, 0x80}, {0xd1, 0x80, 0x00}, {0xd2, 0x80, 0x00, 0x00, 0x00},
		{0xd3, 0x80, 0, 0, 0, 0, 0, 0, 0},
		{0xca, 0x3f, 0xc0, 0x00, 0x00},
		{0xcb, 0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2d, 0x18},
		{0xa3, 'a', 'b', 'c'},
		{0xd9, 0x03, 'x', 'y', 'z'},
		{0xc4, 0x02, 0x01, 0x02},
		{0x92, 0x01, 0xa1, 'x'},
		{0x81, 0xa1, 'k', 0x2a},
		{0xd4, 0x00, 0x01},
		{0xc7, 0x03, 0x08, 1, 2, 3},
		{0xdc, 0x00, 0x11, 0xc0, 0xc0, 0xc0, 0xc0, 0xc0, 0xc0, 0xc0, 0xc0,
			0xc0, 0xc0, 0xc0, 0xc0, 0xc0, 0xc0, 0xc0, 0xc0, 0xc0},
		// adversarial seeds
		{0xc1},
		{0xdb, 0xff, 0xff, 0xff, 0xff},
		{0xdd, 0xff, 0xff, 0xff, 0xff},
		{0xdf, 0xff, 0xff, 0xff, 0xff},
		{0x91, 0x91, 0x91, 0x91, 0x91, 0xc0},
		{0x81, 0x01, 0xa1, 'v'},
		{0xd9},
		{},
	}
	for _, s := range seeds {
		f.Add(s)
	}

	// Small limits keep the fuzzer fast while exercising every limit branch.
	lim := &Limits{MaxDepth: 16, MaxStrBytes: 1 << 12, MaxBinBytes: 1 << 12, MaxElements: 1 << 10}

	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := Unmarshal(data, lim)
		if err != nil {
			return // rejection is fine; panicking is not
		}
		// Re-encode: every decoded value must be encodable...
		b, err := Marshal(v)
		if err != nil {
			t.Fatalf("decoded value failed to re-encode: %#v: %v", v, err)
		}
		// ...and decode back to a semantically equal value (stability).
		v2, err := Unmarshal(b, lim)
		if err != nil {
			t.Fatalf("re-encoded bytes failed to decode: % x: %v", b, err)
		}
		if !equalValue(v, v2) {
			t.Fatalf("unstable round-trip:\n gen1: %#v\n gen2: %#v", v, v2)
		}
	})
}

// FuzzEncodeDecodeInts drives the integer width-selection boundaries.
func FuzzEncodeDecodeInts(f *testing.F) {
	for _, v := range []int64{0, 1, -1, 127, 128, -32, -33, 255, 256, -128,
		-129, 65535, 65536, -32768, -32769, 1 << 31, -(1 << 31), 1<<63 - 1, -(1 << 63)} {
		f.Add(v)
	}
	f.Fuzz(func(t *testing.T, v int64) {
		b, err := Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Unmarshal(b, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != v {
			t.Fatalf("int64 %d round-tripped to %v (%T)", v, got, got)
		}
	})
}
