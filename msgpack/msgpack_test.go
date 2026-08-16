package msgpack

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
)

// roundTrip marshals v, unmarshals the bytes, and returns the result.
func roundTrip(t *testing.T, v any) any {
	t.Helper()
	b, err := Marshal(v)
	if err != nil {
		t.Fatalf("Marshal(%v): %v", v, err)
	}
	out, err := Unmarshal(b, nil)
	if err != nil {
		t.Fatalf("Unmarshal(% x): %v", b, err)
	}
	return out
}

func TestRoundTripScalars(t *testing.T) {
	cases := []struct {
		in   any
		want any
	}{
		{nil, nil},
		{true, true},
		{false, false},

		// positive fixint boundary
		{int64(0), int64(0)},
		{int64(1), int64(1)},
		{int64(127), int64(127)},
		// uint8/16/32/64 thresholds
		{int64(128), int64(128)},
		{int64(255), int64(255)},
		{int64(256), int64(256)},
		{int64(65535), int64(65535)},
		{int64(65536), int64(65536)},
		{int64(math.MaxUint32), int64(math.MaxUint32)},
		{int64(math.MaxUint32 + 1), int64(math.MaxUint32 + 1)},
		{int64(math.MaxInt64), int64(math.MaxInt64)},
		// negative fixint boundary
		{int64(-1), int64(-1)},
		{int64(-32), int64(-32)},
		// int8/16/32/64 thresholds
		{int64(-33), int64(-33)},
		{int64(-128), int64(-128)},
		{int64(-129), int64(-129)},
		{int64(-32768), int64(-32768)},
		{int64(-32769), int64(-32769)},
		{int64(math.MinInt32), int64(math.MinInt32)},
		{int64(math.MinInt32 - 1), int64(math.MinInt32 - 1)},
		{int64(math.MinInt64), int64(math.MinInt64)},
		// uint64 above MaxInt64 stays uint64
		{uint64(math.MaxUint64), uint64(math.MaxUint64)},
		{uint64(math.MaxInt64 + 1), uint64(math.MaxInt64 + 1)},
		// uint64 within int64 range decodes as int64
		{uint64(42), int64(42)},
		// narrow Go int types widen
		{int8(-5), int64(-5)},
		{int16(-300), int64(-300)},
		{int32(70000), int64(70000)},
		{int(12), int64(12)},
		{uint8(200), int64(200)},
		{uint16(60000), int64(60000)},
		{uint32(4000000000), int64(4000000000)},
		{uint(7), int64(7)},

		// floats: float32 widens to float64 on decode
		{float64(0), float64(0)},
		{float64(3.14159), float64(3.14159)},
		{float64(math.MaxFloat64), float64(math.MaxFloat64)},
		{float64(math.SmallestNonzeroFloat64), float64(math.SmallestNonzeroFloat64)},
		{float64(math.Inf(1)), math.Inf(1)},
		{float64(math.Inf(-1)), math.Inf(-1)},
		{float32(1.5), float64(1.5)},

		// strings across width classes
		{"", ""},
		{"a", "a"},
		{strings.Repeat("x", 31), strings.Repeat("x", 31)},       // fixstr max
		{strings.Repeat("x", 32), strings.Repeat("x", 32)},       // str8 min
		{strings.Repeat("x", 255), strings.Repeat("x", 255)},     // str8 max
		{strings.Repeat("x", 256), strings.Repeat("x", 256)},     // str16 min
		{strings.Repeat("x", 65536), strings.Repeat("x", 65536)}, // str32 min
		{"héllo wörld — ünïcode", "héllo wörld — ünïcode"},

		// bin across width classes
		{[]byte{}, []byte{}},
		{[]byte{0x00, 0xff}, []byte{0x00, 0xff}},
		{bytes.Repeat([]byte{7}, 255), bytes.Repeat([]byte{7}, 255)},
		{bytes.Repeat([]byte{7}, 256), bytes.Repeat([]byte{7}, 256)},
		{bytes.Repeat([]byte{7}, 65536), bytes.Repeat([]byte{7}, 65536)},
	}
	for _, tc := range cases {
		got := roundTrip(t, tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("round-trip %T(%v): got %T(%v), want %T(%v)",
				tc.in, tc.in, got, got, tc.want, tc.want)
		}
	}
}

