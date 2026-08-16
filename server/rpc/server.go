package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/server"
)

// Server is a connection-oriented RPC server over a pluggable Codec, built
// on server.Scaffold (ADR-0008). Register handlers with Handle before Run.
type Server struct {
	codec    Codec
	cfg      config
	scaffold *server.Scaffold

	mu       sync.RWMutex
	handlers map[string]Handler
}

// compile-time: Server satisfies the lifecycle contract.
var _ server.Server = (*Server)(nil)

// Request is what a Handler receives.
type Request struct {
	Method  string
	Params  []any
	Peer    net.Addr
	Session *Session
}

// Handler serves one request (or notification — Result is discarded there).
//
// ctx is cancelled when the connection ends for ANY reason — peer
// disconnect, drain, or Shutdown — so honor it. Return *Error for a
// structured error the peer is meant to see; ANY other error is logged
// server-side and reaches the peer only as a generic CodeInternal
// "internal error" (deny-before-disclose: raw error text can carry paths,
// hostnames, or credentials).
type Handler func(ctx context.Context, req *Request) (any, error)

// New builds a Server speaking codec. Construction bugs — nil codec,
// non-positive MaxConcurrent or MaxMessageBytes, non-positive DrainTimeout —
// panic here rather than misbehaving per connection at runtime.
func New(codec Codec, opts ...Option) *Server {
	if codec == nil {
		panic("rpc.New: nil Codec")
	}
	cfg := config{
		addr:            ":0",
		logger:          logger.Nop{},
		drainTimeout:    10 * time.Second,
		maxMessageBytes: 16 << 20,
		maxConcurrent:   8,
	}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if cfg.maxConcurrent <= 0 {
		panic(fmt.Sprintf("rpc.New: MaxConcurrent must be positive, got %d", cfg.maxConcurrent))
	}
	if cfg.maxMessageBytes <= 0 {
		panic(fmt.Sprintf("rpc.New: MaxMessageBytes must be positive, got %d", cfg.maxMessageBytes))
	}
	if cfg.drainTimeout <= 0 {
		panic(fmt.Sprintf("rpc.New: DrainTimeout must be positive, got %v", cfg.drainTimeout))
	}
	if cfg.logger == nil {
		cfg.logger = logger.Nop{}
	}
	if cfg.baseCtx == nil {
		cfg.baseCtx = context.Background()
	}
	s := &Server{codec: codec, cfg: cfg, handlers: make(map[string]Handler)}
	s.scaffold = server.NewScaffold(s.serveConn,
		server.ScaffoldAddr(cfg.addr),
		server.WithListener(cfg.listener),
		server.WithTLSConfig(cfg.tlsConfig),
		server.ScaffoldLogger(cfg.logger),
		server.ScaffoldBaseContext(cfg.baseCtx),
		server.DrainTimeout(cfg.drainTimeout),
		server.ScaffoldSessionFactory(func(_ context.Context, nc net.Conn) server.Session {
			return newConn(nc)
		}),
	)
	return s
}

// Handle registers h for method, replacing any prior registration.
// Registration after Run is safe but races only with its own method's
// dispatch (the map is lock-guarded); register before Run for determinism.
func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

func (s *Server) lookup(method string) Handler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.handlers[method]
}

// Run serves until ctx is cancelled, then drains gracefully.
func (s *Server) Run(ctx context.Context) error { return s.scaffold.Run(ctx) }

// Shutdown stops accepting, cancels in-flight handler contexts, and drains
// politely bounded by ctx.
func (s *Server) Shutdown(ctx context.Context) error { return s.scaffold.Shutdown(ctx) }

// Addr returns the resolved listen address.
func (s *Server) Addr() string { return s.scaffold.Addr() }

