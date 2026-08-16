// Package msgpackrpc implements the msgpack-RPC wire format as an rpc.Codec
// (server ADR-0008 §2.5) — the framing Neovim's sockconnect(..., {rpc=true})
// speaks: request [0, msgid, method, params], response [1, msgid, error,
// result], notification [2, method, params]. Values ride the zero-dep
// golib msgpack codec; Neovim's EXT handle types (Buffer=0, Window=1,
// Tabpage=2) pass through as msgpack.Ext untouched.
package msgpackrpc

import (
	"bufio"
	"errors"
	"fmt"
	"math"

	"github.com/yongjohnlee80/golib/msgpack"
	"github.com/yongjohnlee80/golib/server/rpc"
)

// ErrProtocol reports a well-decoded value that is not a valid msgpack-RPC
// message. The transport closes the connection on it (R7).
var ErrProtocol = errors.New("msgpackrpc: protocol violation")

// Codec is the msgpack-RPC wire codec. The zero value is not usable; build
// with New.
type Codec struct {
	lim *msgpack.Limits
}

var _ rpc.Codec = (*Codec)(nil)

// New builds a Codec decoding values under lim (nil → msgpack.DefaultLimits).
func New(lim *msgpack.Limits) *Codec {
	if lim == nil {
		lim = msgpack.DefaultLimits()
	}
	return &Codec{lim: lim}
}

// Read decodes exactly one message, validating shape strictly: top-level is
// a 3/4-element array, type tag 0/1/2 matching the arity, msgid within
// uint32, method a string, params an array. Anything else is ErrProtocol.
func (c *Codec) Read(r *bufio.Reader) (*rpc.Message, error) {
	v, err := msgpack.Decode(r, c.lim)
	if err != nil {
		return nil, err
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: top-level %T, want array", ErrProtocol, v)
	}
	if len(arr) != 3 && len(arr) != 4 {
		return nil, fmt.Errorf("%w: array of %d elements", ErrProtocol, len(arr))
	}
	tag, ok := arr[0].(int64)
	if !ok {
		return nil, fmt.Errorf("%w: type tag %T, want int", ErrProtocol, arr[0])
	}

	switch tag {
	case 0: // request [0, msgid, method, params]
		if len(arr) != 4 {
			return nil, fmt.Errorf("%w: request with %d elements", ErrProtocol, len(arr))
		}
		id, err := msgid(arr[1])
		if err != nil {
			return nil, err
		}
		method, params, err := methodParams(arr[2], arr[3])
		if err != nil {
			return nil, err
		}
		return &rpc.Message{Kind: rpc.KindRequest, ID: id, Method: method, Params: params}, nil

	case 1: // response [1, msgid, error, result]
		if len(arr) != 4 {
			return nil, fmt.Errorf("%w: response with %d elements", ErrProtocol, len(arr))
		}
		id, err := msgid(arr[1])
		if err != nil {
			return nil, err
		}
		return &rpc.Message{Kind: rpc.KindResponse, ID: id, Err: arr[2], Result: arr[3]}, nil

	case 2: // notification [2, method, params]
		if len(arr) != 3 {
			return nil, fmt.Errorf("%w: notification with %d elements", ErrProtocol, len(arr))
		}
		method, params, err := methodParams(arr[1], arr[2])
		if err != nil {
			return nil, err
		}
		return &rpc.Message{Kind: rpc.KindNotification, Method: method, Params: params}, nil
	}
	return nil, fmt.Errorf("%w: type tag %d", ErrProtocol, tag)
}

// Write encodes one message; the transport flushes.
func (c *Codec) Write(w *bufio.Writer, m *rpc.Message) error {
	var v []any
	switch m.Kind {
	case rpc.KindRequest:
		v = []any{int64(0), int64(m.ID), m.Method, paramsArray(m.Params)}
	case rpc.KindResponse:
		v = []any{int64(1), int64(m.ID), m.Err, m.Result}
	case rpc.KindNotification:
		v = []any{int64(2), m.Method, paramsArray(m.Params)}
	default:
		return fmt.Errorf("%w: unknown message kind %d", ErrProtocol, m.Kind)
	}
	return msgpack.Encode(w, v)
}

func msgid(v any) (uint32, error) {
	switch id := v.(type) {
	case int64:
		if id < 0 || id > math.MaxUint32 {
			return 0, fmt.Errorf("%w: msgid %d out of uint32 range", ErrProtocol, id)
		}
		return uint32(id), nil
	case uint64:
		return 0, fmt.Errorf("%w: msgid %d out of uint32 range", ErrProtocol, id)
	}
	return 0, fmt.Errorf("%w: msgid %T, want int", ErrProtocol, v)
}

func methodParams(mv, pv any) (string, []any, error) {
	method, ok := mv.(string)
	if !ok {
		return "", nil, fmt.Errorf("%w: method %T, want string", ErrProtocol, mv)
	}
	params, ok := pv.([]any)
	if !ok {
		return "", nil, fmt.Errorf("%w: params %T, want array", ErrProtocol, pv)
	}
	return method, params, nil
}

// paramsArray keeps the wire spec-strict: params is always an array, so nil
// encodes as [] rather than msgpack nil.
func paramsArray(p []any) []any {
	if p == nil {
		return []any{}
	}
	return p
}
