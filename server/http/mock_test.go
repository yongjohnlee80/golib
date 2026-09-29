package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestMockServer_StubAndGet(t *testing.T) {
	t.Parallel()
	m := NewMock()
	defer m.Close()

	m.Stub("GET", "/health", http.StatusOK, map[string]string{"status": "up"}).
		Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
			_ = JSON(w, http.StatusOK, map[string]string{"id": URLParam(r, "id")})
		})

	// stubbed route
	resp, err := m.Client().Get(m.URL() + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var health map[string]string
	_ = json.Unmarshal(body, &health)
	if health["status"] != "up" {
		t.Errorf("health=%s", body)
	}

	// dynamic route with a path param
	resp, err = m.Client().Get(m.URL() + "/users/99")
	if err != nil {
		t.Fatalf("GET /users/99: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	var u map[string]string
	_ = json.Unmarshal(body, &u)
	if u["id"] != "99" {
		t.Errorf("id=%s", body)
	}

	// recording captured both requests
	rec := m.Recorded()
	if len(rec) != 2 {
		t.Fatalf("recorded %d requests, want 2", len(rec))
	}
	if rec[0].Path != "/health" || rec[1].Path != "/users/99" {
		t.Errorf("recorded paths = %q, %q", rec[0].Path, rec[1].Path)
	}
}

// TestMockServer_Concurrent runs many concurrent clients to surface data races
// under -race (mutex-guarded recording + routing).
func TestMockServer_Concurrent(t *testing.T) {
	t.Parallel()
	m := NewMock()
	defer m.Close()
	m.Get("/n/{v}", func(w http.ResponseWriter, r *http.Request) {
		_ = JSON(w, http.StatusOK, URLParam(r, "v"))
	})

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			resp, err := m.Client().Get(m.URL() + "/n/" + strconv.Itoa(i))
			if err != nil {
				t.Errorf("req %d: %v", i, err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}(i)
	}
	wg.Wait()

	if len(m.Recorded()) != n {
		t.Errorf("recorded %d, want %d", len(m.Recorded()), n)
	}
}

// registerAll is an application's route registration, written once against
// Routes: every method of the interface, plus a path parameter.
func registerAll(r Routes) {
	echo := func(tag string) http.HandlerFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			_ = JSON(w, http.StatusOK, map[string]string{"route": tag, "id": URLParam(req, "id")})
		}
	}
	r.Get("/items/{id}", echo("get"))
	r.Post("/items", echo("post"))
	r.Put("/items/{id}", echo("put"))
	r.Patch("/items/{id}", echo("patch"))
	r.Delete("/items/{id}", echo("delete"))
	r.Head("/items/{id}", echo("head"))
	r.Options("/items/{id}", echo("options"))
	r.Handle("GET /handle", echo("handle"))
	r.HandleFunc("GET /handlefunc", echo("handlefunc"))
}

func TestRoutes_TheSameRegistrationServesTheMockAndTheServer(t *testing.T) {
	t.Parallel()
	cases := []struct{ method, path, route, id string }{
		{"GET", "/items/7", "get", "7"}, {"POST", "/items", "post", ""}, {"PUT", "/items/7", "put", "7"},
		{"PATCH", "/items/7", "patch", "7"}, {"DELETE", "/items/7", "delete", "7"},
		{"OPTIONS", "/items/7", "options", "7"}, {"GET", "/handle", "handle", ""}, {"GET", "/handlefunc", "handlefunc", ""},
	}
	check := func(t *testing.T, name string, serve func(*http.Request) (int, string)) {
		for _, c := range cases {
			code, body := serve(httptest.NewRequest(c.method, c.path, nil))
			var got map[string]string
			_ = json.Unmarshal([]byte(body), &got)
			if code != http.StatusOK || got["route"] != c.route || got["id"] != c.id {
				t.Errorf("%s %s %s = %d %s, want route %q id %q", name, c.method, c.path, code, body, c.route, c.id)
			}
		}
		if code, _ := serve(httptest.NewRequest("HEAD", "/items/7", nil)); code != http.StatusOK {
			t.Errorf("%s HEAD /items/7 = %d", name, code)
		}
	}

	m := NewMock()
	defer m.Close()
	registerAll(m.Routes())
	check(t, "mock", func(req *http.Request) (int, string) {
		out, err := http.NewRequest(req.Method, m.URL()+req.URL.Path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := m.Client().Do(out)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	})

	s := New()
	registerAll(s)
	check(t, "server", func(req *http.Request) (int, string) {
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	})

	g := New()
	registerAll(g.Group("/api"))
	rec := httptest.NewRecorder()
	g.handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/items/3", nil))
	if !strings.Contains(rec.Body.String(), `"id":"3"`) {
		t.Errorf("group GET /api/items/3 = %d %s", rec.Code, rec.Body.String())
	}
	if m.Recorded()[0].Method != "GET" {
		t.Errorf("registration through Routes still records requests: %+v", m.Recorded()[0])
	}
}