// serveConn is the scaffold ConnHandler: one read loop per connection. It
// owns the connection lifecycle signals: when the read loop exits for any
// reason it cancels every in-flight handler context, waits for admitted
// work to finish flushing replies, then reports completion (workDone) and
// lets the deferred Close run.
func (s *Server) serveConn(ctx context.Context, nc net.Conn) {
	c, ok := server.SessionFromContext(ctx).(*conn)
	if !ok {
		// Registered-while-draining: the registry already closed the session.
		return
	}
	defer nc.Close()
	connCtx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()          // peer gone or draining: unblock ctx-honoring handlers
		c.inflight.Wait() // let them flush their final replies
		close(c.workDone) // Drain's completion signal
	}()
	s.readLoop(connCtx, c)
}

func (s *Server) readLoop(ctx context.Context, c *conn) {
	sem := make(chan struct{}, s.cfg.maxConcurrent)
	release := func() { <-sem }
	for {
		// Acquire the execution slot BEFORE decoding: no message is decoded
		// without a bounded home, so per-connection decoded-value retention
		// is capped at MaxConcurrent messages. The trade-off is documented:
		// while saturated, the loop does not read, so a peer disconnect is
		// observed only when a slot frees (bounded by handler completion,
		// which Shutdown bounds via context cancellation).
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		c.lim.reset(s.cfg.maxMessageBytes)
		m, err := s.codec.Read(c.br)
		if err != nil {
			release()
			s.logReadEnd(c, err)
			return
		}
		switch m.Kind {
		case KindRequest, KindNotification:
			if s.cfg.gate != nil {
				if gerr := s.cfg.gate(c.sess, m.Method); gerr != nil {
					s.rejectGated(c, m, gerr)
					release()
					continue
				}
			}
			if !c.admit() {
				// Drain won the race after this message decoded: stop
				// reading; the message is dropped, not half-served.
				release()
				return
			}
			go func(m *Message) {
				defer func() { c.inflight.Done(); release() }()
				s.dispatchTask(ctx, c, m)
			}(m)
		case KindResponse:
			// v1 never issues requests, so a peer response has no home.
			// Well-formed but unexpected: drop with a log line.
			s.cfg.logger.Log(logger.SeverityWarning, map[string]any{
				"server": "rpc", "event": "unexpected response dropped",
				"remote": c.sess.Peer().String(), "msgid": m.ID,
			})
			release()
		}
	}
}

// rejectGated answers a gate rejection without dispatching. A *Error from
// the gate is the public message; any other error is logged and answered
// with a stable generic denial (deny-before-disclose). Notifications have
// no reply channel; they drop with a log line.
func (s *Server) rejectGated(c *conn, m *Message, gerr error) {
	if m.Kind == KindNotification {
		s.cfg.logger.Log(logger.SeverityWarning, map[string]any{
			"server": "rpc", "event": "notification gated",
			"remote": c.sess.Peer().String(), "method": m.Method,
		})
		return
	}
	var e *Error
	if !errors.As(gerr, &e) {
		s.cfg.logger.Log(logger.SeverityWarning, map[string]any{
			"server": "rpc", "event": "gate rejection (detail withheld from wire)",
			"remote": c.sess.Peer().String(), "method": m.Method, "err": gerr.Error(),
		})
		gerr = &Error{Code: CodeAccessDenied, Message: "access denied"}
	}
	s.reply(c, &Message{Kind: KindResponse, ID: m.ID, Err: wireError(gerr)})
}

// dispatchTask is the panic boundary for one request's ENTIRE lifecycle —
// handler, response construction, encoding, and write. dispatch already
// isolates handler panics into an internal-error reply; a panic past that
// point (reply path) leaves the write stream in an unknown state, so the
// connection is closed.
func (s *Server) dispatchTask(ctx context.Context, c *conn, m *Message) {
	defer func() {
		if rec := recover(); rec != nil {
			s.cfg.logger.Log(logger.SeverityError, map[string]any{
				"server": "rpc", "event": "request task panic",
				"remote": c.sess.Peer().String(), "method": m.Method, "recover": rec,
			})
			_ = c.raw.Close()
		}
	}()
	s.dispatch(ctx, c, m)
}

