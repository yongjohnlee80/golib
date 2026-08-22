package web

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"slices"
	"strings"
	"time"

	"errors"

	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/server/ws"
)

//go:embed assets/client.js
var clientJS string

//go:embed assets/client.css
var clientCSS string

// pageTemplate is the served shell.
//
// Everything is INLINE — no external script, no external stylesheet — so the CSP
// can be nonce-only for script with no host allowlist at all, and so a
// deployment has no static-asset route to secure separately. The cost is that
// the page is not cacheable, which is what Cache-Control: no-store wanted
// anyway: a terminal's contents must not sit in a browser cache.
var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>{{.Title}}</title>
<style>{{.CSS}}</style>
</head><body>
<div id="g"></div>
<div id="cur"></div>
<div id="st"></div>
<textarea id="cap"
  autocomplete="off" autocapitalize="none" autocorrect="off" spellcheck="false"
  aria-label="terminal input"></textarea>
<div id="login" hidden>
  <label for="lu">User</label>
  <input id="lu" type="text" autocomplete="username" autocapitalize="none"
    spellcheck="false" autofocus>
  <label for="lp">Password</label>
  <!-- type=password and autocomplete=current-password on purpose. Unlike the
       capture textarea, this IS a credential field: the browser should treat it
       as one, so it masks the value and a password manager can fill it. A
       manager is a net security gain and suppressing it would push users toward
       weaker, memorable passwords. -->
  <input id="lp" type="password" autocomplete="current-password">
  <button id="lb" type="button">Sign in</button>
  <p id="le" role="alert"></p>
</div>
<script nonce="{{.Nonce}}">window.__WEBTUI__={{.Config}};</script>
<script nonce="{{.Nonce}}">{{.JS}}</script>
</body></html>
`))

// pageData is what the template renders.
type pageData struct {
	Title  string
	CSS    template.CSS
	JS     template.JS
	Nonce  string
	Config template.JS
}

// clientConfig is the JSON handed to the client.
//
// namedKeys comes from the SAME Go table the decoder consults, so the client's
// preventDefault decisions and the server's forwarding decisions cannot drift
// apart — which is the only reason the client is allowed to make that decision
// at all.
type clientConfig struct {
	Path      string         `json:"path"`
	LoginPath string         `json:"loginPath,omitempty"`
	NamedKeys []string       `json:"namedKeys"`
	Reserved  []reservedRule `json:"reserved"`
}

// nonce returns a fresh CSP nonce.
//
// Per RESPONSE, never per process: a reused nonce is a reusable permission slip
// for injected script, which is the whole thing a nonce exists to prevent.
//
// # Why RawURLEncoding
//
// The URL-safe alphabet (A-Za-z0-9-_) passes through every template context
// unchanged, so the byte sequence in the header is the byte sequence in the
// document. That makes the header and the source directly comparable and removes
// a whole class of question.
//
// CORRECTION (lector r1, 2026-08-22): an earlier version of this comment claimed
// that a "+" in a standard-alphabet nonce BREAKS the page, because html/template
// renders it as "&#43;". The escaping is real; the conclusion was invented. A
// browser decodes HTML entities before comparing the nonce, so
// script-src 'nonce-a+b' matches a source nonce written a&#43;b — lector verified
// that in headless Chromium. This encoding is a legitimate simplification, not a
// bug fix, and the test below asserts source-level identity rather than browser
// behavior it cannot observe.
func nonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// ServePage writes the client shell.
//
// The `title` is the operator's, not the App's: an App cannot set it, because a
// window title is a place where application data would escape the cell grid's
// escaping.
func (h *Handler) ServePage(w http.ResponseWriter, r *http.Request) {
	n, err := nonce()
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	hardeningHeaders(w.Header(), n)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	login := ""
	if h.cfg.LoginPolicy != nil {
		login = h.loginPath
	}
	cfg, err := json.Marshal(clientConfig{
		Path:      h.wsPath,
		LoginPath: login,
		NamedKeys: NamedKeys(),
		// Injected, not reimplemented. The reserved table was previously
		// hard-coded in BOTH places while the client's comment claimed it was
		// injected (lector r1), so the two could drift with nothing to notice.
		Reserved: ReservedRules(),
	})
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}

	// A HEAD or a non-GET must not receive a body, and must not receive a
	// different set of security headers either.
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_ = pageTemplate.Execute(w, pageData{
		Title:  h.title,
		CSS:    template.CSS(clientCSS),
		JS:     template.JS(clientJS),
		Nonce:  n,
		Config: template.JS(cfg),
	})
}

// Handler serves the WebTUI: the client shell and the WebSocket endpoint.
type Handler struct {
	cfg       Config
	mgr       *Manager
	log       logger.Logger
	limits    Limits
	title     string
	wsPath    string
	loginPath string
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

// WithLimits overrides §2.9's resource limits. Zero fields keep their defaults.
func WithLimits(l Limits) HandlerOption {
	return func(h *Handler) { h.limits = l.normalize() }
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
// Origin allowlist are all startup errors (§2.5, §2.8).
func NewHandler(cfg Config, mgr *Manager, opts ...HandlerOption) (*Handler, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if mgr == nil {
		return nil, errors.New("web.NewHandler: a session Manager is required")
	}
	// The allowlist is CLONED. A caller mutating their slice after NewHandler
	// changed the live allowlist in lector's probe, which makes it configuration
	// only until someone writes to it.
	cfg.AllowedOrigins = slices.Clone(cfg.AllowedOrigins)
	h := &Handler{
		cfg:       cfg,
		mgr:       mgr,
		log:       logger.Nop{},
		limits:    DefaultLimits(),
		title:     DefaultTitle,
		wsPath:    DefaultWSPath,
		loginPath: DefaultLoginPath,
		grace:     DefaultShutdownGrace,
	}
	for _, o := range opts {
		if o != nil {
			o(h)
		}
	}
	h.limits = h.limits.normalize()
	// Limits.QueueDepth is the single source of the event queue's capacity.
	// It previously said 1024 while Backend defaulted to 256 and nothing read
	// the field, so the documented limit and the real one were different numbers
	// (lector r1). The Manager builds each session's Backend, so the option is
	// pushed there.
	mgr.setQueueDepth(h.limits.QueueDepth)
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
// post-upgrade-check arrangement lector r1 flagged. [Handler.Mount] composes the
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
