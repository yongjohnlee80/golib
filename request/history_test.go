package request

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func makeHistoryParams(method, url, body string, status int) *Params {
	reqBody := strings.NewReader(body)
	req, _ := http.NewRequest(method, url, reqBody)
	// Set GetBody so getRequestBody can re-read it.
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	}
	return &Params{
		Method:       method,
		Url:          url,
		Headers:      map[string]string{"Authorization": "Bearer x"},
		Request:      req,
		Response:     &http.Response{StatusCode: status},
		ResponseBody: `{"ok":true}`,
	}
}

// ---------------------------------------------------------------------------
// NewHistories
// ---------------------------------------------------------------------------

func TestNewHistories_DefaultSize(t *testing.T) {
	h := NewHistories(0)
	if h.limit != 3 {
		t.Fatalf("expected default limit 3, got %d", h.limit)
	}
}

func TestNewHistories_CustomSize(t *testing.T) {
	h := NewHistories(5)
	if h.limit != 5 {
		t.Fatalf("expected limit 5, got %d", h.limit)
	}
	if len(h.entries) != 0 {
		t.Fatalf("expected empty entries")
	}
}

// ---------------------------------------------------------------------------
// Add
// ---------------------------------------------------------------------------

func TestHistories_Add(t *testing.T) {
	h := NewHistories(3)
	p := makeHistoryParams("GET", "http://example.com/api", "", 200)
	h.Add(p)

	if len(h.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(h.entries))
	}
	e := h.entries[0]
	if e.method != "GET" {
		t.Fatalf("expected GET, got %s", e.method)
	}
	if e.url != "http://example.com/api" {
		t.Fatalf("expected url, got %s", e.url)
	}
	if e.res.status != 200 {
		t.Fatalf("expected status 200, got %d", e.res.status)
	}
	if string(e.res.body) != `{"ok":true}` {
		t.Fatalf("expected response body, got %s", string(e.res.body))
	}
}

func TestHistories_Add_CapturesRequestBody(t *testing.T) {
	h := NewHistories(3)
	p := makeHistoryParams("POST", "http://example.com", `{"name":"test"}`, 201)
	h.Add(p)

	if string(h.entries[0].req.body) != `{"name":"test"}` {
		t.Fatalf("expected request body captured, got %q", string(h.entries[0].req.body))
	}
	if h.entries[0].req.truncated {
		t.Fatal("small body should not be truncated")
	}
}

func TestHistories_Add_EvictsOldest(t *testing.T) {
	h := NewHistories(2)
	h.Add(makeHistoryParams("GET", "http://a.com/1", "", 200))
	h.Add(makeHistoryParams("GET", "http://a.com/2", "", 200))
	h.Add(makeHistoryParams("GET", "http://a.com/3", "", 200))

	if len(h.entries) != 2 {
		t.Fatalf("expected 2 entries after eviction, got %d", len(h.entries))
	}
	if h.entries[0].url != "http://a.com/2" {
		t.Fatalf("expected oldest evicted, first entry is %s", h.entries[0].url)
	}
	if h.entries[1].url != "http://a.com/3" {
		t.Fatalf("expected newest last, got %s", h.entries[1].url)
	}
}

func TestHistories_Add_ManyEvictions(t *testing.T) {
	h := NewHistories(1)
	for i := 0; i < 10; i++ {
		h.Add(makeHistoryParams("GET", "http://a.com", "", 200))
	}
	if len(h.entries) != 1 {
		t.Fatalf("expected 1 entry with limit=1, got %d", len(h.entries))
	}
}

func TestHistories_Add_CopiesHeaders(t *testing.T) {
	h := NewHistories(3)
	p := makeHistoryParams("GET", "http://a.com", "", 200)
	h.Add(p)

	// Mutate the original headers map.
	p.Headers["Authorization"] = "Bearer CHANGED"
	p.Headers["New-Header"] = "injected"

	// History entry should be unaffected.
	e := h.entries[0]
	if e.headers["Authorization"] != "Bearer x" {
		t.Fatalf("expected original header, got %s", e.headers["Authorization"])
	}
	if _, ok := e.headers["New-Header"]; ok {
		t.Fatal("history should not contain header added after Add()")
	}
}

