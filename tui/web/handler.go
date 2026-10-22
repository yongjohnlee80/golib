package web

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/server/ws"
)

// Handler serves the WebTUI: the client shell and the WebSocket endpoint.
type Handler struct {
	cfg       Config
	mgr       *Manager
	log       logger.Logger
	limits    Limits
	title     string
	wsPath    string
	loginPath string
	onLogin   func(handoff string, id *auth.Identity, stash *Stash) error
	adm       *Admission // the Manager's: the login route leases from it
	// grace bounds session teardown on Serve's exit. Injectable so the boundary
	// is testable without a 30-second wait.
	grace time.Duration
	loop  *sessionLoop
}

// HandlerOption configures a [Handler].
type HandlerOption func(*Handler)

// Title sets the served page's title. Default "WebTUI".
//
// The operator's, not the App's: a window title is a place where application
// data would escape the cell grid's escaping, so an App cannot set it.
func Title(s string) HandlerOption {
	return func(h *Handler) {
		if s != "" {
			h.title = s
		}
	}
}

// WSPath sets the WebSocket endpoint path. Default "/ws".
func WSPath(p string) HandlerOption {
	return func(h *Handler) {
		if strings.HasPrefix(p, "/") {
			h.wsPath = p
		}
	}
}

// WithLimits overrides 's resource limits. Zero fields keep their defaults.
func WithLimits(l Limits) HandlerOption {
	return func(h *Handler) { h.limits = l.normalize() }
}

// OnLogin registers the hook that runs after [Config.LoginPolicy] succeeds and
// the attach ticket is minted.
//
// It receives the ticket's [HandoffID], the authenticated identity, and the
// request's [Stash] — so a consumer moves whatever its factor allocated during
// verification into its own park, keyed by the handoff.
//
// Returning an error FAILS THE LOGIN, before the ticket reaches the client. That
// is the rollback for "the state could not be recorded": a login whose state does
// not exist must not look like it succeeded.
//
// # The admission reservation, if you park by hand
//
// A slot in the [Admission]'s budget is reserved BEFORE this hook runs, keyed
// by the handoff, and returned when the handoff is claimed, released or expires.
// The reservation covers this hook's duration and no more.
//
// A hook slow enough to outlive it — a dial to an upstream that hangs — can come
// back to find its slot reclaimed by another login, and parking anyway produces an
// entry that nothing accounts for: the budget then allows more parked logins than
// its cap.
//
// So do not write to your park directly. Publish it through [Stash.CommitPark],
// which performs the write while the reservation's own lock is held — making
// membership and publication one step — and returns false when the reservation has
// lapsed, in which case you must not park. [SSO] does this for you and returns
// [ErrAdmissionLapsed] on that path.
func OnLogin(fn func(handoff string, id *auth.Identity, stash *Stash) error) HandlerOption {
	return func(h *Handler) { h.onLogin = fn }
}

// ShutdownGrace bounds how long sessions get to exit when [Handler.Serve]
// returns. Defaults to [DefaultShutdownGrace].
func ShutdownGrace(d time.Duration) HandlerOption {
	return func(h *Handler) {
		if d > 0 {
			h.grace = d
		}
	}
}

// HandlerLogger sets the log sink. Defaults to logger.Nop{}.
func HandlerLogger(l logger.Logger) HandlerOption {
	return func(h *Handler) {
		if l != nil {
			h.log = l
		}
	}
}

// Defaults for a [Handler].
const (
	DefaultTitle     = "WebTUI"
	DefaultWSPath    = "/ws"
	DefaultLoginPath = "/login"
)

// NewHandler validates the configuration and builds the served handler.
//
// Validation happens HERE, so a misconfigured WebTUI fails before it can accept
// a connection: a non-loopback plaintext bind, a missing policy and an empty
// Origin allowlist are all startup errors.
func NewHandler(cfg Config, mgr *Manager, opts ...HandlerOption) (*Handler, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if mgr == nil {
		return nil, errs.Wrap(errs.ErrInvalidArgument, "web.NewHandler: a session Manager is required")
	}
	// The allowlist is CLONED. Without this, a caller mutating their slice after
	// NewHandler returned would change the live allowlist, which leaves the
	// configuration mutable long after construction.
	cfg.AllowedOrigins = slices.Clone(cfg.AllowedOrigins)
	mgr.handlerQueue.Store(true) // its sessions default to DefaultQueueDepth (sessionBackendOptions)
	h := &Handler{
		cfg:       cfg,
		mgr:       mgr,
		log:       logger.Nop{},
		limits:    DefaultLimits(),
		title:     DefaultTitle,
		wsPath:    DefaultWSPath,
		loginPath: DefaultLoginPath,
		grace:     DefaultShutdownGrace,
		adm:       mgr.adm,
	}
	for _, o := range opts {
		if o != nil {
			o(h)
		}
	}
	h.limits = h.limits.normalize()
	h.loop = &sessionLoop{
		cfg:     cfg,
		mgr:     mgr,
		log:     h.log,
		limits:  h.limits,
		decoder: &decoder{},
		pending: newGate(h.limits.MaxPending),
	}
	return h, nil
}

// ServeWS runs one WebSocket session.
//
// LOW-LEVEL. It performs the handshake checks itself as a second line of
// defence, but it is meant to sit behind [Handler.Guard], which refuses before
// the upgrade — reaching this function already means a 101 was sent. An earlier
// comment said to wire it directly into ws.Handler, which invited exactly the
// post-upgrade-check arrangement flagged. [Handler.Mount] composes the
// two correctly; use this directly only if you are replicating that composition
// deliberately.
func (h *Handler) ServeWS(ctx context.Context, s *ws.Session) {
	if err := h.loop.serve(ctx, s, requestInfo{http: s.Request()}); err != nil {
		logger.Info(h.log, protocolNote{What: "session", Reason: err.Error()})
	}
}

// ReadLimit reports the WebSocket read limit to configure on ws.Handler, so the
// transport and this package agree on one number.
func (h *Handler) ReadLimit() int64 { return h.limits.MaxMessage }

// AllowedOrigins reports the configured origins, for wiring into the transport.
func (h *Handler) AllowedOrigins() []string {
	return slices.Clone(h.cfg.AllowedOrigins)
}
