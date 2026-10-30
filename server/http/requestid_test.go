package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestARequestIDIsKeptOnlyPlain: an inbound X-Request-ID of 1 to 64 plain characters is kept; one
// with a newline, a space, or 65 characters is replaced by a generated one, never echoed or put in
// the context.
func TestARequestIDIsKeptOnlyPlain(t *testing.T) {
	var seen string
	h := RequestID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = RequestIDFrom(r.Context()) }))
	for in, kept := range map[string]bool{
		"abc-DEF_123": true, strings.Repeat("a", 64): true,
		strings.Repeat("a", 65): false, "a\nb": false, "a b": false, "<script>": false, "": false,
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", in)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		got := w.Header().Get("X-Request-ID")
		if kept != (got == in) || got != seen || got == "" {
			t.Errorf("%q: echoed %q, in the context %q; kept %v", in, got, seen, kept)
		}
	}
}

// TestTheDefaultAddressIsThisHostOnly: a server given no Addr listens on loopback.
func TestTheDefaultAddressIsThisHostOnly(t *testing.T) {
	if got := defaultConfig().addr; got != "127.0.0.1:8080" {
		t.Errorf("the default listen address is %q, want 127.0.0.1:8080", got)
	}
}