func TestRoundTripNaN(t *testing.T) {
	got := roundTrip(t, math.NaN())
	f, ok := got.(float64)
	if !ok || !math.IsNaN(f) {
		t.Fatalf("NaN round-trip: got %T(%v)", got, got)
	}
}

func TestRoundTripContainers(t *testing.T) {
	cases := []any{
		[]any{},
		[]any{int64(1), "two", true, nil, 3.5},
		map[string]any{},
		map[string]any{"k": int64(1), "nested": map[string]any{"deep": []any{"x"}}},
		// array16 (>15 elements)
		func() []any {
			out := make([]any, 20)
			for i := range out {
				out[i] = int64(i)
			}
			return out
		}(),
		// map16 (>15 pairs)
		func() map[string]any {
			out := make(map[string]any, 20)
			for _, k := range strings.Split("abcdefghijklmnopqrst", "") {
				out[k] = k
			}
			return out
		}(),
	}
	for _, in := range cases {
		got := roundTrip(t, in)
		if !reflect.DeepEqual(got, in) {
			t.Errorf("round-trip %#v: got %#v", in, got)
		}
	}
}

func TestRoundTripArray32(t *testing.T) {
	in := make([]any, 70000) // > MaxUint16 → array32 header
	for i := range in {
		in[i] = int64(i % 3)
	}
	got := roundTrip(t, in)
	if !reflect.DeepEqual(got, in) {
		t.Fatal("array32 round-trip mismatch")
	}
}

func TestRoundTripExt(t *testing.T) {
	cases := []Ext{
		{Type: 0, Data: []byte{1}},                       // fixext1 — nvim Buffer
		{Type: 1, Data: []byte{1, 2}},                    // fixext2 — nvim Window
		{Type: 2, Data: []byte{1, 2, 3, 4}},              // fixext4 — nvim Tabpage
		{Type: 5, Data: []byte{1, 2, 3, 4, 5, 6, 7, 8}},  // fixext8
		{Type: 6, Data: bytes.Repeat([]byte{9}, 16)},     // fixext16
		{Type: 7, Data: []byte{}},                        // ext8 len 0
		{Type: 8, Data: []byte{1, 2, 3}},                 // ext8 (non-power len)
		{Type: -1, Data: []byte{0, 0, 0, 1}},             // timestamp passthrough
		{Type: 9, Data: bytes.Repeat([]byte{1}, 300)},    // ext16
		{Type: 10, Data: bytes.Repeat([]byte{1}, 70000)}, // ext32
	}
	for _, in := range cases {
		got := roundTrip(t, in)
		e, ok := got.(Ext)
		if !ok {
			t.Fatalf("ext round-trip: got %T", got)
		}
		if e.Type != in.Type || !bytes.Equal(e.Data, in.Data) {
			t.Errorf("ext round-trip type=%d len=%d: got type=%d len=%d",
				in.Type, len(in.Data), e.Type, len(e.Data))
		}
	}
}

func TestEncodeUnsupportedType(t *testing.T) {
	for _, v := range []any{struct{}{}, map[int]any{1: "x"}, []string{"a"}, make(chan int)} {
		if _, err := Marshal(v); !errors.Is(err, ErrUnsupportedType) {
			t.Errorf("Marshal(%T): err = %v, want ErrUnsupportedType", v, err)
		}
	}
}

// --- adversarial table (R4) ---

