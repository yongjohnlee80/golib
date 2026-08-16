package msgpack

import (
	"errors"
	"fmt"
)

// errAggregate reports an exhausted whole-decode budget.
func errAggregate[T int | int64](kind string, max T) error {
	return fmt.Errorf("%w: aggregate %s budget (%d) exhausted", ErrLimitExceeded, kind, max)
}

// Typed codec failures; compare with errors.Is.
var (
	// ErrMalformed reports bytes that are not valid MessagePack (truncated
	// input, the reserved 0xc1 byte, or a structurally invalid sequence).
	// A clean end-of-stream BEFORE any byte of a value is io.EOF, not this.
	ErrMalformed = errors.New("msgpack: malformed input")

	// ErrDepthExceeded reports nesting beyond Limits.MaxDepth on decode, or
	// beyond the fixed encoder depth bound on encode (which is how a cyclic
	// container value surfaces instead of exhausting the stack).
	ErrDepthExceeded = errors.New("msgpack: nesting depth exceeded")

	// ErrLimitExceeded reports a declared size beyond the configured Limits —
	// per-item (string/binary/collection) or aggregate (whole-decode element
	// and byte budgets).
	ErrLimitExceeded = errors.New("msgpack: size limit exceeded")

	// ErrNonStringKey reports a decoded map whose key is not a string —
	// a documented v1 restriction, never a silent coercion.
	ErrNonStringKey = errors.New("msgpack: map key is not a string")

	// ErrUnsupportedType reports an Encode value outside the package's Go
	// vocabulary.
	ErrUnsupportedType = errors.New("msgpack: unsupported Go type")
)

// Limits bounds decoding of untrusted input (R4). Fields at zero (or
// negative) fall back to the DefaultLimits value, so the zero value decodes
// with full default protection — a partially-filled Limits can only tighten
// or explicitly loosen individual bounds, never silently disable one.
type Limits struct {
	// MaxDepth bounds container nesting.
	MaxDepth int
	// MaxStrBytes bounds a single string's declared byte length.
	MaxStrBytes int
	// MaxBinBytes bounds a single binary/ext payload's declared byte length.
	MaxBinBytes int
	// MaxElements bounds a single array's element count and a single map's
	// pair count.
	MaxElements int

	// MaxTotalElements bounds the aggregate number of decoded values (every
	// scalar, container, and container element) across one whole Decode.
	// Per-container limits alone don't bound a message packed with many
	// maximal siblings; this does. Each decoded value retains at least one
	// interface word (16 bytes on 64-bit), so the worst-case decoded
	// footprint is roughly MaxTotalElements×16 + MaxTotalBytes.
	MaxTotalElements int
	// MaxTotalBytes bounds the aggregate string/binary/ext payload bytes
	// across one whole Decode.
	MaxTotalBytes int64
}

// DefaultLimits returns the standard decode bounds (ADR-0008 §2.2):
// depth 64, 8 MiB per string/binary, 1 M elements per collection,
// 1 M total decoded values and 16 MiB total payload bytes per decode
// (≈32 MiB worst-case decoded footprint on 64-bit).
func DefaultLimits() *Limits {
	return &Limits{
		MaxDepth:         64,
		MaxStrBytes:      8 << 20,
		MaxBinBytes:      8 << 20,
		MaxElements:      1 << 20,
		MaxTotalElements: 1 << 20,
		MaxTotalBytes:    16 << 20,
	}
}

// normalized returns a copy with zero/negative fields replaced by defaults.
// Decode calls this once per top-level decode; callers never see the copy.
func (l *Limits) normalized() Limits {
	d := DefaultLimits()
	out := *l
	if out.MaxDepth <= 0 {
		out.MaxDepth = d.MaxDepth
	}
	if out.MaxStrBytes <= 0 {
		out.MaxStrBytes = d.MaxStrBytes
	}
	if out.MaxBinBytes <= 0 {
		out.MaxBinBytes = d.MaxBinBytes
	}
	if out.MaxElements <= 0 {
		out.MaxElements = d.MaxElements
	}
	if out.MaxTotalElements <= 0 {
		out.MaxTotalElements = d.MaxTotalElements
	}
	if out.MaxTotalBytes <= 0 {
		out.MaxTotalBytes = d.MaxTotalBytes
	}
	return out
}

// decodeState carries the normalized limits and the whole-decode aggregate
// budgets through the recursive decode.
type decodeState struct {
	lim       Limits
	nodesLeft int   // decremented per decoded value
	bytesLeft int64 // decremented per payload byte (str/bin/ext)
}

// chargeNode spends one aggregate value slot.
func (st *decodeState) chargeNode() error {
	st.nodesLeft--
	if st.nodesLeft < 0 {
		return errAggregate("decoded values", st.lim.MaxTotalElements)
	}
	return nil
}

// chargeBytes spends n aggregate payload bytes.
func (st *decodeState) chargeBytes(n int) error {
	st.bytesLeft -= int64(n)
	if st.bytesLeft < 0 {
		return errAggregate("payload bytes", st.lim.MaxTotalBytes)
	}
	return nil
}

// prealloc caps eager allocation for a declared collection size: grow by
// append beyond this, so a forged header cannot allocate what the input
// never supplies.
const prealloc = 4096

// maxEncodeDepth bounds Encode's recursion. Outbound values are
// server-authored, so this is a bug guard, not an attacker bound: a cyclic
// []any/map[string]any fails with ErrDepthExceeded instead of exhausting
// the stack and killing the process.
const maxEncodeDepth = 256
