package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestANonCanonicalPathIsRedirectedBeforeAnything: a path with //, . or .. is redirected (308, the
// query kept) to the one the router would read it as, before any middleware or route runs; a
// canonical path, trailing slash and all, is served.
func TestANonCanonicalPathIsRedirectedBeforeAnything(t *testing.T) {
	served := 0
	s := New(Middlewares(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { served++; next.ServeHTTP(w, r) })
	}))
	s.Get("/admin/keys", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := s.handler()
	for in, want := range map[string]string{
		"/admin//keys?x=1": "/admin/keys?x=1", "//admin/keys": "/admin/keys", "/admin/./keys": "/admin/keys",
		"/x/../admin/keys": "/admin/keys", "/admin//": "/admin/",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, in, nil))
		if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != want {
			t.Errorf("%s: %d to %q, want 308 to %q", in, rec.Code, rec.Header().Get("Location"), want)
		}
	}
	if served != 0 {
		t.Errorf("a middleware ran %d times for a path not canonical", served)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/keys", nil))
	if rec.Code != http.StatusNoContent || served != 1 {
		t.Errorf("the canonical path: %d, middleware ran %d", rec.Code, served)
	}
}
