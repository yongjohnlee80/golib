package msgpackrpc

import (
	"bufio"
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/msgpack"
	"github.com/yongjohnlee80/golib/server/rpc"
)

func encodeRaw(t *testing.T, v any) []byte {
	t.Helper()
	b, err := msgpack.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readOne(t *testing.T, c *Codec, raw []byte) (*rpc.Message, error) {
	t.Helper()
	return c.Read(bufio.NewReader(bytes.NewReader(raw)))
}

func TestCodec_ReadValidMessages(t *testing.T) {
	c := New(nil)
	cases := []struct {
		name string
		wire any
		want rpc.Message
	}{
		{
			"request",
			[]any{int64(0), int64(9), "db.query", []any{"select 1"}},
			rpc.Message{Kind: rpc.KindRequest, ID: 9, Method: "db.query", Params: []any{"select 1"}},
		},
		{
			"request empty params",
			[]any{int64(0), int64(0), "ping", []any{}},
			rpc.Message{Kind: rpc.KindRequest, ID: 0, Method: "ping", Params: []any{}},
		},
		{
			"response result",
			[]any{int64(1), int64(9), nil, "ok"},
			rpc.Message{Kind: rpc.KindResponse, ID: 9, Result: "ok"},
		},
		{
			"response error",
			[]any{int64(1), int64(9), map[string]any{"code": int64(-1)}, nil},
			rpc.Message{Kind: rpc.KindResponse, ID: 9, Err: map[string]any{"code": int64(-1)}},
		},
		{
			"notification",
			[]any{int64(2), "log", []any{"line"}},
			rpc.Message{Kind: rpc.KindNotification, Method: "log", Params: []any{"line"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readOne(t, c, encodeRaw(t, tc.wire))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*got, tc.want) {
				t.Fatalf("got %#v, want %#v", *got, tc.want)
			}
		})
	}
}

func TestCodec_ReadProtocolViolations(t *testing.T) {
	c := New(nil)
	cases := []struct {
		name string
		wire any
	}{
		{"top-level scalar", "hi"},
		{"top-level map", map[string]any{"x": int64(1)}},
		{"two elements", []any{int64(0), int64(1)}},
		{"five elements", []any{int64(0), int64(1), "m", []any{}, "extra"}},
		{"unknown tag", []any{int64(3), int64(1), "m", []any{}}},
		{"tag not int", []any{"0", int64(1), "m", []any{}}},
		{"request three elements", []any{int64(0), int64(1), "m"}},
		{"notification four elements", []any{int64(2), "m", []any{}, "extra"}},
		{"msgid negative", []any{int64(0), int64(-1), "m", []any{}}},
		{"msgid too big", []any{int64(0), int64(1) << 33, "m", []any{}}},
		{"msgid uint64 overflow", []any{int64(0), uint64(1) << 63, "m", []any{}}},
		{"msgid not int", []any{int64(0), "1", "m", []any{}}},
		{"method not string", []any{int64(0), int64(1), int64(7), []any{}}},
		{"params not array", []any{int64(0), int64(1), "m", map[string]any{}}},
		{"params nil", []any{int64(0), int64(1), "m", nil}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := readOne(t, c, encodeRaw(t, tc.wire)); !errors.Is(err, ErrProtocol) {
				t.Fatalf("err = %v, want ErrProtocol", err)
			}
		})
	}
}

func TestCodec_ReadPassesDecodeErrorsThrough(t *testing.T) {
	c := New(nil)
	if _, err := readOne(t, c, []byte{0xc1}); !errors.Is(err, msgpack.ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
	if _, err := readOne(t, c, []byte{}); !errors.Is(err, msgpack.ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}

func TestCodec_WriteReadRoundTrip(t *testing.T) {
	c := New(nil)
	msgs := []*rpc.Message{
		{Kind: rpc.KindRequest, ID: 3, Method: "m", Params: []any{int64(1)}},
		{Kind: rpc.KindRequest, ID: 4, Method: "m"}, // nil params → []
		{Kind: rpc.KindResponse, ID: 3, Result: map[string]any{"rows": []any{}}},
		{Kind: rpc.KindResponse, ID: 5, Err: map[string]any{"code": int64(-32601), "message": "nope"}},
		{Kind: rpc.KindNotification, Method: "evt", Params: []any{"x"}},
	}
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	for _, m := range msgs {
		if err := c.Write(w, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(&buf)
	for i, want := range msgs {
		got, err := c.Read(r)
		if err != nil {
			t.Fatalf("msg %d: %v", i, err)
		}
		norm := *want
		if norm.Params == nil && norm.Kind != rpc.KindResponse {
			norm.Params = []any{}
		}
		if !reflect.DeepEqual(*got, norm) {
			t.Fatalf("msg %d: got %#v, want %#v", i, *got, norm)
		}
	}
}

func TestCodec_WriteUnknownKind(t *testing.T) {
	c := New(nil)
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := c.Write(w, &rpc.Message{Kind: rpc.Kind(9)}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("err = %v, want ErrProtocol", err)
	}
}

func TestCodec_LimitsFlowThrough(t *testing.T) {
	c := New(&msgpack.Limits{MaxDepth: 4, MaxStrBytes: 8, MaxBinBytes: 8, MaxElements: 8})
	raw := encodeRaw(t, []any{int64(0), int64(1), "m", []any{"a-string-longer-than-eight"}})
	if _, err := readOne(t, c, raw); !errors.Is(err, msgpack.ErrLimitExceeded) {
		t.Fatalf("err = %v, want ErrLimitExceeded", err)
	}
}