func TestHistories_Add_TruncatesLargeBody(t *testing.T) {
	h := NewHistories(3)
	// Create a body larger than maxHistoryBodySize (64 KB).
	largeBody := strings.Repeat("x", maxHistoryBodySize+1024)
	p := makeHistoryParams("POST", "http://a.com", largeBody, 200)
	h.Add(p)

	e := h.entries[0]
	if !e.req.truncated {
		t.Fatal("expected body to be marked as truncated")
	}
	if len(e.req.body) != maxHistoryBodySize {
		t.Fatalf("expected body capped at %d, got %d", maxHistoryBodySize, len(e.req.body))
	}
}

// ---------------------------------------------------------------------------
// GetHistory
// ---------------------------------------------------------------------------

func TestHistories_GetHistory(t *testing.T) {
	h := NewHistories(5)
	h.Add(makeHistoryParams("GET", "http://a.com/1", "", 200))
	h.Add(makeHistoryParams("GET", "http://a.com/2", "", 201))
	h.Add(makeHistoryParams("GET", "http://a.com/3", "", 202))

	// prevPos=1 → most recent
	e := h.GetHistory(1)
	if e.url != "http://a.com/3" {
		t.Fatalf("expected most recent (pos=1), got %s", e.url)
	}

	// prevPos=2 → second most recent
	e = h.GetHistory(2)
	if e.url != "http://a.com/2" {
		t.Fatalf("expected second (pos=2), got %s", e.url)
	}

	// prevPos=3 → oldest
	e = h.GetHistory(3)
	if e.url != "http://a.com/1" {
		t.Fatalf("expected oldest (pos=3), got %s", e.url)
	}
}

func TestHistories_GetHistory_OutOfRange_High(t *testing.T) {
	h := NewHistories(5)
	h.Add(makeHistoryParams("GET", "http://a.com/1", "", 200))
	h.Add(makeHistoryParams("GET", "http://a.com/2", "", 200))

	// prevPos way too high → clamp to oldest
	e := h.GetHistory(100)
	if e.url != "http://a.com/1" {
		t.Fatalf("expected clamped to oldest, got %s", e.url)
	}
}

func TestHistories_GetHistory_OutOfRange_Low(t *testing.T) {
	h := NewHistories(5)
	h.Add(makeHistoryParams("GET", "http://a.com/1", "", 200))
	h.Add(makeHistoryParams("GET", "http://a.com/2", "", 200))

	// prevPos=0 → pos = len (out of range), clamp to newest
	e := h.GetHistory(0)
	if e.url != "http://a.com/2" {
		t.Fatalf("expected clamped to newest, got %s", e.url)
	}
}

// ---------------------------------------------------------------------------
// getRequestBody edge cases
// ---------------------------------------------------------------------------

func TestGetRequestBody_NilParams(t *testing.T) {
	h := NewHistories(1)
	data, _ := h.getRequestBody(nil)
	if data != nil {
		t.Fatal("expected nil for nil params")
	}
}

func TestGetRequestBody_NilRequest(t *testing.T) {
	h := NewHistories(1)
	data, _ := h.getRequestBody(&Params{})
	if data != nil {
		t.Fatal("expected nil for nil request")
	}
}

func TestGetRequestBody_NilBody(t *testing.T) {
	h := NewHistories(1)
	req, _ := http.NewRequest("GET", "http://x.com", nil)
	data, _ := h.getRequestBody(&Params{Request: req})
	if data != nil {
		t.Fatal("expected nil for nil body")
	}
}

func TestGetRequestBody_FallbackReadBody(t *testing.T) {
	h := NewHistories(1)
	body := "raw body content"
	req, _ := http.NewRequest("POST", "http://x.com", strings.NewReader(body))
	// No GetBody set — should read Body directly and restore it.
	req.GetBody = nil
	p := &Params{Request: req}

	data, truncated := h.getRequestBody(p)
	if string(data) != body {
		t.Fatalf("expected %q, got %q", body, string(data))
	}
	if truncated {
		t.Fatal("small body should not be truncated")
	}

	// Body should be restored and re-readable.
	restored, _ := io.ReadAll(p.Request.Body)
	if string(restored) != body {
		t.Fatalf("body not restored: %q", string(restored))
	}
}
