package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func handlerFor(t *testing.T, opts ...HandlerOption) *Handler {
	t.Helper()
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { return newFakeApp() })
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Config{
		Addr:           "127.0.0.1:8080",
		Policy:         testPolicy(t),
		AllowedOrigins: []string{"https://tui.example.test"},
		ExpectedHost:   "tui.example.test",
	}, m, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Validation happens at construction, so a misconfigured WebTUI cannot accept a
// connection at all.
func TestNewHandler_ValidatesConfig(t *testing.T) {
	t.Parallel()
	m, err := NewManager(func(*Backend, *SessionInfo) Runner { return newFakeApp() })
	if err != nil {
		t.Fatal(err)
	}
	bad := map[string]Config{
		"exposed plaintext": {Addr: "0.0.0.0:8080", Policy: testPolicy(t),
			AllowedOrigins: []string{"https://x.test"}, ExpectedHost: "x.test"},
		"no policy":  {Addr: "127.0.0.1:8080", AllowedOrigins: []string{"https://x.test"}, ExpectedHost: "x.test"},
		"no origins": {Addr: "127.0.0.1:8080", Policy: testPolicy(t), ExpectedHost: "x.test"},
		"no expected host": {Addr: "127.0.0.1:8080", Policy: testPolicy(t),
			AllowedOrigins: []string{"https://x.test"}},
	}
	for name, cfg := range bad {
		if _, err := NewHandler(cfg, m); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// A nil manager would accept authenticated attaches and serve nothing.
	if _, err := NewHandler(Config{Addr: "127.0.0.1:1", Policy: testPolicy(t),
		AllowedOrigins: []string{"https://x.test"}, ExpectedHost: "x.test"}, nil); err == nil {
		t.Error("a nil Manager was accepted")
	}
}

// Criterion 10c: the served page carries the hardening headers, and its CSP is
// nonce-only for script.
func TestServePage_Hardening(t *testing.T) {
	t.Parallel()
	h := handlerFor(t)
	rec := httptest.NewRecorder()
	h.ServePage(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP missing frame-ancestors: %s", csp)
	}
	if !strings.Contains(csp, "nonce-") {
		t.Errorf("CSP has no script nonce: %s", csp)
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Error("a terminal's contents must not be cacheable")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}

	body := rec.Body.String()
	// The nonce in the header must be the one on the script tags, or the script
	// simply will not run.
	n := nonceFromCSP(t, csp)
	if got := strings.Count(body, `nonce="`+n+`"`); got != 2 {
		t.Errorf("%d script tags carry the header's nonce, want 2", got)
	}
	// No external script or stylesheet: the CSP allows neither, so a reference
	// would be a broken page rather than a working one.
	for _, forbidden := range []string{"<script src=", "<link rel=\"stylesheet\"", "http://", "https://fonts"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the page references something external: %q", forbidden)
		}
	}
}