func TestDecodeMalformed(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want error
	}{
		{"empty input", []byte{}, ErrMalformed},
		{"reserved 0xc1", []byte{0xc1}, ErrMalformed},
		{"truncated str header", []byte{0xd9}, ErrMalformed},
		{"truncated str body", []byte{0xa5, 'a', 'b'}, ErrMalformed},
		{"truncated bin header", []byte{0xc5, 0x01}, ErrMalformed},
		{"truncated bin body", []byte{0xc4, 0x05, 1, 2}, ErrMalformed},
		{"truncated uint16", []byte{0xcd, 0x01}, ErrMalformed},
		{"truncated uint32", []byte{0xce, 0x01, 0x02}, ErrMalformed},
		{"truncated uint64", []byte{0xcf, 1, 2, 3, 4, 5, 6, 7}, ErrMalformed},
		{"truncated int64", []byte{0xd3, 1, 2, 3}, ErrMalformed},
		{"truncated float32", []byte{0xca, 0x40}, ErrMalformed},
		{"truncated float64", []byte{0xcb, 0x40, 0x09}, ErrMalformed},
		{"truncated array element", []byte{0x92, 0x01}, ErrMalformed},
		{"truncated map value", []byte{0x81, 0xa1, 'k'}, ErrMalformed},
		{"truncated fixext1", []byte{0xd4}, ErrMalformed},
		{"truncated fixext16 body", []byte{0xd8, 0x01, 1, 2}, ErrMalformed},
		{"truncated ext8 header", []byte{0xc7}, ErrMalformed},
		{"trailing bytes", []byte{0xc0, 0x00}, ErrMalformed},
		{"non-string map key int", []byte{0x81, 0x01, 0xa1, 'v'}, ErrNonStringKey},
		{"non-string map key nil", []byte{0x81, 0xc0, 0xc0}, ErrNonStringKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Unmarshal(tc.in, nil); !errors.Is(err, tc.want) {
				t.Errorf("Unmarshal(% x): err = %v, want %v", tc.in, err, tc.want)
			}
		})
	}
}

func TestDecodeForgedLengths(t *testing.T) {
	// Giant declared sizes with no body: must fail via limits or truncation
	// WITHOUT allocating the declared amount.
	cases := []struct {
		name string
		in   []byte
	}{
		{"str32 4GiB", []byte{0xdb, 0xff, 0xff, 0xff, 0xff}},
		{"bin32 4GiB", []byte{0xc6, 0xff, 0xff, 0xff, 0xff}},
		{"ext32 4GiB", []byte{0xc9, 0xff, 0xff, 0xff, 0xff, 0x00}},
		{"array32 4G elements", []byte{0xdd, 0xff, 0xff, 0xff, 0xff}},
		{"map32 4G pairs", []byte{0xdf, 0xff, 0xff, 0xff, 0xff}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Unmarshal(tc.in, nil)
			if err == nil {
				t.Fatalf("Unmarshal(% x): expected error", tc.in)
			}
			if !errors.Is(err, ErrLimitExceeded) && !errors.Is(err, ErrMalformed) {
				t.Errorf("err = %v, want ErrLimitExceeded or ErrMalformed", err)
			}
		})
	}
}

func TestDecodeForgedLengthUnderLimit(t *testing.T) {
	// Declared size passes the limit but the body is absent: the chunked
	// reader must fail on truncation after bounded allocation, not pre-alloc
	// the full declared size.
	in := []byte{0xc5, 0xff, 0xff} // bin16 declaring 65535 bytes, zero supplied
	if _, err := Unmarshal(in, nil); !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}

func TestDecodeDepthBomb(t *testing.T) {
	// 100 nested single-element arrays exceeds MaxDepth 64.
	in := bytes.Repeat([]byte{0x91}, 100)
	in = append(in, 0xc0)
	if _, err := Unmarshal(in, nil); !errors.Is(err, ErrDepthExceeded) {
		t.Fatalf("err = %v, want ErrDepthExceeded", err)
	}
	// Same shape under the limit decodes fine.
	in = bytes.Repeat([]byte{0x91}, 60)
	in = append(in, 0xc0)
	if _, err := Unmarshal(in, nil); err != nil {
		t.Fatalf("depth 60: unexpected err %v", err)
	}
}

func TestDecodeCustomLimits(t *testing.T) {
	lim := &Limits{MaxDepth: 2, MaxStrBytes: 4, MaxBinBytes: 4, MaxElements: 2}

	if _, err := Unmarshal([]byte{0xa5, 'a', 'b', 'c', 'd', 'e'}, lim); !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("str over MaxStrBytes: err = %v", err)
	}
	if _, err := Unmarshal([]byte{0xc4, 0x05, 1, 2, 3, 4, 5}, lim); !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("bin over MaxBinBytes: err = %v", err)
	}
	if _, err := Unmarshal([]byte{0x93, 0xc0, 0xc0, 0xc0}, lim); !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("array over MaxElements: err = %v", err)
	}
	// depth 3 nested arrays over MaxDepth 2
	if _, err := Unmarshal([]byte{0x91, 0x91, 0x91, 0xc0}, lim); !errors.Is(err, ErrDepthExceeded) {
		t.Errorf("depth over MaxDepth: err = %v", err)
	}
}

