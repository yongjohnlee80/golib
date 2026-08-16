package rpc

import (
	"context"
	"errors"
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
// ctx is cancelled on Shutdown; honor it. Return *Error for a structured
// wire error; any other error reaches the peer as CodeInternal + Error()
// text.
type Handler func(ctx context.Context, req *Request) (any, error)

// New builds a Server speaking codec. Panics on a nil codec (construction
// bug, not a runtime condition).
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

// serveConn is the scaffold ConnHandler: one read loop per connection.
func (s *Server) serveConn(ctx context.Context, nc net.Conn) {
	c, ok := server.SessionFromContext(ctx).(*conn)
	if !ok {
		// Registered-while-draining: the registry already closed the session.
		return
	}
	defer nc.Close()
	s.readLoop(ctx, c)
	// Replies must stay flushable until every in-flight handler finishes:
	// returning runs the deferred Close, so wait here. Shutdown bounds this
	// via handler-context cancellation; a peer disconnect leaves it bounded
	// by handler discipline (as with net/http).
	c.inflight.Wait()
}

func (s *Server) readLoop(ctx context.Context, c *conn) {
	sem := make(chan struct{}, s.cfg.maxConcurrent)
	for {
		c.lim.reset(s.cfg.maxMessageBytes)
		m, err := s.codec.Read(c.br)
		if err != nil {
			s.logReadEnd(c, err)
			return
		}
		if c.draining.Load() {
			return
		}
		switch m.Kind {
		case KindRequest, KindNotification:
			if s.cfg.gate != nil {
				if gerr := s.cfg.gate(c.sess, m.Method); gerr != nil {
					s.rejectGated(c, m, gerr)
					continue
				}
			}
			sem <- struct{}{} // backpressure: block reads at MaxConcurrent
			c.inflight.Add(1)
			go func(m *Message) {
				defer func() { <-sem; c.inflight.Done() }()
				s.dispatch(ctx, c, m)
			}(m)
		case KindResponse:
			// v1 never issues requests, so a peer response has no home.
			// Well-formed but unexpected: drop with a log line.
			s.cfg.logger.Log(logger.SeverityWarning, map[string]any{
				"server": "rpc", "event": "unexpected response dropped",
				"remote": c.sess.Peer().String(), "msgid": m.ID,
			})
		}
	}
}

// rejectGated answers a gate rejection without dispatching. Notifications
// have no reply channel; they drop with a log line.
func (s *Server) rejectGated(c *conn, m *Message, gerr error) {
	if m.Kind == KindNotification {
		s.cfg.logger.Log(logger.SeverityWarning, map[string]any{
			"server": "rpc", "event": "notification gated",
			"remote": c.sess.Peer().String(), "method": m.Method,
		})
		return
	}
	werr := gerr
	var e *Error
	if !errors.As(gerr, &e) {
		werr = &Error{Code: CodeAccessDenied, Message: gerr.Error()}
	}
	s.reply(c, &Message{Kind: KindResponse, ID: m.ID, Err: wireError(werr)})
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

func (s *Server) reply(c *conn, m *Message) {
	if err := c.write(s.codec, m); err != nil {
		s.cfg.logger.Log(logger.SeverityWarning, map[string]any{
			"server": "rpc", "event": "reply write failed",
			"remote": c.sess.Peer().String(), "msgid": m.ID, "err": err.Error(),
		})
	}
}

// logReadEnd classifies why the read loop stopped: clean close and drain are
// info-level; anything else (malformed frame, size overrun) warns before the
// connection closes (R7: a peer that cannot frame correctly is done).
func (s *Server) logReadEnd(c *conn, err error) {
	payload := map[string]any{
		"server": "rpc", "event": "connection closed",
		"remote": c.sess.Peer().String(),
	}
	if c.draining.Load() || errors.Is(err, net.ErrClosed) || isEOF(err) {
		s.cfg.logger.Log(logger.SeverityInfo, payload)
		return
	}
	payload["event"] = "connection dropped"
	payload["err"] = err.Error()
	s.cfg.logger.Log(logger.SeverityWarning, payload)
}

func isEOF(err error) bool {
	// Peer hang-up between messages (io.EOF) surfaces wrapped by the codec's
	// malformed-truncation error; a deadline from Drain surfaces as timeout.
	var to interface{ Timeout() bool }
	if errors.As(err, &to) && to.Timeout() {
		return true
	}
	return errors.Is(err, os.ErrDeadlineExceeded)
}
