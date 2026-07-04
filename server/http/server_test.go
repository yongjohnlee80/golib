package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// TestServer_Lifecycle exercises the real listener-owning path: Addr(":0")
// resolves a concrete port via Listen, Run serves it, and ctx-cancel shuts down.
func TestServer_Lifecycle(t *testing.T) {
	t.Parallel()
	s := New(Addr("127.0.0.1:0"))
	s.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = JSON(w, http.StatusOK, map[string]string{"id": URLParam(r, "id")})
	})

	if err := s.Listen(context.Background()); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := s.Addr()
	if addr == "" {
		t.Fatal("Addr empty after Listen")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	resp, err := http.Get("http://" + addr + "/users/42")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var got map[string]string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if got["id"] != "42" {
		t.Errorf("id=%q want 42", got["id"])
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil on graceful shutdown", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// TestServer_NotFoundVsMethodNotAllowed checks the 404/405 split surfaces from
// MatchResult, including the sorted Allow header.
func TestServer_NotFoundVsMethodNotAllowed(t *testing.T) {
	t.Parallel()
	s := New()
	s.Get("/x", func(w http.ResponseWriter, r *http.Request) { _ = JSON(w, 200, "g") })
	s.Post("/x", func(w http.ResponseWriter, r *http.Request) { _ = JSON(w, 200, "p") })
	h := s.handler()

	// 404 with the JSON envelope.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("404 expected, got %d", rec.Code)
	}
	var env struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Status != 404 {
		t.Errorf("404 envelope = %s (err %v)", rec.Body.Bytes(), err)
	}

	// 405 with the sorted Allow header.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("DELETE", "/x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("405 expected, got %d", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET, POST" {
		t.Errorf("Allow=%q want \"GET, POST\"", allow)
	}
}

// TestServer_MiddlewareOrder verifies global -> group -> route(With) -> handler.
func TestServer_MiddlewareOrder(t *testing.T) {
	t.Parallel()
	var trace []string
	tag := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				trace = append(trace, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	s := New(Middlewares(tag("global")))
	s.Group("/api", tag("group")).With(tag("route")).Get("/x", func(w http.ResponseWriter, r *http.Request) {
		trace = append(trace, "handler")
		_ = JSON(w, 200, "ok")
	})

	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/x", nil))
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
	want := []string{"global", "group", "route", "handler"}
	if !reflect.DeepEqual(trace, want) {
		t.Errorf("order=%v want %v", trace, want)
	}
}

// TestServer_Mount checks a sub-handler mounted under a prefix sees a stripped path.
func TestServer_Mount(t *testing.T) {
	t.Parallel()
	sub := http.NewServeMux()
	sub.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		_ = JSON(w, 200, map[string]string{"path": r.URL.Path})
	})
	s := New()
	s.Mount("/admin", sub)

	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/admin/status", nil))
	if rec.Code != 200 {
		t.Fatalf("mount status=%d body=%s", rec.Code, rec.Body.Bytes())
	}
	var got map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["path"] != "/status" {
		t.Errorf("mounted handler saw path %q, want /status (prefix stripped)", got["path"])
	}
}

func TestServer_RegisterAfterStartPanics(t *testing.T) {
	t.Parallel()
	s := New(Addr("127.0.0.1:0"))
	s.Get("/before", func(w http.ResponseWriter, r *http.Request) {})

	if err := s.Listen(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown(context.Background())

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic registering a route after the server started")
		}
	}()
	s.Get("/after", func(w http.ResponseWriter, r *http.Request) {})
}
