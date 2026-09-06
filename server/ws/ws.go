package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/server"
)

// MessageType identifies a data frame kind. Control frames are handled
// internally and never surface here.
type MessageType int

const (
	Text   = MessageType(websocket.MessageText)
	Binary = MessageType(websocket.MessageBinary)
)

// StatusCode is a WebSocket close code (RFC 6455).
type StatusCode int

const (
	StatusNormalClosure = StatusCode(websocket.StatusNormalClosure)
	StatusGoingAway     = StatusCode(websocket.StatusGoingAway)
	StatusInternalError = StatusCode(websocket.StatusInternalError)
	StatusMessageTooBig = StatusCode(websocket.StatusMessageTooBig)
)

// Session is one established WebSocket connection. Methods are safe for one
// concurrent reader and one concurrent writer (the protocol's constraint —);
// all take ctx and honor its cancellation.
type Session struct {
	c      *websocket.Conn
	req    *http.Request
	cancel context.CancelFunc
	done   chan struct{}

	// closeOnce makes the close handshake single-owner: whoever fires first
	// (drain's GoingAway, the handler's normal exit, a panic path) wins and
	// the rest are no-ops — two concurrent Close calls on the underlying
	// connection can tear it down before any close frame flushes.
	closeOnce sync.Once
}

// closeWith performs the close handshake exactly once.
func (s *Session) closeWith(code StatusCode, reason string) {
	s.closeOnce.Do(func() { _ = s.c.Close(websocket.StatusCode(code), reason) })
}

// Read returns the next data message.
func (s *Session) Read(ctx context.Context) (MessageType, []byte, error) {
	t, p, err := s.c.Read(ctx)
	return MessageType(t), p, err
}

// Write sends one data message.
func (s *Session) Write(ctx context.Context, t MessageType, data []byte) error {
	return s.c.Write(ctx, websocket.MessageType(t), data)
}

// ReadJSON reads the next text message and unmarshals it into v.
func (s *Session) ReadJSON(ctx context.Context, v any) error {
	_, p, err := s.c.Read(ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(p, v)
}

// WriteJSON marshals v and sends it as one text message.
func (s *Session) WriteJSON(ctx context.Context, v any) error {
	p, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.c.Write(ctx, websocket.MessageText, p)
}

// Close performs the close handshake with the given code and reason.
func (s *Session) Close(code StatusCode, reason string) error {
	return s.c.Close(websocket.StatusCode(code), reason)
}

// Subprotocol returns the negotiated subprotocol ("" when none).
func (s *Session) Subprotocol() string { return s.c.Subprotocol() }

// Request returns the upgrade request (auth material, route params, peer info).
func (s *Session) Request() *http.Request { return s.req }

// keepalive pings on interval and force-ends the session when a pong misses
// the timeout — dead peers cannot hold shutdown hostage or leak goroutines.
func (s *Session) keepalive(ctx context.Context, interval, timeout time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, timeout)
			err := s.c.Ping(pctx)
			cancel()
			if err != nil {
				if ctx.Err() == nil { // a real miss, not session teardown
					s.cancel()
					_ = s.c.CloseNow()
				}
				return
			}
		}
	}
}

// config is the option-mutated Handler configuration.
type config struct {
	subprotocols   []string
	originPatterns []string
	readLimit      int64
	kaInterval     time.Duration
	kaTimeout      time.Duration
	logger         logger.Logger
}

// Option configures Handler.
type Option func(*config)

// Subprotocols advertises the subprotocols the endpoint accepts; the
// negotiated one is available via Session.Subprotocol.
func Subprotocols(names ...string) Option {
	return func(c *config) { c.subprotocols = names }
}

// InsecureAllowOrigins relaxes the SAME-ORIGIN DEFAULT: patterns are matched
// against the Origin header's host (e.g. "app.example.com"). The name says
// what it does — cross-origin browser access (CSWSH exposure) is a
// deliberate, visible decision (G4).
func InsecureAllowOrigins(patterns ...string) Option {
	return func(c *config) { c.originPatterns = patterns }
}

