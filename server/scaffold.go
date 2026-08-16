package server

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/yongjohnlee80/golib/logger"
)

// ConnHandler serves one accepted connection until it returns. ctx is the
// per-connection context: it derives from the scaffold's base context and is
// cancelled when Shutdown begins, so long-lived handlers can wind down.
// Closing conn is the handler's responsibility (defer conn.Close()).
type ConnHandler func(ctx context.Context, conn net.Conn)

// scaffoldConfig is the option-mutated construction state of a Scaffold.
type scaffoldConfig struct {
	addr           string
	listener       net.Listener
	tlsConfig      *tls.Config
	logger         logger.Logger
	baseCtx        context.Context
	drainTimeout   time.Duration
	sessionFactory func(ctx context.Context, conn net.Conn) Session
}

// ScaffoldOption configures a Scaffold.
type ScaffoldOption func(*scaffoldConfig)

// ScaffoldAddr sets the TCP listen address (default ":0").
func ScaffoldAddr(addr string) ScaffoldOption {
	return func(c *scaffoldConfig) { c.addr = addr }
}

// WithListener injects a pre-bound listener (tests, socket activation,
// zero-downtime restarts); it overrides ScaffoldAddr.
func WithListener(ln net.Listener) ScaffoldOption {
	return func(c *scaffoldConfig) { c.listener = ln }
}

// WithTLSConfig wraps the listener in tls.NewListener with cfg.
func WithTLSConfig(cfg *tls.Config) ScaffoldOption {
	return func(c *scaffoldConfig) { c.tlsConfig = cfg }
}

// ScaffoldLogger sets the lifecycle/accept-error logger (default Nop).
func ScaffoldLogger(l logger.Logger) ScaffoldOption {
	return func(c *scaffoldConfig) { c.logger = l }
}

// ScaffoldBaseContext sets the parent of every per-connection context.
func ScaffoldBaseContext(ctx context.Context) ScaffoldOption {
	return func(c *scaffoldConfig) { c.baseCtx = ctx }
}

// DrainTimeout bounds the graceful drain Run performs when its context is
// cancelled (default 10s, mirroring the HTTP transport's shutdown timeout).
// An explicit Shutdown call is bounded by its own ctx in addition.
func DrainTimeout(d time.Duration) ScaffoldOption {
	return func(c *scaffoldConfig) { c.drainTimeout = d }
}

// ScaffoldSessionFactory replaces the session registered for each accepted
// connection (ADR-0006 amendment, via ADR-0008). By default the scaffold
// registers a bare conn-closing session; a transport whose drain must be
// polite (finish in-flight replies before close) supplies a factory whose
// Session also implements Drainer. The ConnHandler retrieves the factory's
// session via SessionFromContext. A factory returning nil falls back to the
// default session for that connection.
func ScaffoldSessionFactory(fn func(ctx context.Context, conn net.Conn) Session) ScaffoldOption {
	return func(c *scaffoldConfig) { c.sessionFactory = fn }
}

// Scaffold owns the accept-loop lifecycle every connection-oriented transport
// otherwise reimplements (ADR-0006 §2.1): bind (or accept an injected
// listener), optional TLS, per-connection goroutine + context, structured
// accept-error handling with backoff, and drain-aware graceful shutdown via a
// session Registry. Scaffold satisfies the Server lifecycle contract.
type Scaffold struct {
	handle ConnHandler
	cfg    scaffoldConfig
	reg    Registry

	mu          sync.Mutex
	ln          net.Listener
	addr        string
	connCtx     context.Context
	cancelConns context.CancelFunc
}

// compile-time: Scaffold satisfies the lifecycle contract.
var _ Server = (*Scaffold)(nil)

// NewScaffold builds a Scaffold serving each accepted connection with handle.
func NewScaffold(handle ConnHandler, opts ...ScaffoldOption) *Scaffold {
	if handle == nil {
		panic("server.NewScaffold: nil ConnHandler")
	}
	cfg := scaffoldConfig{addr: ":0", logger: logger.Nop{}, drainTimeout: 10 * time.Second}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if cfg.baseCtx == nil {
		cfg.baseCtx = context.Background()
	}
	if cfg.logger == nil {
		cfg.logger = logger.Nop{}
	}
	return &Scaffold{handle: handle, cfg: cfg}
}

// Sessions returns the scaffold's registry. Accepted connections are tracked
// automatically; transports register richer sessions of their own.
func (s *Scaffold) Sessions() *Registry { return &s.reg }

