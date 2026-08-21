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
	Path      string   `json:"path"`
	NamedKeys []string `json:"namedKeys"`
}

// nonce returns a fresh CSP nonce.
//
// Per RESPONSE, never per process: a reused nonce is a reusable permission slip
// for injected script, which is the whole thing a nonce exists to prevent.
//
// # Why RawURLEncoding and not RawStdEncoding
//
// Not a style choice. html/template escapes "+" to "&#43;" inside an attribute
// value, so a standard-alphabet nonce containing a plus renders in the document
// as a DIFFERENT string than the CSP header declares — the browser then finds no
// matching nonce and blocks the script, on roughly half of all responses. The
// URL-safe alphabet (A-Za-z0-9-_) passes through every template context
// untouched. Found by a test that was intermittently red and would have been
// very easy to dismiss as flaky.
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

	cfg, err := json.Marshal(clientConfig{Path: h.wsPath, NamedKeys: NamedKeys()})
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
	cfg    Config
	mgr    *Manager
	log    logger.Logger
	limits Limits
	title  string
	wsPath string
	loop   *sessionLoop
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
	DefaultTitle  = "WebTUI"
	DefaultWSPath = "/ws"
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
	h := &Handler{
		cfg:    cfg,
		mgr:    mgr,
		log:    logger.Nop{},
		limits: DefaultLimits(),
		title:  DefaultTitle,
		wsPath: DefaultWSPath,
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
	}
	return h, nil
}

// ServeWS runs one WebSocket session. Wire it into ws.Handler.
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
