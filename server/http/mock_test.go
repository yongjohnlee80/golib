package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
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