// Addr returns the resolved listen address (valid once bound; reports the
// real port after binding ":0").
func (s *Scaffold) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// listen binds (or adopts the injected listener) and prepares the
// per-connection context. Idempotent.
func (s *Scaffold) listen(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return nil
	}
	ln := s.cfg.listener
	if ln == nil {
		var lc net.ListenConfig
		var err error
		ln, err = lc.Listen(ctx, "tcp", s.cfg.addr)
		if err != nil {
			return err
		}
	}
	if s.cfg.tlsConfig != nil {
		ln = tls.NewListener(ln, s.cfg.tlsConfig)
	}
	s.ln = ln
	s.addr = ln.Addr().String()
	s.connCtx, s.cancelConns = context.WithCancel(s.cfg.baseCtx)
	return nil
}

// Run binds synchronously (bind errors return before any goroutine; Addr
// resolves), serves until ctx is cancelled, then drains gracefully bounded by
// DrainTimeout. A terminal accept error is returned, never swallowed.
func (s *Scaffold) Run(ctx context.Context) error {
	if err := s.listen(ctx); err != nil {
		return err
	}
	s.cfg.logger.Log(logger.SeverityInfo,
		map[string]any{"server": "scaffold", "event": "listening", "addr": s.Addr()})
	errc := make(chan error, 1)
	go func() { errc <- s.acceptLoop() }()
	select {
	case err := <-errc:
		// Terminal accept failure: cancel per-conn contexts, best-effort drain.
		sctx, cancel := context.WithTimeout(context.Background(), s.cfg.drainTimeout)
		defer cancel()
		return errors.Join(err, s.Shutdown(sctx))
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), s.cfg.drainTimeout)
		defer cancel()
		s.cfg.logger.Log(logger.SeverityInfo,
			map[string]any{"server": "scaffold", "event": "shutting down"})
		shutdownErr := s.Shutdown(sctx)
		// The accept loop exits on the closed listener; joining keeps a racing
		// terminal error from being dropped (golib 02 rule).
		return errors.Join(shutdownErr, <-errc)
	}
}

// acceptLoop accepts until the listener closes (clean shutdown, returns nil)
// or a terminal error occurs. Temporary failures back off with a cap.
func (s *Scaffold) acceptLoop() error {
	backoff := 5 * time.Millisecond
	const maxBackoff = time.Second
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil // Shutdown closed the listener
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				s.cfg.logger.Log(logger.SeverityWarning,
					map[string]any{"server": "scaffold", "event": "accept retry", "err": err.Error()})
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				continue
			}
			return err
		}
		backoff = 5 * time.Millisecond
		go s.serveConn(conn)
	}
}

// connSession adapts a net.Conn to the registry's Session contract.
type connSession struct{ net.Conn }

// sessionCtxKey carries the per-connection Session in the handler context.
type sessionCtxKey struct{}

// SessionFromContext returns the Session registered for this connection —
// the ScaffoldSessionFactory product when one is configured, the default
// conn-closing session otherwise. Valid inside a ConnHandler; nil elsewhere.
func SessionFromContext(ctx context.Context) Session {
	s, _ := ctx.Value(sessionCtxKey{}).(Session)
	return s
}

// serveConn runs the handler for one connection: registered for drain,
// panic-isolated, always closed. The close and recovery defers are
// installed BEFORE any user-provided hook runs — a session-factory panic
// must be isolated exactly like a handler panic, never allowed to escape
// the accepted-connection goroutine and kill the process.
func (s *Scaffold) serveConn(conn net.Conn) {
	phase := "session setup"
	defer func() {
		if rec := recover(); rec != nil {
			s.cfg.logger.Log(logger.SeverityError, map[string]any{
				"server": "scaffold", "event": phase + " panic",
				"remote": remoteStr(conn), "recover": rec,
			})
		}
	}()
	defer conn.Close()

	sess := Session(connSession{conn})
	if s.cfg.sessionFactory != nil {
		if custom := s.cfg.sessionFactory(s.connCtx, conn); custom != nil {
			sess = custom
		}
	}
	unregister := s.reg.Register(sess)
	defer unregister()
	phase = "handler"
	s.handle(context.WithValue(s.connCtx, sessionCtxKey{}, sess), conn)
}

// remoteStr renders a peer address defensively: RemoteAddr may be nil on
// exotic net.Conn implementations, and this runs inside panic recovery.
func remoteStr(conn net.Conn) string {
	if conn == nil {
		return "unknown"
	}
	if a := conn.RemoteAddr(); a != nil {
		return a.String()
	}
	return "unknown"
}

// Shutdown stops accepting, cancels every per-connection context, and drains
// the registry bounded by ctx. Idempotent.
func (s *Scaffold) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	ln, cancel := s.ln, s.cancelConns
	s.mu.Unlock()
	if ln == nil {
		return nil
	}
	err := ln.Close()
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}
	if cancel != nil {
		cancel()
	}
	return errors.Join(err, s.reg.Drain(ctx))
}
