package rpc

import (
	"bufio"
	"errors"
	"fmt"
)

// Kind classifies a wire message.
type Kind uint8

const (
	// KindRequest expects a KindResponse echoing its ID.
	KindRequest Kind = iota
	// KindResponse answers exactly one KindRequest.
	KindResponse
	// KindNotification is fire-and-forget: no ID, no reply.
	KindNotification
)

// Message is one wire unit, codec-independent. Field relevance by Kind:
// Request uses ID/Method/Params; Response uses ID and exactly one of
// Err/Result; Notification uses Method/Params.
type Message struct {
	Kind   Kind
	ID     uint32
	Method string
	Params []any
	Err    any
	Result any
}

// Codec owns the wire format: one Message in, one Message out. Read must
// consume exactly one message and treat the byte stream as attacker-adjacent
// (bounded, panic-free — KB security-core-hardening R4/R7). Write buffers
// into w; the transport owns flushing.
//
// Concurrency contract: the Server shares ONE Codec instance across every
// live connection. The transport serializes reads and writes per connection
// (one reader goroutine; writes under a per-connection lock), but different
// connections call Read and Write concurrently — implementations must
// therefore be stateless or internally synchronized across calls. The
// shipped msgpackrpc codec is stateless.
type Codec interface {
	Read(r *bufio.Reader) (*Message, error)
	Write(w *bufio.Writer, m *Message) error
}

// Error carries a structured RPC error to the wire as {code, message} —
// the ONLY error form whose text reaches the peer. Handlers return *Error
// for messages meant to be public; any other error is logged server-side
// and crosses the wire as a generic CodeInternal "internal error"
// (deny-before-disclose: raw error text can carry paths, hostnames, query
// fragments, or credentials).
type Error struct {
	Code    int64
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// Wire error codes (JSON-RPC-aligned where a standard code exists).
const (
	// CodeMethodNotFound answers a request for an unregistered method.
	CodeMethodNotFound int64 = -32601
	// CodeInvalidParams is for handlers to return on malformed params.
	CodeInvalidParams int64 = -32602
	// CodeInternal answers handler panics and untyped handler errors.
	CodeInternal int64 = -32603
	// CodeAccessDenied answers a gate rejection (handshake/authz policy).
	CodeAccessDenied int64 = -32001
)

// ErrMessageTooLarge surfaces (wrapped in the codec's read error) when a
// single message exceeds the transport's MaxMessageBytes window.
var ErrMessageTooLarge = errors.New("rpc: message exceeds size limit")

// wireError converts an error to its wire form. Only *Error text is
// public; everything else becomes a generic internal error (the caller is
// responsible for logging the withheld detail).
func wireError(err error) map[string]any {
	var e *Error
	if errors.As(err, &e) {
		return map[string]any{"code": e.Code, "message": e.Message}
	}
	return map[string]any{"code": CodeInternal, "message": "internal error"}
}
