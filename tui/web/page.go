package web

import (
	_ "embed"
	"encoding/json"
	"html/template"
	"net/http"

	"github.com/yongjohnlee80/golib/tui/style"
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
	// Attrs is the attribute bits the frames' masks carry, by name: style's AttrMask, injected so
	// the painter reads the same values the server packs.
	Attrs map[string]uint16 `json:"attrs"`
}

// attrBits is the attribute bits by name, as the client is handed them (clientConfig.Attrs).
func attrBits() map[string]uint16 {
	names := style.AttrNames()
	m := make(map[string]uint16, len(names))
	for _, a := range names {
		m[a.Name] = uint16(a.Bit)
	}
	return m
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
// A "+" in a standard-alphabet nonce would not break the page: html/template
// renders it as "&#43;", but a browser decodes HTML entities before comparing the
// nonce, so script-src 'nonce-a+b' matches a source nonce written a&#43;b, as
// headless Chromium shows. This encoding is a simplification, and the test asserts
// source-level identity rather than browser behavior it cannot observe.
func nonce() string { return randomToken(16) }

// ServePage writes the client shell.
//
// The `title` is the operator's, not the App's: an App cannot set it, because a
// window title is a place where application data would escape the cell grid's
// escaping.
func (h *Handler) ServePage(w http.ResponseWriter, r *http.Request) {
	n := nonce()
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
		// Injected, not reimplemented: a table hard-coded in both the server and
		// the client could drift with nothing to notice.
		Reserved: ReservedRules(),
		Attrs:    attrBits(),
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