func TestDecodeStreamLeavesRemainder(t *testing.T) {
	// Decode (not Unmarshal) must consume exactly one value and leave the
	// rest — the transport reads back-to-back messages from one reader.
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := Encode(w, "first"); err != nil {
		t.Fatal(err)
	}
	if err := Encode(w, int64(2)); err != nil {
		t.Fatal(err)
	}
	w.Flush()

	r := bufio.NewReader(&buf)
	v1, err := Decode(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := Decode(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v1 != "first" || v2 != int64(2) {
		t.Fatalf("got %v, %v", v1, v2)
	}
}

func TestDecodeCompactWireForms(t *testing.T) {
	// Hand-encoded wire bytes → expected values (spec fidelity, not just
	// self round-trip).
	cases := []struct {
		in   []byte
		want any
	}{
		{[]byte{0x00}, int64(0)},
		{[]byte{0x7f}, int64(127)},
		{[]byte{0xe0}, int64(-32)},
		{[]byte{0xff}, int64(-1)},
		{[]byte{0xc0}, nil},
		{[]byte{0xc2}, false},
		{[]byte{0xc3}, true},
		{[]byte{0xcc, 0xff}, int64(255)},
		{[]byte{0xd0, 0x81}, int64(-127)},
		{[]byte{0xca, 0x3f, 0xc0, 0x00, 0x00}, float64(1.5)},
		{[]byte{0xa3, 'a', 'b', 'c'}, "abc"},
		{[]byte{0x90}, []any{}},
		{[]byte{0x80}, map[string]any{}},
		{[]byte{0x92, 0x01, 0xa1, 'x'}, []any{int64(1), "x"}},
		{[]byte{0x81, 0xa1, 'k', 0x2a}, map[string]any{"k": int64(42)}},
	}
	for _, tc := range cases {
		got, err := Unmarshal(tc.in, nil)
		if err != nil {
			t.Errorf("Unmarshal(% x): %v", tc.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Unmarshal(% x): got %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

func TestEncodeCompactWireForms(t *testing.T) {
	// Encoder must pick the most compact representation.
	cases := []struct {
		in   any
		want []byte
	}{
		{int64(0), []byte{0x00}},
		{int64(127), []byte{0x7f}},
		{int64(128), []byte{0xcc, 0x80}},
		{int64(-1), []byte{0xff}},
		{int64(-32), []byte{0xe0}},
		{int64(-33), []byte{0xd0, 0xdf}},
		{"abc", []byte{0xa3, 'a', 'b', 'c'}},
		{[]any{}, []byte{0x90}},
		{map[string]any{}, []byte{0x80}},
		{Ext{Type: 0, Data: []byte{1}}, []byte{0xd4, 0x00, 0x01}},
	}
	for _, tc := range cases {
		got, err := Marshal(tc.in)
		if err != nil {
			t.Errorf("Marshal(%v): %v", tc.in, err)
			continue
		}
		if !bytes.Equal(got, tc.want) {
			t.Errorf("Marshal(%v): got % x, want % x", tc.in, got, tc.want)
		}
	}
}

// --- aggregate budgets (review finding 1) ---

// TestDecodeAggregateAmplificationAttack reproduces the review's
// amplification vector: sixteen sibling 1M-element nil arrays, each under
// the per-container MaxElements, ~16 MB on the wire but ~256 MB decoded.
// The aggregate node budget must refuse it under DefaultLimits.
func TestDecodeAggregateAmplificationAttack(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(0x90 | 16&0x0f) // deliberately wrong if 16 > 15
	buf.Reset()
	// array16 header for 16 elements
	buf.Write([]byte{0xdc, 0x00, 0x10})
	inner := make([]byte, 0, 5+1<<20)
	inner = append(inner, 0xdd, 0x00, 0x0f, 0x42, 0x40) // array32(1_000_000)
	inner = append(inner, bytes.Repeat([]byte{0xc0}, 1<<20)[:1000000]...)
	for i := 0; i < 16; i++ {
		buf.Write(inner)
	}
	_, err := Unmarshal(buf.Bytes(), nil)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("amplification message: err = %v, want ErrLimitExceeded", err)
	}
}

func TestDecodeAggregateElementBudget(t *testing.T) {
	lim := &Limits{MaxTotalElements: 10}
	// Three sibling 5-element arrays: every container is small, but the
	// whole decode is 3 containers + 15 nils + 1 root = 19 nodes.
	in := []byte{0x93, 0x95, 0xc0, 0xc0, 0xc0, 0xc0, 0xc0,
		0x95, 0xc0, 0xc0, 0xc0, 0xc0, 0xc0,
		0x95, 0xc0, 0xc0, 0xc0, 0xc0, 0xc0}
	if _, err := Unmarshal(in, lim); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("sibling arrays: err = %v, want ErrLimitExceeded", err)
	}
	// The same shape passes with room to spare.
	if _, err := Unmarshal(in, &Limits{MaxTotalElements: 100}); err != nil {
		t.Fatalf("under budget: %v", err)
	}
}

func TestDecodeAggregateByteBudget(t *testing.T) {
	lim := &Limits{MaxTotalBytes: 10}
	// Two 8-byte strings: each fine per-item, 16 aggregate payload bytes.
	in := []byte{0x92, 0xa8, 'a', 'a', 'a', 'a', 'a', 'a', 'a', 'a',
		0xa8, 'b', 'b', 'b', 'b', 'b', 'b', 'b', 'b'}
	if _, err := Unmarshal(in, lim); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("aggregate bytes: err = %v, want ErrLimitExceeded", err)
	}
	if _, err := Unmarshal(in, &Limits{MaxTotalBytes: 100}); err != nil {
		t.Fatalf("under budget: %v", err)
	}
}

func TestLimitsZeroFieldsFallBackToDefaults(t *testing.T) {
	// A zero-value Limits (or one with unset new fields) must decode with
	// full default protection, not zero budgets.
	v, err := Unmarshal([]byte{0x92, 0x01, 0xa1, 'x'}, &Limits{})
	if err != nil {
		t.Fatalf("zero Limits: %v", err)
	}
	if !reflect.DeepEqual(v, []any{int64(1), "x"}) {
		t.Fatalf("zero Limits decoded %#v", v)
	}
	// And a tightened single field still applies while the rest default.
	if _, err := Unmarshal([]byte{0x91, 0x91, 0xc0}, &Limits{MaxDepth: 1}); !errors.Is(err, ErrDepthExceeded) {
		t.Fatalf("partial Limits: err = %v, want ErrDepthExceeded", err)
	}
}

// --- clean EOF vs truncation (review should-fix 3) ---

func TestDecodeCleanEOF(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader(nil))
	if _, err := Decode(r, nil); err != io.EOF {
		t.Fatalf("empty stream: err = %v, want io.EOF", err)
	}
	// Between two values on one stream: first decodes, second sees io.EOF.
	b, _ := Marshal("only")
	r = bufio.NewReader(bytes.NewReader(b))
	if _, err := Decode(r, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(r, nil); err != io.EOF {
		t.Fatalf("post-value: err = %v, want io.EOF", err)
	}
	// Mid-value truncation stays ErrMalformed, never io.EOF.
	r = bufio.NewReader(bytes.NewReader([]byte{0xa5, 'a'}))
	if _, err := Decode(r, nil); !errors.Is(err, ErrMalformed) {
		t.Fatalf("truncated: err = %v, want ErrMalformed", err)
	}
	// Unmarshal keeps its one-shot contract: empty input is malformed.
	if _, err := Unmarshal(nil, nil); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Unmarshal(empty): err = %v, want ErrMalformed", err)
	}
}

// --- encoder cycle guard (review finding 4, outbound depth) ---

func TestEncodeCyclicValueFailsInsteadOfStackDeath(t *testing.T) {
	cycle := []any{nil}
	cycle[0] = cycle
	if _, err := Marshal(cycle); !errors.Is(err, ErrDepthExceeded) {
		t.Fatalf("cyclic slice: err = %v, want ErrDepthExceeded", err)
	}
	m := map[string]any{}
	m["self"] = m
	if _, err := Marshal(m); !errors.Is(err, ErrDepthExceeded) {
		t.Fatalf("cyclic map: err = %v, want ErrDepthExceeded", err)
	}
}
