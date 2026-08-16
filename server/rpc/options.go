package rpc

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"github.com/yongjohnlee80/golib/logger"
)

// Gate is consulted before dispatching every request and notification on a
// connection. A non-nil error blocks the handler: requests are answered with
// the error (structured via *Error, CodeAccessDenied otherwise);
// notifications are dropped and logged. Consumers implement
// handshake-before-methods by admitting the session inside the handshake
// handler (Session.SetValue) and checking it here.
type Gate func(s *Session, method string) error

type config struct {
	addr            string
	listener        net.Listener
	tlsConfig       *tls.Config
	logger          logger.Logger
	baseCtx         context.Context
	drainTimeout    time.Duration
	maxMessageBytes int64
	maxConcurrent   int
	gate            Gate
}

// Option configures a Server.
type Option func(*config)

// Addr sets the TCP listen address (default ":0").
func Addr(addr string) Option {
	return func(c *config) { c.addr = addr }
}

// WithListener injects a pre-bound listener (tests, socket activation); it
// overrides Addr.
func WithListener(ln net.Listener) Option {
	return func(c *config) { c.listener = ln }
}

// WithTLSConfig wraps the listener in TLS.
func WithTLSConfig(cfg *tls.Config) Option {
	return func(c *config) { c.tlsConfig = cfg }
}

// WithLogger sets the transport logger (default Nop).
func WithLogger(l logger.Logger) Option {
	return func(c *config) { c.logger = l }
}

// BaseContext sets the parent of every per-connection (and so per-request)
// context.
func BaseContext(ctx context.Context) Option {
	return func(c *config) { c.baseCtx = ctx }
}

// DrainTimeout bounds the graceful drain when Run's context is cancelled
// (default 10s). Must be positive; New panics otherwise.
func DrainTimeout(d time.Duration) Option {
	return func(c *config) { c.drainTimeout = d }
}

// MaxMessageBytes bounds a single message in BOTH directions (default
// 16 MiB): an inbound overrun closes the connection; an outbound overrun is
// replaced by a generic internal-error reply. Must be positive; New panics
// otherwise.
func MaxMessageBytes(n int64) Option {
	return func(c *config) { c.maxMessageBytes = n }
}

// MaxConcurrent bounds concurrently executing handlers per connection
// (default 8). The read loop acquires a slot BEFORE decoding the next
// message, so decoded-message retention is bounded too — while saturated
// the connection is not read at all (backpressure, not goroutine growth —
// R4). Must be positive; New panics otherwise.
func MaxConcurrent(n int) Option {
	return func(c *config) { c.maxConcurrent = n }
}

// WithGate installs the pre-dispatch gate.
func WithGate(g Gate) Option {
	return func(c *config) { c.gate = g }
}
