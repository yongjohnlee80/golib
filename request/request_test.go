package request

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Request
// ---------------------------------------------------------------------------

func TestRequest_GET_JSON(t *testing.T) {
	want := map[string]string{"hello": "world"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", TypeJSON)
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	p := &Params{Method: "GET", Url: srv.URL}
	if err := Request(p, nil); err != nil {
		t.Fatal(err)
	}
	if p.Response.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", p.Response.StatusCode)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(p.ResponseBody), &got); err != nil {
		t.Fatal(err)
	}
	if got["hello"] != "world" {
		t.Fatalf("expected world, got %s", got["hello"])
	}
}

func TestRequest_POST_JSONPayload(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		ct := r.Header.Get("Content-Type")
		if ct != TypeJSONUTF8 {
			t.Fatalf("expected content-type %s, got %s", TypeJSONUTF8, ct)
		}
		var p payload
		json.NewDecoder(r.Body).Decode(&p)
		json.NewEncoder(w).Encode(p)
	}))
	defer srv.Close()

	p := &Params{Method: "POST", Url: srv.URL}
	if err := Request(p, payload{Name: "test"}); err != nil {
		t.Fatal(err)
	}
	var got payload
	json.Unmarshal([]byte(p.ResponseBody), &got)
	if got.Name != "test" {
		t.Fatalf("expected test, got %s", got.Name)
	}
}

func TestRequest_POST_FormValues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/x-www-form-urlencoded") {
			t.Fatalf("expected form content-type, got %s", ct)
		}
		r.ParseForm()
		if r.FormValue("key") != "value" {
			t.Fatalf("expected key=value, got key=%s", r.FormValue("key"))
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	vals := url.Values{"key": {"value"}}
	p := &Params{Method: "POST", Url: srv.URL}
	if err := Request(p, vals); err != nil {
		t.Fatal(err)
	}
}

func TestRequest_POST_IOReader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if string(data) != "raw bytes" {
			t.Fatalf("expected 'raw bytes', got %q", string(data))
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := &Params{Method: "POST", Url: srv.URL}
	if err := Request(p, strings.NewReader("raw bytes")); err != nil {
		t.Fatal(err)
	}
}

