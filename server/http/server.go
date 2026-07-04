package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/server"
)

// Middleware is HTTP middleware: the core's Middleware instantiated for
// http.Handler (i.e. func(http.Handler) http.Handler — net/http-compatible).
type Middleware = server.Middleware[http.Handler]

// Server is a net/http server with chi-style routing, builder middleware, and a
// graceful, listener-owning lifecycle. Build with New, register routes, then Run.
type Server struct {
	router *server.Router[http.Handler]
	global *server.Chain[http.Handler]
	log    logger.Logger
	cfg    config

	notFound         http.HandlerFunc
	methodNotAllowed http.HandlerFunc

	mu      sync.Mutex
	httpSrv *http.Server
	ln      net.Listener
	addr    string

	// started flips when Listen binds. The router is not synchronized for
	// concurrent mutation, so registration after the server starts serving is
	// a programmer error and panics (matching the bad-route policy).
	started atomic.Bool
}

// config is the resolved, internal configuration of a Server. It is populated by
// applying defaultConfig and then each Option in order; callers never touch it
// directly. Duration fields follow net/http semantics: a zero value means "no
// timeout".
type config struct {
	addr              string           // TCP listen address, e.g. ":8080" or "127.0.0.1:0"
	readTimeout       time.Duration    // max time to read the entire request (incl. body)
	readHeaderTimeout time.Duration    // max time to read request headers (Slowloris guard)
	writeTimeout      time.Duration    // max time from end-of-headers to end-of-response
	idleTimeout       time.Duration    // max keep-alive idle time between requests
	shutdownTimeout   time.Duration    // grace period Run allows for in-flight requests
	maxHeaderBytes    int              // cap on request header size
	baseCtx           context.Context  // optional base context for all inbound requests
	tlsCert, tlsKey   string           // PEM file paths; non-empty cert enables TLS
	logger            logger.Logger    // lifecycle logger (defaults to logger.Nop)
	middleware        []Middleware     // global middleware, applied outermost in order
	notFound          http.HandlerFunc // override for the 404 handler
	methodNotAllowed  http.HandlerFunc // override for the 405 handler
}

// defaultConfig returns the baseline configuration applied before any Option. The
// non-zero timeout defaults are deliberately conservative production values; pass
// the matching timeout option with 0 to disable one explicitly.
func defaultConfig() *config {
	return &config{
		addr:              ":8080",
		readTimeout:       15 * time.Second,
		readHeaderTimeout: 5 * time.Second, // Slowloris guard
		writeTimeout:      15 * time.Second,
		idleTimeout:       60 * time.Second,
		shutdownTimeout:   10 * time.Second,
		maxHeaderBytes:    1 << 20,
	}
}

// Option configures a Server (mutate-and-return; applied in order). A 0 duration
// passed to a timeout option means "no timeout" explicitly.
type Option func(*config) *config

// Addr sets the TCP listen address (host:port). Use a ":0" port to bind an
// ephemeral port and read the real one back from Server.Addr after Listen.
func Addr(a string) Option { return func(c *config) *config { c.addr = a; return c } }

// ReadTimeout caps the time to read an entire request, including the body. 0
// disables it. Maps to http.Server.ReadTimeout.
func ReadTimeout(d time.Duration) Option {
	return func(c *config) *config { c.readTimeout = d; return c }
}

// ReadHeaderTimeout caps the time to read request headers — the primary Slowloris
// defence. 0 disables it. Maps to http.Server.ReadHeaderTimeout.
func ReadHeaderTimeout(d time.Duration) Option {
	return func(c *config) *config { c.readHeaderTimeout = d; return c }
}

// WriteTimeout caps the time from the end of the request headers to the end of the
// response write. 0 disables it. Maps to http.Server.WriteTimeout.
func WriteTimeout(d time.Duration) Option {
	return func(c *config) *config { c.writeTimeout = d; return c }
}

// IdleTimeout caps how long a keep-alive connection may sit idle between requests.
// 0 disables it. Maps to http.Server.IdleTimeout.
func IdleTimeout(d time.Duration) Option {
	return func(c *config) *config { c.idleTimeout = d; return c }
}

// ShutdownTimeout bounds the grace period Run grants in-flight requests when its
// context is cancelled. After it elapses, Shutdown returns and connections are
// dropped. It does not map to an http.Server field; Run applies it itself.
func ShutdownTimeout(d time.Duration) Option {
	return func(c *config) *config { c.shutdownTimeout = d; return c }
}

// MaxHeaderBytes caps the size of request headers. Maps to
// http.Server.MaxHeaderBytes (which itself defaults to 1 MiB when 0).
func MaxHeaderBytes(n int) Option { return func(c *config) *config { c.maxHeaderBytes = n; return c } }

// BaseContext sets the context that is the parent of every inbound request's
// context, letting handlers observe a server-wide cancellation or carry shared
// values. Maps to http.Server.BaseContext.
func BaseContext(ctx context.Context) Option {
	return func(c *config) *config { c.baseCtx = ctx; return c }
}

// WithLogger sets the logger used for lifecycle events (listening, shutting down)
// and, when passed to the built-in middleware, request logging. Defaults to
// logger.Nop (silent).
func WithLogger(l logger.Logger) Option { return func(c *config) *config { c.logger = l; return c } }

// Middlewares appends global middleware, applied outermost (before routing) and in
// the order given. Repeated calls accumulate. Recover is a common first entry.
func Middlewares(mw ...Middleware) Option {
	return func(c *config) *config { c.middleware = append(c.middleware, mw...); return c }
}

