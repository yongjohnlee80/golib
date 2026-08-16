# server/rpc/msgpackrpc

The msgpack-RPC wire codec for [`server/rpc`](../README.md) (golib-server
ADR-0008 §2.5) — the framing Neovim's `sockconnect(..., {rpc = true})`
speaks natively. Values ride the zero-dep [`msgpack`](../../../msgpack/)
codec.

```go
srv := rpc.New(msgpackrpc.New(nil)) // nil → msgpack.DefaultLimits
```

## Wire format

Per the msgpack-RPC spec:

| kind | shape |
|---|---|
| request | `[0, msgid, method, params]` |
| response | `[1, msgid, error, result]` |
| notification | `[2, method, params]` |

`Read` validates shape strictly: top-level must be a 3/4-element array with
a type tag matching its arity, msgid within uint32, method a string, params
an array. Anything else is `ErrProtocol` and the transport closes the
connection. Decode-level failures (malformed bytes, limit violations) pass
through as the `msgpack` package's typed errors.

`Write` keeps the wire spec-strict in the other direction: nil params encode
as `[]`, never msgpack nil.

Neovim's EXT handle types (Buffer=0, Window=1, Tabpage=2) pass through as
`msgpack.Ext` untouched — interpretation belongs to the consumer.