// The nonce in the CSP header must appear VERBATIM in the document source, on
// every response.
//
// This asserts SOURCE-LEVEL identity, which is all a Go test can observe. It is
// deliberately not a claim about whether a browser would accept a nonce the
// template altered: browsers decode HTML entities before comparing, so
// script-src 'nonce-a+b' matches a source nonce written a&#43;b (verified in
// headless Chromium by lector r1, correcting an earlier claim of mine that said
// otherwise). What this test buys is a template that cannot silently rewrite the
// value, which keeps header and source comparable by inspection.
func TestServePage_NonceSurvivesTemplateEscaping(t *testing.T) {
	t.Parallel()
	h := handlerFor(t)
	for i := range 200 {
		rec := httptest.NewRecorder()
		h.ServePage(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		n := nonceFromCSP(t, rec.Header().Get("Content-Security-Policy"))
		body := rec.Body.String()
		if got := strings.Count(body, `nonce="`+n+`"`); got != 2 {
			t.Fatalf("response %d: nonce %q appears on %d script tags, want 2 — the "+
				"template rewrote the value, so header and source are no longer "+
				"comparable by inspection", i, n, got)
		}
		for _, c := range n {
			ok := c == '-' || c == '_' ||
				(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
			if !ok {
				t.Fatalf("nonce %q contains %q, which html/template may rewrite", n, c)
			}
		}
	}
}

func nonceFromCSP(t *testing.T, csp string) string {
	t.Helper()
	const marker = "'nonce-"
	i := strings.Index(csp, marker)
	if i < 0 {
		t.Fatalf("no nonce in CSP: %s", csp)
	}
	rest := csp[i+len(marker):]
	j := strings.Index(rest, "'")
	if j < 0 {
		t.Fatalf("unterminated nonce in CSP: %s", csp)
	}
	return rest[:j]
}

// A nonce must be per RESPONSE. A reused one is a reusable permission slip for
// injected script, which is the whole thing a nonce exists to prevent.
func TestServePage_NoncePerResponse(t *testing.T) {
	t.Parallel()
	h := handlerFor(t)
	seen := make(map[string]bool, 32)
	for range 32 {
		rec := httptest.NewRecorder()
		h.ServePage(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		n := nonceFromCSP(t, rec.Header().Get("Content-Security-Policy"))
		if seen[n] {
			t.Fatalf("nonce %q reused across responses", n)
		}
		seen[n] = true
	}
}

// The client's preventDefault tables come from the SAME Go values the decoder
// uses. That shared source is the only reason the client is allowed to make that
// decision at all.
func TestServePage_ShipsTheDecoderTables(t *testing.T) {
	t.Parallel()
	h := handlerFor(t, WSPath("/attach"))
	rec := httptest.NewRecorder()
	h.ServePage(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	for _, key := range NamedKeys() {
		if !strings.Contains(body, `"`+key+`"`) {
			t.Errorf("named key %q was not shipped to the client", key)
		}
	}
	if !strings.Contains(body, `"path":"/attach"`) {
		t.Errorf("the WS path was not shipped: %s", firstLineOf(body, "__WEBTUI__"))
	}
}

func firstLineOf(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], "\n")
	if j < 0 {
		j = len(s) - i
	}
	return s[i : i+j]
}

func TestServePage_NonGETIsRefused(t *testing.T) {
	t.Parallel()
	h := handlerFor(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServePage(rec, httptest.NewRequest(method, "/", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", method, rec.Code)
		}
		// The hardening headers must be present even on a refusal: an attacker
		// choosing the method must not choose the headers.
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s: no CSP on the refusal", method)
		}
	}
}

// The capture element must be focusable, so it cannot be display:none or
// visibility:hidden, and it must carry the attributes that disable autocomplete
// and autocorrect (§2.9 r8).
func TestServePage_CaptureElementAttributes(t *testing.T) {
	t.Parallel()
	h := handlerFor(t)
	rec := httptest.NewRecorder()
	h.ServePage(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()

	i := strings.Index(body, `id="cap"`)
	if i < 0 {
		t.Fatal("no capture element")
	}
	tag := body[i:]
	if j := strings.Index(tag, ">"); j > 0 {
		tag = tag[:j]
	}
	for _, want := range []string{
		`autocomplete="off"`, `autocapitalize="none"`, `spellcheck="false"`, `autocorrect="off"`,
	} {
		if !strings.Contains(tag, want) {
			t.Errorf("capture element missing %s: %s", want, tag)
		}
	}
	// No name and no form: a named field inside a form is what browsers offer to
	// save and autofill.
	if strings.Contains(tag, "name=") {
		t.Error("the capture element has a name, which invites autofill")
	}
	if strings.Contains(body, "<form") {
		t.Error("the page contains a form")
	}
	// Focusable: hidden elements cannot receive input.
	if strings.Contains(clientCSS, "#cap") {
		block := clientCSS[strings.Index(clientCSS, "#cap"):]
		if k := strings.Index(block, "}"); k > 0 {
			block = block[:k]
		}
		for _, bad := range []string{"display: none", "display:none", "visibility: hidden"} {
			if strings.Contains(block, bad) {
				t.Errorf("the capture element is unfocusable: %q", bad)
			}
		}
	}
}

// §2.6's containment must be in the shipped stylesheet: the probe informs the
// capability report, these rules are the actual guarantee.
func TestClientCSS_ContainsTheWideGraphemeHazard(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"overflow: hidden", "contain: strict"} {
		if !strings.Contains(clientCSS, want) {
			t.Errorf("the cell rule is missing %q, so a font mismatch could shift a row", want)
		}
	}
	// Ligatures would merge two cells into one glyph and desynchronize the row.
	if !strings.Contains(clientCSS, "font-variant-ligatures: none") {
		t.Error("ligatures are not disabled")
	}
	if !strings.Contains(clientCSS, "line-height: 1") {
		t.Error("line-height must be 1 or the row height disagrees with the measurement")
	}
}

// The client must drain the capture element, and must never use innerHTML for
// cell content.
func TestClientJS_Invariants(t *testing.T) {
	t.Parallel()
	if !strings.Contains(clientJS, "capture.value = ''") {
		t.Error("the capture element is never cleared, so the DOM becomes a keystroke log")
	}
	// Assignment specifically: the file's own comment mentions innerHTML to say
	// it is not used, and a test that cannot tell those apart would have to be
	// worked around rather than trusted.
	for _, form := range []string{"innerHTML =", "innerHTML=", ".innerHTML"} {
		if strings.Contains(clientJS, form) {
			t.Errorf("the client writes %q: cell content is application data", form)
		}
	}
	if !strings.Contains(clientJS, "textContent") {
		t.Error("cell content should be set via textContent")
	}
	// The ticket must be scrubbed from the address bar before the socket opens.
	if !strings.Contains(clientJS, "replaceState") {
		t.Error("the fragment is not scrubbed, so the ticket stays in the address bar")
	}
	// Acknowledge AFTER applying, or the server advances its baseline for a
	// frame this client never painted.
	applyIdx := strings.Index(clientJS, "for (let i = 0; i < u.length; i++) paint(u[i]);")
	ackIdx := strings.Index(clientJS, "send({ t: 'ack'")
	if applyIdx < 0 || ackIdx < 0 || ackIdx < applyIdx {
		t.Error("the ack is not sent after the frame is applied")
	}
}