// TLS enables HTTPS using the given PEM certificate and key files. When set, Serve
// calls ServeTLS instead of Serve.
func TLS(certFile, keyFile string) Option {
	return func(c *config) *config { c.tlsCert, c.tlsKey = certFile, keyFile; return c }
}

// NotFound overrides the handler invoked when no route matches the path (HTTP 404).
// Global middleware still wraps it. The default writes a JSON error envelope.
func NotFound(h http.HandlerFunc) Option { return func(c *config) *config { c.notFound = h; return c } }

// MethodNotAllowed overrides the handler invoked when the path matches but the
// method does not (HTTP 405). The dispatcher sets the Allow header before calling
// it. The default writes a JSON error envelope.
func MethodNotAllowed(h http.HandlerFunc) Option {
	return func(c *config) *config { c.methodNotAllowed = h; return c }
}

// New builds a Server from options, applying safe defaults (see defaultConfig).
func New(opts ...Option) *Server {
	c := defaultConfig()
	for _, o := range opts {
		if o != nil {
			c = o(c)
		}
	}
	log := c.logger
	if log == nil {
		log = logger.Nop{}
	}
	s := &Server{
		router:           server.NewRouter[http.Handler](),
		global:           server.NewChain[http.Handler](c.middleware...),
		log:              log,
		cfg:              *c,
		notFound:         c.notFound,
		methodNotAllowed: c.methodNotAllowed,
	}
	if s.notFound == nil {
		s.notFound = defaultNotFound
	}
	if s.methodNotAllowed == nil {
		s.methodNotAllowed = defaultMethodNotAllowed
	}
	return s
}

// handler folds the global middleware once around the dispatcher.
func (s *Server) handler() http.Handler {
	return s.global.Then(http.HandlerFunc(s.dispatch))
}

// dispatch matches the request and invokes the (group/route-wrapped) handler, or
// the 404/405 handlers. Global middleware has already wrapped this.
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	res := s.router.Match(r.Method, r.URL.Path)
	switch {
	case !res.Found:
		s.notFound(w, r)
	case !res.MethodAllowed:
		w.Header().Set("Allow", strings.Join(res.AllowedMethods, ", "))
		s.methodNotAllowed(w, r)
	default:
		if res.Context != nil {
			r = r.WithContext(context.WithValue(r.Context(), rcKey{}, res.Context))
		}
		res.Handler.ServeHTTP(w, r)
	}
}

func defaultNotFound(w http.ResponseWriter, _ *http.Request) {
	Error(w, http.StatusNotFound, "not found")
}
func defaultMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	Error(w, http.StatusMethodNotAllowed, "method not allowed")
}

// --- lifecycle (the server owns listener creation) -------------------------

// Listen binds the listener synchronously (so bind errors are returned here and
// Addr() resolves a ":0" bind) and prepares the http.Server. Idempotent.
func (s *Server) Listen(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return nil
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.cfg.addr)
	if err != nil {
		return err
	}
	s.started.Store(true)
	s.ln = ln
	s.addr = ln.Addr().String()
	s.httpSrv = &http.Server{
		Handler:           s.handler(),
		ReadTimeout:       s.cfg.readTimeout,
		ReadHeaderTimeout: s.cfg.readHeaderTimeout,
		WriteTimeout:      s.cfg.writeTimeout,
		IdleTimeout:       s.cfg.idleTimeout,
		MaxHeaderBytes:    s.cfg.maxHeaderBytes,
	}
	if s.cfg.baseCtx != nil {
		base := s.cfg.baseCtx
		s.httpSrv.BaseContext = func(net.Listener) context.Context { return base }
	}
	return nil
}

// Serve serves the already-bound listener (blocks). Call after Listen.
func (s *Server) Serve() error {
	s.mu.Lock()
	srv, ln := s.httpSrv, s.ln
	cert, key := s.cfg.tlsCert, s.cfg.tlsKey
	s.mu.Unlock()
	if srv == nil || ln == nil {
		return errors.New("httpserver: Serve called before Listen")
	}
	if cert != "" {
		return srv.ServeTLS(ln, cert, key)
	}
	return srv.Serve(ln)
}

// Start binds and serves (blocking). Lower-level than Run.
func (s *Server) Start() error {
	if err := s.Listen(context.Background()); err != nil {
		return err
	}
	return s.Serve()
}

// Run binds synchronously, serves on a goroutine, and on ctx cancellation shuts
// down gracefully within ShutdownTimeout. Returns nil on a clean shutdown.
func (s *Server) Run(ctx context.Context) error {
	if err := s.Listen(ctx); err != nil {
		return err
	}
	s.log.Log(logger.SeverityInfo, map[string]any{"server": "http", "event": "listening", "addr": s.Addr()})
	errc := make(chan error, 1)
	go func() {
		err := s.Serve()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errc <- err
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), s.cfg.shutdownTimeout)
		defer cancel()
		s.log.Log(logger.SeverityInfo, map[string]any{"server": "http", "event": "shutting down"})
		shutdownErr := s.Shutdown(sctx)
		// Serve returns as soon as Shutdown is initiated, so this cannot
		// block; joining keeps a serve-time error that raced the
		// cancellation from being silently dropped.
		return errors.Join(shutdownErr, <-errc)
	}
}

// RunUntilSignal runs until SIGINT/SIGTERM, then graceful shutdown.
func (s *Server) RunUntilSignal(ctx context.Context) error {
	sctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return s.Run(sctx)
}

// Shutdown gracefully drains, bounded by ctx.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.httpSrv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// Addr returns the resolved listen address (valid after Listen/Run binds).
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// compile-time: *Server satisfies the core lifecycle contract.
var _ server.Server = (*Server)(nil)
