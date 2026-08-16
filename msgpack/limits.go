package msgpack

import "errors"

// Typed decode failures; compare with errors.Is.
var (
	// ErrMalformed reports bytes that are not valid MessagePack (truncated
	// input, the reserved 0xc1 byte, or a structurally invalid sequence).
	ErrMalformed = errors.New("msgpack: malformed input")

	// ErrDepthExceeded reports nesting beyond Limits.MaxDepth.
	ErrDepthExceeded = errors.New("msgpack: nesting depth exceeded")

	// ErrLimitExceeded reports a declared string/binary/collection size
	// beyond the configured Limits.
	ErrLimitExceeded = errors.New("msgpack: size limit exceeded")

	// ErrNonStringKey reports a decoded map whose key is not a string —
	// a documented v1 restriction, never a silent coercion.
	ErrNonStringKey = errors.New("msgpack: map key is not a string")

	// ErrUnsupportedType reports an Encode value outside the package's Go
	// vocabulary.
	ErrUnsupportedType = errors.New("msgpack: unsupported Go type")
)

// Limits bounds decoding of untrusted input (R4). The zero value is NOT
// usable; obtain defaults from DefaultLimits.
type Limits struct {
	// MaxDepth bounds container nesting.
	MaxDepth int
	// MaxStrBytes bounds a single string's declared byte length.
	MaxStrBytes int
	// MaxBinBytes bounds a single binary blob's declared byte length.
	MaxBinBytes int
	// MaxElements bounds a single array's element count and a single map's
	// pair count.
	MaxElements int
}

// DefaultLimits returns the standard decode bounds (ADR-0008 §2.2).
func DefaultLimits() *Limits {
	return &Limits{
		MaxDepth:    64,
		MaxStrBytes: 8 << 20, // 8 MiB
		MaxBinBytes: 8 << 20,
		MaxElements: 1 << 20,
	}
}

// prealloc caps eager allocation for a declared collection size: grow by
// append beyond this, so a forged header cannot allocate what the input
// never supplies.
const prealloc = 4096