// dispatch runs the handler for one request/notification, panic-isolated,
// and writes the reply for requests.
func (s *Server) dispatch(ctx context.Context, c *conn, m *Message) {
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var result any
	var err error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				s.cfg.logger.Log(logger.SeverityError, map[string]any{
					"server": "rpc", "event": "handler panic",
					"remote": c.sess.Peer().String(), "method": m.Method, "recover": rec,
				})
				// Generic wire message: panic detail is server-internal.
				err = &Error{Code: CodeInternal, Message: "internal error"}
			}
		}()
		h := s.lookup(m.Method)
		if h == nil {
			err = &Error{Code: CodeMethodNotFound, Message: "unknown method: " + m.Method}
			return
		}
		result, err = h(rctx, &Request{
			Method: m.Method, Params: m.Params,
			Peer: c.sess.Peer(), Session: c.sess,
		})
	}()

	// Untyped errors are logged with full detail but cross the wire only as
	// a generic internal error (deny-before-disclose).
	if err != nil {
		var e *Error
		if !errors.As(err, &e) {
			s.cfg.logger.Log(logger.SeverityWarning, map[string]any{
				"server": "rpc", "event": "handler error (detail withheld from wire)",
				"remote": c.sess.Peer().String(), "method": m.Method, "err": err.Error(),
			})
		}
	}

	if m.Kind == KindNotification {
		if err != nil {
			s.cfg.logger.Log(logger.SeverityWarning, map[string]any{
				"server": "rpc", "event": "notification handler error",
				"remote": c.sess.Peer().String(), "method": m.Method, "err": err.Error(),
			})
		}
		return
	}
	resp := &Message{Kind: KindResponse, ID: m.ID}
	if err != nil {
		resp.Err = wireError(err)
	} else {
		resp.Result = result
	}
	s.reply(c, resp)
}

// reply writes one response frame. Encoding failures happen entirely in the
// staging buffer (stream untouched), so the peer still gets an answer: the
// unencodable result is logged and replaced with a generic internal error.
// A socket failure — or a failure to even send the substitute — is terminal
// for the connection.
func (s *Server) reply(c *conn, m *Message) {
	err := c.write(s.codec, m, s.cfg.maxMessageBytes)
	if err == nil {
		return
	}
	if errors.Is(err, errEncode) {
		s.cfg.logger.Log(logger.SeverityError, map[string]any{
			"server": "rpc", "event": "unencodable response replaced",
			"remote": c.sess.Peer().String(), "msgid": m.ID, "err": err.Error(),
		})
		fallback := &Message{Kind: KindResponse, ID: m.ID,
			Err: map[string]any{"code": CodeInternal, "message": "internal error"}}
		if err = c.write(s.codec, fallback, s.cfg.maxMessageBytes); err == nil {
			return
		}
	}
	s.cfg.logger.Log(logger.SeverityWarning, map[string]any{
		"server": "rpc", "event": "reply write failed; closing connection",
		"remote": c.sess.Peer().String(), "msgid": m.ID, "err": err.Error(),
	})
	_ = c.raw.Close()
}

// logReadEnd classifies why the read loop stopped: clean close (io.EOF
// between messages), drain, and shutdown are info-level; anything else
// (malformed frame, size overrun, truncation) warns before the connection
// closes (R7: a peer that cannot frame correctly is done).
func (s *Server) logReadEnd(c *conn, err error) {
	payload := map[string]any{
		"server": "rpc", "event": "connection closed",
		"remote": c.sess.Peer().String(),
	}
	if errors.Is(err, io.EOF) || c.isDraining() || errors.Is(err, net.ErrClosed) || isDeadline(err) {
		s.cfg.logger.Log(logger.SeverityInfo, payload)
		return
	}
	payload["event"] = "connection dropped"
	payload["err"] = err.Error()
	s.cfg.logger.Log(logger.SeverityWarning, payload)
}

// isDeadline reports the read-deadline expiry Drain uses to unblock the
// read loop.
func isDeadline(err error) bool {
	var to interface{ Timeout() bool }
	if errors.As(err, &to) && to.Timeout() {
		return true
	}
	return errors.Is(err, os.ErrDeadlineExceeded)
}
