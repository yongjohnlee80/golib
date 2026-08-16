package msgpack

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Decode reads exactly one MessagePack value from r under lim (nil lim or
// zero fields use DefaultLimits values). Untrusted-input contract: no input
// panics the decoder; declared sizes are validated against per-item AND
// whole-decode aggregate limits before allocation; truncation, the reserved
// 0xc1 byte, depth bombs, and oversized declarations return typed errors.
//
// A clean end-of-stream — the reader is exhausted before ANY byte of a
// value — returns io.EOF, so transports can tell a polite peer hang-up
// from a truncated value (ErrMalformed).
func Decode(r *bufio.Reader, lim *Limits) (any, error) {
	if lim == nil {
		lim = DefaultLimits()
	}
	n := lim.normalized()
	st := &decodeState{lim: n, nodesLeft: n.MaxTotalElements, bytesLeft: n.MaxTotalBytes}
	if _, err := r.Peek(1); err == io.EOF {
		return nil, io.EOF
	}
	return decodeValue(r, st, 0)
}

// Unmarshal decodes one value from b and requires the input to be fully
// consumed (empty input and trailing bytes are both ErrMalformed).
func Unmarshal(b []byte, lim *Limits) (any, error) {
	r := bufio.NewReader(bytes.NewReader(b))
	v, err := Decode(r, lim)
	if err == io.EOF {
		return nil, fmt.Errorf("%w: empty input", ErrMalformed)
	}
	if err != nil {
		return nil, err
	}
	if _, err := r.ReadByte(); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing bytes after value", ErrMalformed)
	}
	return v, nil
}

func decodeValue(r *bufio.Reader, st *decodeState, depth int) (any, error) {
	if depth > st.lim.MaxDepth {
		return nil, ErrDepthExceeded
	}
	if err := st.chargeNode(); err != nil {
		return nil, err
	}
	b, err := r.ReadByte()
	if err != nil {
		return nil, wrapEOF(err)
	}

	switch {
	case b <= 0x7f: // positive fixint
		return int64(b), nil
	case b >= 0xe0: // negative fixint
		return int64(int8(b)), nil
	case b >= 0x80 && b <= 0x8f: // fixmap
		return decodeMap(r, st, depth, int(b&0x0f))
	case b >= 0x90 && b <= 0x9f: // fixarray
		return decodeArray(r, st, depth, int(b&0x0f))
	case b >= 0xa0 && b <= 0xbf: // fixstr
		return decodeStrBody(r, st, int(b&0x1f))
	}

	switch b {
	case 0xc0:
		return nil, nil
	case 0xc1:
		return nil, fmt.Errorf("%w: reserved byte 0xc1", ErrMalformed)
	case 0xc2:
		return false, nil
	case 0xc3:
		return true, nil

	case 0xc4, 0xc5, 0xc6: // bin8/16/32
		n, err := readLen(r, b-0xc4)
		if err != nil {
			return nil, err
		}
		if n > int64(st.lim.MaxBinBytes) {
			return nil, fmt.Errorf("%w: bin of %d bytes", ErrLimitExceeded, n)
		}
		if err := st.chargeBytes(int(n)); err != nil {
			return nil, err
		}
		return readBytes(r, int(n))

	case 0xc7, 0xc8, 0xc9: // ext8/16/32
		n, err := readLen(r, b-0xc7)
		if err != nil {
			return nil, err
		}
		if n > int64(st.lim.MaxBinBytes) {
			return nil, fmt.Errorf("%w: ext of %d bytes", ErrLimitExceeded, n)
		}
		return decodeExtBody(r, st, int(n))

	case 0xca: // float32
		u, err := readUint32(r)
		if err != nil {
			return nil, err
		}
		return float64(math.Float32frombits(u)), nil
	case 0xcb: // float64
		u, err := readUint64(r)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(u), nil

	case 0xcc: // uint8
		v, err := r.ReadByte()
		if err != nil {
			return nil, wrapEOF(err)
		}
		return int64(v), nil
	case 0xcd: // uint16
		v, err := readUint16(r)
		if err != nil {
			return nil, err
		}
		return int64(v), nil
	case 0xce: // uint32
		v, err := readUint32(r)
		if err != nil {
			return nil, err
		}
		return int64(v), nil
	case 0xcf: // uint64
		v, err := readUint64(r)
		if err != nil {
			return nil, err
		}
		if v > math.MaxInt64 {
			return v, nil // uint64 only when it cannot fit int64
		}
		return int64(v), nil

	case 0xd0: // int8
		v, err := r.ReadByte()
		if err != nil {
			return nil, wrapEOF(err)
		}
		return int64(int8(v)), nil
	case 0xd1: // int16
		v, err := readUint16(r)
		if err != nil {
			return nil, err
		}
		return int64(int16(v)), nil
	case 0xd2: // int32
		v, err := readUint32(r)
		if err != nil {
			return nil, err
		}
		return int64(int32(v)), nil
	case 0xd3: // int64
		v, err := readUint64(r)
		if err != nil {
			return nil, err
		}
		return int64(v), nil

	case 0xd4, 0xd5, 0xd6, 0xd7, 0xd8: // fixext 1/2/4/8/16
		return decodeExtBody(r, st, 1<<(b-0xd4))

	case 0xd9, 0xda, 0xdb: // str8/16/32
		n, err := readLen(r, b-0xd9)
		if err != nil {
			return nil, err
		}
		if n > int64(st.lim.MaxStrBytes) {
			return nil, fmt.Errorf("%w: str of %d bytes", ErrLimitExceeded, n)
		}
		return decodeStrBody(r, st, int(n))

	case 0xdc, 0xdd: // array16/32
		n, err := readLen16or32(r, b == 0xdd)
		if err != nil {
			return nil, err
		}
		if n > int64(st.lim.MaxElements) {
			return nil, fmt.Errorf("%w: array of %d elements", ErrLimitExceeded, n)
		}
		return decodeArray(r, st, depth, int(n))

	case 0xde, 0xdf: // map16/32
		n, err := readLen16or32(r, b == 0xdf)
		if err != nil {
			return nil, err
		}
		if n > int64(st.lim.MaxElements) {
			return nil, fmt.Errorf("%w: map of %d pairs", ErrLimitExceeded, n)
		}
		return decodeMap(r, st, depth, int(n))
	}

	return nil, fmt.Errorf("%w: unknown type byte 0x%02x", ErrMalformed, b)
}