// ReadLimit caps a single message's size (default 1 MiB). Oversize messages
// fail the read and close the session with StatusMessageTooBig.
func ReadLimit(bytes int64) Option {
	return func(c *config) { c.readLimit = bytes }
}

// Keepalive configures the ping interval and pong deadline (defaults 30s /
// 10s). An interval of 0 disables keepalive.
func Keepalive(interval, timeout time.Duration) Option {
	return func(c *config) { c.kaInterval = interval; c.kaTimeout = timeout }
}

// WithLogger sets the upgrade/lifecycle logger (default Nop).
func WithLogger(l logger.Logger) Option {
	return func(c *config) { c.logger = l }
}

// wsEntry adapts a Session to the registry's Session/Drainer contracts.
type wsEntry struct{ s *Session }

// Close force-terminates the connection (registry deadline path).
func (e wsEntry) Close() error { return e.s.c.CloseNow() }

// Drain ends the session politely: initiate the StatusGoingAway close
// handshake (which unblocks the handler's pending Read via the peer's echo)
// and wait for the handler to return — bounded by ctx, after which the
// context is cancelled and the connection force-closed (/). The handshake is
// initiated before any cancellation so the
// peer deterministically observes GoingAway, never a bare teardown.
func (e wsEntry) Drain(ctx context.Context) error {
	go e.s.closeWith(StatusGoingAway, "server shutting down")
	select {
	case <-e.s.done:
		return nil
	case <-ctx.Done():
		e.s.cancel()
		return e.s.c.CloseNow()
	}
}

// Handler gates, upgrades, and serves one WebSocket endpoint with fn — an
// ordinary http.Handler, registered on the router and wrapped by middleware
// like any route.
//
// Drain gate: Handler reserves a registry slot BEFORE the
// handshake; during shutdown Reserve refuses and the request receives a plain
// HTTP 503 — never a successful upgrade followed by an immediate close. On
// success the reservation completes with the established session, which
// shutdown drains politely with StatusGoingAway.
//
// fn runs on the handler goroutine with a ctx cancelled at shutdown or
// keepalive failure; fn returning closes the session with a normal close
// frame. A panic in fn is recovered, logged, and closed as
// StatusInternalError.
func Handler(reg *server.Registry, fn func(ctx context.Context, s *Session), opts ...Option) http.Handler {
	cfg := config{readLimit: 1 << 20, kaInterval: 30 * time.Second, kaTimeout: 10 * time.Second, logger: logger.Nop{}}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res, ok := reg.Reserve()
		if !ok {
			http.Error(w, "server draining", http.StatusServiceUnavailable)
			return
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols:   cfg.subprotocols,
			OriginPatterns: cfg.originPatterns,
		})
		if err != nil {
			res.Cancel()
			cfg.logger.Log(logger.SeverityWarning, map[string]any{
				"server": "ws", "event": "upgrade failed", "path": r.URL.Path, "err": err.Error(),
			})
			return // Accept already wrote the HTTP error response
		}
		c.SetReadLimit(cfg.readLimit)

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		s := &Session{c: c, req: r, cancel: cancel, done: make(chan struct{})}

		unregister := res.Complete(wsEntry{s})
		defer unregister()
		defer close(s.done) // fires before unregister (LIFO): Drain unblocks first

		if cfg.kaInterval > 0 {
			go s.keepalive(ctx, cfg.kaInterval, cfg.kaTimeout)
		}

		defer func() {
			if rec := recover(); rec != nil {
				cfg.logger.Log(logger.SeverityError, map[string]any{
					"server": "ws", "event": "handler panic", "path": r.URL.Path, "recover": rec,
				})
				s.closeWith(StatusInternalError, "internal error")
				return
			}
			s.closeWith(StatusNormalClosure, "")
		}()
		fn(ctx, s)
	})
}
