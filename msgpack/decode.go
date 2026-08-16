package msgpack

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Decode reads exactly one MessagePack value from r under lim (nil lim uses
// DefaultLimits). Untrusted-input contract: no input panics the decoder;
// declared sizes are validated before allocation; truncation, the reserved
// 0xc1 byte, depth bombs, and oversized declarations return typed errors.
func Decode(r *bufio.Reader, lim *Limits) (any, error) {
	if lim == nil {
		lim = DefaultLimits()
	}
	return decodeValue(r, lim, 0)
}

// Unmarshal decodes one value from b and requires the input to be fully
// consumed (trailing bytes are ErrMalformed).
func Unmarshal(b []byte, lim *Limits) (any, error) {
	r := bufio.NewReader(bytes.NewReader(b))
	v, err := Decode(r, lim)
	if err != nil {
		return nil, err
	}
	if _, err := r.ReadByte(); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing bytes after value", ErrMalformed)
	}
	return v, nil
}

func decodeValue(r *bufio.Reader, lim *Limits, depth int) (any, error) {
	if depth > lim.MaxDepth {
		return nil, ErrDepthExceeded
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
		return decodeMap(r, lim, depth, int(b&0x0f))
	case b >= 0x90 && b <= 0x9f: // fixarray
		return decodeArray(r, lim, depth, int(b&0x0f))
	case b >= 0xa0 && b <= 0xbf: // fixstr
		return decodeStrBody(r, lim, int(b&0x1f))
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
		if n > lim.MaxBinBytes {
			return nil, fmt.Errorf("%w: bin of %d bytes", ErrLimitExceeded, n)
		}
		return readBytes(r, n)

	case 0xc7, 0xc8, 0xc9: // ext8/16/32
		n, err := readLen(r, b-0xc7)
		if err != nil {
			return nil, err
		}
		if n > lim.MaxBinBytes {
			return nil, fmt.Errorf("%w: ext of %d bytes", ErrLimitExceeded, n)
		}
		return decodeExtBody(r, n)

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
		return decodeExtBody(r, 1<<(b-0xd4))

	case 0xd9, 0xda, 0xdb: // str8/16/32
		n, err := readLen(r, b-0xd9)
		if err != nil {
			return nil, err
		}
		if n > lim.MaxStrBytes {
			return nil, fmt.Errorf("%w: str of %d bytes", ErrLimitExceeded, n)
		}
		return decodeStrBody(r, lim, n)

	case 0xdc, 0xdd: // array16/32
		n, err := readLen16or32(r, b == 0xdd)
		if err != nil {
			return nil, err
		}
		return decodeArray(r, lim, depth, n)

	case 0xde, 0xdf: // map16/32
		n, err := readLen16or32(r, b == 0xdf)
		if err != nil {
			return nil, err
		}
		return decodeMap(r, lim, depth, n)
	}

	return nil, fmt.Errorf("%w: unknown type byte 0x%02x", ErrMalformed, b)
}

func decodeStrBody(r *bufio.Reader, lim *Limits, n int) (string, error) {
	if n > lim.MaxStrBytes {
		return "", fmt.Errorf("%w: str of %d bytes", ErrLimitExceeded, n)
	}
	b, err := readBytes(r, n)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeArray(r *bufio.Reader, lim *Limits, depth, n int) ([]any, error) {
	if n > lim.MaxElements {
		return nil, fmt.Errorf("%w: array of %d elements", ErrLimitExceeded, n)
	}
	out := make([]any, 0, min(n, prealloc))
	for i := 0; i < n; i++ {
		v, err := decodeValue(r, lim, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func decodeMap(r *bufio.Reader, lim *Limits, depth, n int) (map[string]any, error) {
	if n > lim.MaxElements {
		return nil, fmt.Errorf("%w: map of %d pairs", ErrLimitExceeded, n)
	}
	out := make(map[string]any, min(n, prealloc))
	for i := 0; i < n; i++ {
		k, err := decodeValue(r, lim, depth+1)
		if err != nil {
			return nil, err
		}
		key, ok := k.(string)
		if !ok {
			return nil, fmt.Errorf("%w: got %T", ErrNonStringKey, k)
		}
		v, err := decodeValue(r, lim, depth+1)
		if err != nil {
			return nil, err
		}
		out[key] = v
	}
	return out, nil
}

func decodeExtBody(r *bufio.Reader, n int) (Ext, error) {
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
func readLen(r *bufio.Reader, width byte) (int, error) {
	switch width {
	case 0:
		v, err := r.ReadByte()
		if err != nil {
			return 0, wrapEOF(err)
		}
		return int(v), nil
	case 1:
		v, err := readUint16(r)
		if err != nil {
			return 0, err
		}
		return int(v), nil
	default:
		v, err := readUint32(r)
		if err != nil {
			return 0, err
		}
		return int(v), nil
	}
}

func readLen16or32(r *bufio.Reader, wide bool) (int, error) {
	if wide {
		v, err := readUint32(r)
		if err != nil {
			return 0, err
		}
		return int(v), nil
	}
	v, err := readUint16(r)
	if err != nil {
		return 0, err
	}
	return int(v), nil
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

// wrapEOF folds io.EOF/io.ErrUnexpectedEOF into ErrMalformed (truncated
// value) while passing real I/O errors through.
func wrapEOF(err error) error {
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return fmt.Errorf("%w: truncated input", ErrMalformed)
	}
	return err
}