func TestRequest_POST_CustomPayload(t *testing.T) {
	mb := &MultipartBuffer{Boundary: "testboundary"}
	mb.WriteString("custom body")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if !strings.Contains(ct, "multipart/form-data; boundary=testboundary") {
			t.Fatalf("expected multipart content-type with boundary, got %s", ct)
		}
		data, _ := io.ReadAll(r.Body)
		if string(data) != "custom body" {
			t.Fatalf("expected 'custom body', got %q", string(data))
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := &Params{Method: "POST", Url: srv.URL}
	if err := Request(p, mb); err != nil {
		t.Fatal(err)
	}
}

func TestRequest_Headers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "abc" {
			t.Fatalf("expected X-Custom=abc, got %s", r.Header.Get("X-Custom"))
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Fatalf("expected Authorization header")
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := &Params{
		Method: "GET",
		Url:    srv.URL,
		Headers: map[string]string{
			"X-Custom":      "abc",
			"Authorization": "Bearer tok",
		},
	}
	if err := Request(p, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRequest_Cookies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("session")
		if err != nil {
			t.Fatal("missing session cookie")
		}
		if c.Value != "xyz" {
			t.Fatalf("expected session=xyz, got %s", c.Value)
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := &Params{
		Method:  "GET",
		Url:     srv.URL,
		Cookies: []*http.Cookie{{Name: "session", Value: "xyz"}},
	}
	if err := Request(p, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRequest_BasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, pw, ok := r.BasicAuth()
		if !ok || u != "user" || pw != "pass" {
			t.Fatalf("expected basic auth user:pass, got %s:%s (ok=%v)", u, pw, ok)
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := &Params{Method: "GET", Url: srv.URL, Username: "user", Password: "pass"}
	if err := Request(p, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRequest_NonSuccessStatus_NoTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"message":"internal"}`))
	}))
	defer srv.Close()

	p := &Params{Method: "GET", Url: srv.URL}
	err := Request(p, nil)
	if err != nil {
		t.Fatalf("Request should not return error for non-2xx status, got: %v", err)
	}
	if p.Response.StatusCode != 500 {
		t.Fatalf("expected 500, got %d", p.Response.StatusCode)
	}
	if p.ResponseBody != `{"message":"internal"}` {
		t.Fatalf("unexpected body: %s", p.ResponseBody)
	}
}

func TestRequest_TransportError(t *testing.T) {
	p := &Params{Method: "GET", Url: "http://127.0.0.1:1", Timeout: 1}
	err := Request(p, nil)
	if err == nil {
		t.Fatal("expected transport error for unreachable host")
	}
}

func TestRequest_InvalidPayload(t *testing.T) {
	p := &Params{Method: "POST", Url: "http://localhost"}
	err := Request(p, func() {}) // functions can't be marshalled
	if err == nil {
		t.Fatal("expected json marshal error for func payload")
	}
}

func TestRequest_DefaultTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p := &Params{Method: "GET", Url: srv.URL, Timeout: 0}
	if err := Request(p, nil); err != nil {
		t.Fatal(err)
	}
	if p.Timeout != 10 {
		t.Fatalf("expected default timeout 10, got %d", p.Timeout)
	}
}

func TestRequest_ResponseBodySizeLimit(t *testing.T) {
	// Server returns a body larger than our custom limit.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(strings.Repeat("x", 1024)))
	}))
	defer srv.Close()

	p := &Params{Method: "GET", Url: srv.URL, MaxResponseSize: 512}
	err := Request(p, nil)
	if err == nil {
		t.Fatal("expected error for oversized response body")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRequest_ResponseBodyUnlimited(t *testing.T) {
	body := strings.Repeat("x", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	p := &Params{Method: "GET", Url: srv.URL, MaxResponseSize: -1}
	err := Request(p, nil)
	if err != nil {
		t.Fatalf("unlimited mode should not error: %v", err)
	}
	if p.ResponseBody != body {
		t.Fatalf("expected full body, got %d bytes", len(p.ResponseBody))
	}
}

func TestRequest_WithRequestOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Injected") != "yes" {
			t.Fatal("expected X-Injected header from option")
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	opt := func(p *Params) *Params {
		if p.Headers == nil {
			p.Headers = make(map[string]string)
		}
		p.Headers["X-Injected"] = "yes"
		return p
	}

	p := &Params{Method: "GET", Url: srv.URL}
	if err := Request(p, nil, opt); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// StatusCodeInBounds
// ---------------------------------------------------------------------------

func TestStatusCodeInBounds(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{199, false},
		{200, true},
		{301, true},
		{399, true},
		{400, false},
		{404, false},
		{500, false},
	}
	for _, tt := range tests {
		if got := StatusCodeInBounds(tt.code); got != tt.want {
			t.Errorf("StatusCodeInBounds(%d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Get / Post convenience
// ---------------------------------------------------------------------------

func TestGet_Success(t *testing.T) {
	type resp struct {
		Value int `json:"value"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatalf("expected GET")
		}
		if r.URL.Query().Get("a") != "1" {
			t.Fatalf("expected query param a=1, got %s", r.URL.Query().Get("a"))
		}
		w.Header().Set("Content-Type", TypeJSON)
		w.Write([]byte(`{"value":42}`))
	}))
	defer srv.Close()

	var r resp
	res, err := Get(srv.URL, map[string][]string{"a": {"1"}}, &r)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	if r.Value != 42 {
		t.Fatalf("expected 42, got %d", r.Value)
	}
}

func TestGet_TrailingQuestionMark(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	type resp struct{ Ok bool }
	var r resp
	_, err := Get(srv.URL+"/?", nil, &r)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Ok {
		t.Fatal("expected ok=true")
	}
}

func TestGet_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"message":"gone"}`))
	}))
	defer srv.Close()

	var r map[string]any
	_, err := Get(srv.URL, nil, &r)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	reqErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if reqErr.Status != 404 {
		t.Fatalf("expected status 404, got %d", reqErr.Status)
	}
}

func TestPost_Success(t *testing.T) {
	type resp struct {
		ID string `json:"id"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST")
		}
		w.Header().Set("Content-Type", TypeJSON)
		w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	var r resp
	res, err := Post(srv.URL, map[string]string{"name": "test"}, &r)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("expected 200")
	}
	if r.ID != "abc" {
		t.Fatalf("expected abc, got %s", r.ID)
	}
}

func TestPost_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		w.Write([]byte(`{"message":"validation failed"}`))
	}))
	defer srv.Close()

	var r map[string]any
	_, err := Post(srv.URL, nil, &r)
	if err == nil {
		t.Fatal("expected error for 422")
	}
	reqErr := err.(*Error)
	if reqErr.Status != 422 {
		t.Fatalf("expected 422, got %d", reqErr.Status)
	}
}