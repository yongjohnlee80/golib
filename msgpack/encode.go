package msgpack

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// Ext is a MessagePack application extension value (e.g. Neovim's
// Buffer/Window/Tabpage handles, types 0/1/2). Data is carried opaquely.
type Ext struct {
	Type int8
	Data []byte
}

// Encode writes one value in MessagePack encoding. The Go vocabulary is
// fixed (package doc); anything else returns ErrUnsupportedType.
func Encode(w *bufio.Writer, v any) error {
	switch x := v.(type) {
	case nil:
		return w.WriteByte(0xc0)
	case bool:
		if x {
			return w.WriteByte(0xc3)
		}
		return w.WriteByte(0xc2)
	case int:
		return encodeInt(w, int64(x))
	case int8:
		return encodeInt(w, int64(x))
	case int16:
		return encodeInt(w, int64(x))
	case int32:
		return encodeInt(w, int64(x))
	case int64:
		return encodeInt(w, x)
	case uint:
		return encodeUint(w, uint64(x))
	case uint8:
		return encodeUint(w, uint64(x))
	case uint16:
		return encodeUint(w, uint64(x))
	case uint32:
		return encodeUint(w, uint64(x))
	case uint64:
		return encodeUint(w, x)
	case float32:
		if err := w.WriteByte(0xca); err != nil {
			return err
		}
		return writeUint32(w, math.Float32bits(x))
	case float64:
		if err := w.WriteByte(0xcb); err != nil {
			return err
		}
		return writeUint64(w, math.Float64bits(x))
	case string:
		return encodeStr(w, x)
	case []byte:
		return encodeBin(w, x)
	case []any:
		if err := encodeArrayHeader(w, len(x)); err != nil {
			return err
		}
		for _, el := range x {
			if err := Encode(w, el); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		if err := encodeMapHeader(w, len(x)); err != nil {
			return err
		}
		for k, el := range x {
			if err := encodeStr(w, k); err != nil {
				return err
			}
			if err := Encode(w, el); err != nil {
				return err
			}
		}
		return nil
	case Ext:
		return encodeExt(w, x)
	default:
		return fmt.Errorf("%w: %T", ErrUnsupportedType, v)
	}
}

// Marshal encodes v into a byte slice.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := Encode(w, v); err != nil {
		return nil, err
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeInt(w *bufio.Writer, v int64) error {
	if v >= 0 {
		return encodeUint(w, uint64(v))
	}
	switch {
	case v >= -32:
		return w.WriteByte(byte(v)) // negative fixint
	case v >= math.MinInt8:
		if err := w.WriteByte(0xd0); err != nil {
			return err
		}
		return w.WriteByte(byte(int8(v)))
	case v >= math.MinInt16:
		if err := w.WriteByte(0xd1); err != nil {
			return err
		}
		return writeUint16(w, uint16(int16(v)))
	case v >= math.MinInt32:
		if err := w.WriteByte(0xd2); err != nil {
			return err
		}
		return writeUint32(w, uint32(int32(v)))
	default:
		if err := w.WriteByte(0xd3); err != nil {
			return err
		}
		return writeUint64(w, uint64(v))
	}
}

func encodeUint(w *bufio.Writer, v uint64) error {
	switch {
	case v <= 0x7f:
		return w.WriteByte(byte(v)) // positive fixint
	case v <= math.MaxUint8:
		if err := w.WriteByte(0xcc); err != nil {
			return err
		}
		return w.WriteByte(byte(v))
	case v <= math.MaxUint16:
		if err := w.WriteByte(0xcd); err != nil {
			return err
		}
		return writeUint16(w, uint16(v))
	case v <= math.MaxUint32:
		if err := w.WriteByte(0xce); err != nil {
			return err
		}
		return writeUint32(w, uint32(v))
	default:
		if err := w.WriteByte(0xcf); err != nil {
			return err
		}
		return writeUint64(w, v)
	}
}

func encodeStr(w *bufio.Writer, s string) error {
	n := len(s)
	switch {
	case n <= 31:
		if err := w.WriteByte(0xa0 | byte(n)); err != nil {
			return err
		}
	case n <= math.MaxUint8:
		if err := w.WriteByte(0xd9); err != nil {
			return err
		}
		if err := w.WriteByte(byte(n)); err != nil {
			return err
		}
	case n <= math.MaxUint16:
		if err := w.WriteByte(0xda); err != nil {
			return err
		}
		if err := writeUint16(w, uint16(n)); err != nil {
			return err
		}
	default:
		if err := w.WriteByte(0xdb); err != nil {
			return err
		}
		if err := writeUint32(w, uint32(n)); err != nil {
			return err
		}
	}
	_, err := w.WriteString(s)
	return err
}

func encodeBin(w *bufio.Writer, b []byte) error {
	n := len(b)
	switch {
	case n <= math.MaxUint8:
		if err := w.WriteByte(0xc4); err != nil {
			return err
		}
		if err := w.WriteByte(byte(n)); err != nil {
			return err
		}
	case n <= math.MaxUint16:
		if err := w.WriteByte(0xc5); err != nil {
			return err
		}
		if err := writeUint16(w, uint16(n)); err != nil {
			return err
		}
	default:
		if err := w.WriteByte(0xc6); err != nil {
			return err
		}
		if err := writeUint32(w, uint32(n)); err != nil {
			return err
		}
	}
	_, err := w.Write(b)
	return err
}

func encodeArrayHeader(w *bufio.Writer, n int) error {
	switch {
	case n <= 15:
		return w.WriteByte(0x90 | byte(n))
	case n <= math.MaxUint16:
		if err := w.WriteByte(0xdc); err != nil {
			return err
		}
		return writeUint16(w, uint16(n))
	default:
		if err := w.WriteByte(0xdd); err != nil {
			return err
		}
		return writeUint32(w, uint32(n))
	}
}

func encodeMapHeader(w *bufio.Writer, n int) error {
	switch {
	case n <= 15:
		return w.WriteByte(0x80 | byte(n))
	case n <= math.MaxUint16:
		if err := w.WriteByte(0xde); err != nil {
			return err
		}
		return writeUint16(w, uint16(n))
	default:
		if err := w.WriteByte(0xdf); err != nil {
			return err
		}
		return writeUint32(w, uint32(n))
	}
}

func encodeExt(w *bufio.Writer, e Ext) error {
	n := len(e.Data)
	switch n {
	case 1, 2, 4, 8, 16:
		var b byte
		switch n {
		case 1:
			b = 0xd4
		case 2:
			b = 0xd5
		case 4:
			b = 0xd6
		case 8:
			b = 0xd7
		default:
			b = 0xd8
		}
		if err := w.WriteByte(b); err != nil {
			return err
		}
	default:
		switch {
		case n <= math.MaxUint8:
			if err := w.WriteByte(0xc7); err != nil {
				return err
			}
			if err := w.WriteByte(byte(n)); err != nil {
				return err
			}
		case n <= math.MaxUint16:
			if err := w.WriteByte(0xc8); err != nil {
				return err
			}
			if err := writeUint16(w, uint16(n)); err != nil {
				return err
			}
		default:
			if err := w.WriteByte(0xc9); err != nil {
				return err
			}
			if err := writeUint32(w, uint32(n)); err != nil {
				return err
			}
		}
	}
	if err := w.WriteByte(byte(e.Type)); err != nil {
		return err
	}
	_, err := w.Write(e.Data)
	return err
}

func writeUint16(w *bufio.Writer, v uint16) error {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	_, err := w.Write(b[:])
	return err
}

func writeUint32(w *bufio.Writer, v uint32) error {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	_, err := w.Write(b[:])
	return err
}

func writeUint64(w *bufio.Writer, v uint64) error {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	_, err := w.Write(b[:])
	return err
}
