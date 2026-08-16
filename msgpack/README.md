# msgpack

Zero-dependency MessagePack value codec (golib-server ADR-0008 §2.2): the
full wire family over a fixed Go vocabulary, with decode limits designed for
attacker-adjacent input. Stdlib only.

```go
import "github.com/yongjohnlee80/golib/msgpack"
```

## Value vocabulary

| Go type | encodes to | decodes to |
|---|---|---|
| `nil` | nil | `nil` |
| `bool` | bool | `bool` |
| `int`, `int8..64`, `uint`, `uint8..32` | most compact int form | `int64` |
| `uint64` | most compact uint form | `int64`, or `uint64` when > MaxInt64 |
| `float32` / `float64` | float32 / float64 | `float64` |
| `string` | fixstr/str8/16/32 | `string` |
| `[]byte` | bin8/16/32 | `[]byte` |
| `[]any` | fixarray/array16/32 | `[]any` |
| `map[string]any` | fixmap/map16/32 | `map[string]any` |
| `Ext{Type, Data}` | fixext1..16/ext8/16/32 | `Ext` |

Anything else fails `Encode` with `ErrUnsupportedType`. Deliberate v1
restrictions, loud rather than silent: map keys must be strings both
directions (`ErrNonStringKey` on decode), and the timestamp extension
(type −1) is not interpreted — it passes through as `Ext`.

## Usage

```go
b, err := msgpack.Marshal(map[string]any{"rows": []any{int64(1)}})
v, err := msgpack.Unmarshal(b, nil) // nil → DefaultLimits

// Streaming (back-to-back messages on one reader):
v, err := msgpack.Decode(bufioReader, msgpack.DefaultLimits())
err = msgpack.Encode(bufioWriter, v) // caller flushes
```

`Unmarshal` requires full consumption — trailing bytes are `ErrMalformed`.
`Decode` consumes exactly one value and leaves the rest, which is what a
message transport wants.

## Decoding untrusted input

Every decode is bounded by `Limits` (KB convention security-core-hardening
R4), on two axes:

- **Per item:** `MaxDepth` (64), `MaxStrBytes`/`MaxBinBytes` (8 MiB),
  `MaxElements` per collection (1 M).
- **Per decode (aggregate):** `MaxTotalElements` (1 M decoded values) and
  `MaxTotalBytes` (16 MiB payload bytes) across the WHOLE value. Per-item
  limits alone don't stop a message packed with many maximal siblings —
  sixteen 1M-element nil arrays fit ~16 MiB of wire but decode to ~256 MiB.
  The budgets make decoded footprint linear and tunable; as an estimate
  (not a byte ceiling), it's `MaxTotalElements×(per-value overhead) +
  MaxTotalBytes` — scalars cost one 16-byte interface word each, while
  container-heavy shapes (e.g. arrays of empty maps) cost several times
  that per element. Size the budgets for the shapes you expect.

Zero (or negative) `Limits` fields fall back to the default values, so a
partially-filled struct tightens or loosens individual bounds without ever
silently disabling one. Declared sizes are validated before allocation and
preallocation is capped, so a forged `array32(0xffffffff)` header cannot
allocate memory the input never supplies. Truncation, the reserved `0xc1`
byte, depth bombs, and oversize declarations return typed sentinels
(`ErrMalformed`, `ErrDepthExceeded`, `ErrLimitExceeded`); no input panics
the decoder — enforced by `FuzzDecode`.

A clean end-of-stream before any byte of a value is `io.EOF` (a polite peer
hang-up); truncation inside a value is `ErrMalformed` — transports log the
two differently. `Encode` bounds its own recursion, so a cyclic container
value fails with `ErrDepthExceeded` instead of exhausting the stack.

## Neovim interop

Neovim's API handle types (Buffer=0, Window=1, Tabpage=2) are EXT values;
they round-trip as `msgpack.Ext` untouched. The msgpack-RPC framing that
carries them lives in [`server/rpc/msgpackrpc`](../server/rpc/msgpackrpc/).