func decodeStrBody(r *bufio.Reader, st *decodeState, n int) (string, error) {
	if n > st.lim.MaxStrBytes {
		return "", fmt.Errorf("%w: str of %d bytes", ErrLimitExceeded, n)
	}
	if err := st.chargeBytes(n); err != nil {
		return "", err
	}
	b, err := readBytes(r, n)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeArray(r *bufio.Reader, st *decodeState, depth, n int) ([]any, error) {
	if n > st.lim.MaxElements {
		return nil, fmt.Errorf("%w: array of %d elements", ErrLimitExceeded, n)
	}
	out := make([]any, 0, min(n, prealloc))
	for i := 0; i < n; i++ {
		v, err := decodeValue(r, st, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func decodeMap(r *bufio.Reader, st *decodeState, depth, n int) (map[string]any, error) {
	if n > st.lim.MaxElements {
		return nil, fmt.Errorf("%w: map of %d pairs", ErrLimitExceeded, n)
	}
	out := make(map[string]any, min(n, prealloc))
	for i := 0; i < n; i++ {
		k, err := decodeValue(r, st, depth+1)
		if err != nil {
			return nil, err
		}
		key, ok := k.(string)
		if !ok {
			return nil, fmt.Errorf("%w: got %T", ErrNonStringKey, k)
		}
		v, err := decodeValue(r, st, depth+1)
		if err != nil {
			return nil, err
		}
		out[key] = v
	}
	return out, nil
}

func decodeExtBody(r *bufio.Reader, st *decodeState, n int) (Ext, error) {
	if err := st.chargeBytes(n); err != nil {
		return Ext{}, err
	}
	t, err := r.ReadByte()
	if err != nil {
		return Ext{}, wrapEOF(err)
	}
	data, err := readBytes(r, n)
	if err != nil {
		return Ext{}, err
	}
	return Ext{Type: int8(t), Data: data}, nil
}

// readLen reads a 1/2/4-byte big-endian length selected by width 0/1/2.
// It returns int64, NOT int: on 32-bit platforms int(uint32) wraps
// 0xffffffff to -1, which would slip under every limit check and panic
// make with a negative cap. Callers validate against Limits (whose fields
// are far below MaxInt32) before narrowing to int.
func readLen(r *bufio.Reader, width byte) (int64, error) {
	switch width {
	case 0:
		v, err := r.ReadByte()
		if err != nil {
			return 0, wrapEOF(err)
		}
		return int64(v), nil
	case 1:
		v, err := readUint16(r)
		if err != nil {
			return 0, err
		}
		return int64(v), nil
	default:
		v, err := readUint32(r)
		if err != nil {
			return 0, err
		}
		return int64(v), nil
	}
}

// readLen16or32 mirrors readLen's int64 contract for array/map headers.
func readLen16or32(r *bufio.Reader, wide bool) (int64, error) {
	if wide {
		v, err := readUint32(r)
		if err != nil {
			return 0, err
		}
		return int64(v), nil
	}
	v, err := readUint16(r)
	if err != nil {
		return 0, err
	}
	return int64(v), nil
}

// readBytes reads exactly n bytes without trusting n for preallocation:
// data is accumulated in bounded chunks, so a forged length header cannot
// allocate memory the input never supplies.
func readBytes(r *bufio.Reader, n int) ([]byte, error) {
	if n == 0 {
		return []byte{}, nil
	}
	out := make([]byte, 0, min(n, prealloc))
	chunk := make([]byte, min(n, prealloc))
	remaining := n
	for remaining > 0 {
		c := min(remaining, len(chunk))
		if _, err := io.ReadFull(r, chunk[:c]); err != nil {
			return nil, wrapEOF(err)
		}
		out = append(out, chunk[:c]...)
		remaining -= c
	}
	return out, nil
}

func readUint16(r *bufio.Reader) (uint16, error) {
	var b [2]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, wrapEOF(err)
	}
	return binary.BigEndian.Uint16(b[:]), nil
}

func readUint32(r *bufio.Reader) (uint32, error) {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, wrapEOF(err)
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

func readUint64(r *bufio.Reader) (uint64, error) {
	var b [8]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, wrapEOF(err)
	}
	return binary.BigEndian.Uint64(b[:]), nil
}

// wrapEOF folds io.EOF/io.ErrUnexpectedEOF INSIDE a value into ErrMalformed
// (truncation) while passing real I/O errors through. A clean pre-value EOF
// never reaches here — Decode peeks for it first.
func wrapEOF(err error) error {
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return fmt.Errorf("%w: truncated input", ErrMalformed)
	}
	return err
}
