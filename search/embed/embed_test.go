package embed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// server answers like Ollama or an OpenAI-compatible endpoint, recording each embed request.
type server struct {
	mu       sync.Mutex
	inputs   [][]string
	numCtx   []int // each embed request's options.num_ctx; 0 for none
	auth     []string
	dims     int
	status   int // non-zero: every embed answers this
	wrongLen bool
}

func TestOllamaContextReadsModelMaximum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" || r.Method != http.MethodPost {
			t.Errorf("unexpected route: %s %s", r.Method, r.URL.Path)
		}
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req.Model != "embeddinggemma" {
			t.Errorf("model %q", req.Model)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model_info": map[string]any{"gemma3.context_length": 2048}})
	}))
	defer srv.Close()
	got, err := OllamaContext(context.Background(), srv.URL, "", "embeddinggemma", srv.Client())
	if err != nil || got != 2048 {
		t.Fatalf("maximum = %d, %v; want 2048", got, err)
	}
}

func (s *server) vec(text string) []float32 {
	v := make([]float32, s.dims)
	for i := range v {
		v[i] = float32(len(text) + i)
	}
	return v
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{
			{"name": "embedder:latest", "model": "embedder:latest", "digest": "sha256:abc"},
			{"name": "other", "model": "other", "digest": "sha256:def"},
		}})
	})
	embed := func(w http.ResponseWriter, r *http.Request, openai bool) {
		var req struct {
			Model   string   `json:"model"`
			Input   []string `json:"input"`
			Options struct {
				NumCtx int `json:"num_ctx"`
			} `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.inputs = append(s.inputs, req.Input)
		s.numCtx = append(s.numCtx, req.Options.NumCtx)
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		status, wrong := s.status, s.wrongLen
		s.mu.Unlock()
		if status != 0 {
			http.Error(w, "model overloaded", status)
			return
		}
		var vecs [][]float32
		for i, t := range req.Input {
			v := s.vec(t)
			if wrong && i == 1 {
				v = v[:1]
			}
			vecs = append(vecs, v)
		}
		if !openai {
			_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": vecs, "prompt_eval_count": 3 * len(req.Input)})
			return
		}
		var data []map[string]any
		for i := len(vecs) - 1; i >= 0; i-- { // out of order: the client sorts by index
			data = append(data, map[string]any{"index": i, "embedding": vecs[i]})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "usage": map[string]any{"total_tokens": 5 * len(req.Input)}})
	}
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "text-embedding-3-small"}, {"id": "gpt-x"}}})
	})
	mux.HandleFunc("POST /api/embed", func(w http.ResponseWriter, r *http.Request) { embed(w, r, false) })
	mux.HandleFunc("POST /v1/embeddings", func(w http.ResponseWriter, r *http.Request) { embed(w, r, true) })
	return mux
}

func TestOllama(t *testing.T) {
	s := &server{dims: 4}
	ts := httptest.NewServer(s.handler())
	defer ts.Close()
	ctx := context.Background()
	o, err := NewOllama(ctx, ts.URL+"/", "", "embedder", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Model{Provider: "ollama", Name: "embedder", Digest: "sha256:abc", Dims: 4}
	if o.Model() != want || o.Model().Fingerprint() != "ollama|embedder|sha256:abc|4" {
		t.Errorf("model %+v, fingerprint %q", o.Model(), o.Model().Fingerprint())
	}
	got, err := o.Embed(ctx, []string{"a", "bcd"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, [][]float32{s.vec("a"), s.vec("bcd")}) {
		t.Errorf("vectors %v", got)
	}
	if !reflect.DeepEqual(s.inputs, [][]string{{"probe"}, {"a", "bcd"}}) {
		t.Errorf("requests %q", s.inputs)
	}
	if !reflect.DeepEqual(s.numCtx, []int{0, 0}) {
		t.Errorf("num_ctx without WithContext: %v, want none sent", s.numCtx)
	}
	if _, err := NewOllama(ctx, ts.URL, "", "missing", nil); err == nil || !strings.Contains(err.Error(), `no model "missing"`) {
		t.Errorf("a model the server lacks: %v", err)
	}
	s.wrongLen = true
	if _, err := o.Embed(ctx, []string{"a", "b"}); !errors.Is(err, ErrDims) {
		t.Errorf("a short vector: %v", err)
	}
	s.wrongLen = false
	for status, rejected := range map[int]bool{
		http.StatusServiceUnavailable: false, http.StatusUnauthorized: false, http.StatusTooManyRequests: false,
		http.StatusBadRequest: true, http.StatusRequestEntityTooLarge: true, http.StatusUnprocessableEntity: true,
	} {
		s.mu.Lock()
		s.status = status
		s.mu.Unlock()
		_, err := o.Embed(ctx, []string{"a"})
		if err == nil || !strings.Contains(err.Error(), strconv.Itoa(status)) || !strings.Contains(err.Error(), "model overloaded") {
			t.Errorf("a %d: %v", status, err)
		}
		if errors.Is(err, ErrRejected) != rejected {
			t.Errorf("a %d is ErrRejected: %v, want %v", status, errors.Is(err, ErrRejected), rejected)
		}
	}
}

func TestOpenAI(t *testing.T) {
	s := &server{dims: 3}
	ts := httptest.NewServer(s.handler())
	defer ts.Close()
	ctx := context.Background()
	o, err := NewOpenAI(ctx, ts.URL, "sk-test", "text-embed", nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.Model() != (Model{Provider: "openai", Name: "text-embed", Dims: 3}) {
		t.Errorf("model %+v", o.Model())
	}
	got, err := o.Embed(ctx, []string{"x", "yy", "zzz"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, [][]float32{s.vec("x"), s.vec("yy"), s.vec("zzz")}) {
		t.Errorf("vectors, in input order: %v", got)
	}
	for _, a := range s.auth {
		if a != "Bearer sk-test" {
			t.Errorf("authorization %q", a)
		}
	}
	anon, err := NewOpenAI(ctx, ts.URL, "", "text-embed", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := anon.Embed(ctx, []string{"x"}); err != nil || s.auth[len(s.auth)-1] != "" {
		t.Errorf("no key: %v, authorization %q", err, s.auth[len(s.auth)-1])
	}
}

// TestModelsListsWhatAProviderOffers: an Ollama server's installed models, an OpenAI-compatible
// endpoint's /v1/models under the key, sorted; a kind there is none of is refused.
func TestModelsListsWhatAProviderOffers(t *testing.T) {
	s := &server{dims: 4}
	ts := httptest.NewServer(s.handler())
	defer ts.Close()
	ctx := context.Background()
	if got, err := Models(ctx, "ollama", ts.URL, "", nil); err != nil || fmt.Sprint(got) != "[embedder:latest other]" {
		t.Fatalf("ollama models %v, %v", got, err)
	}
	if got, err := Models(ctx, "openai", ts.URL+"/", "k", nil); err != nil || fmt.Sprint(got) != "[gpt-x text-embedding-3-small]" {
		t.Fatalf("openai models %v, %v", got, err)
	}
	if last := s.auth[len(s.auth)-1]; last != "Bearer k" {
		t.Errorf("/v1/models was asked with %q, want the key", last)
	}
	if _, err := Models(ctx, "cohere", ts.URL, "", nil); err == nil {
		t.Error("a kind there is none of was not refused")
	}
}

// TestALimitAndARefusedKeyAreNamed: 429 is the usage limit, 401 and 403 the key, so a client can
// say which, and switch.
func TestALimitAndARefusedKeyAreNamed(t *testing.T) {
	for _, c := range []struct {
		status int
		want   error
	}{{http.StatusTooManyRequests, ErrRateLimited}, {http.StatusUnauthorized, ErrUnauthorized}, {http.StatusForbidden, ErrUnauthorized}} {
		s := &server{dims: 4}
		ts := httptest.NewServer(s.handler())
		p, err := NewOpenAI(context.Background(), ts.URL, "k", "m", nil)
		if err != nil {
			t.Fatal(err)
		}
		s.status = c.status
		if _, err := p.Embed(context.Background(), []string{"x"}); !errors.Is(err, c.want) {
			t.Errorf("status %d: %v, want %v", c.status, err, c.want)
		}
		ts.Close()
	}
}

// TestTheMeterHearsEveryCall: each Embed, after the probe, reports its texts, the tokens the
// provider counted and its outcome.
func TestTheMeterHearsEveryCall(t *testing.T) {
	s := &server{dims: 4}
	ts := httptest.NewServer(s.handler())
	defer ts.Close()
	ctx := context.Background()
	o, err := NewOllama(ctx, ts.URL, "", "embedder", nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewOpenAI(ctx, ts.URL, "k", "m", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		p    Provider
		want string
	}{{o, "2 6 <nil>; 1 0 embed:"}, {a, "2 10 <nil>; 1 0 embed:"}} {
		var calls []string
		c.p.(Metered).SetMeter(func(call Call) {
			e := "<nil>"
			if call.Err != nil {
				e = "embed:"
			}
			calls = append(calls, fmt.Sprintf("%d %d %s", call.Texts, call.Tokens, e))
		})
		if _, err := c.p.Embed(ctx, []string{"a", "b"}); err != nil {
			t.Fatal(err)
		}
		s.status = http.StatusTooManyRequests
		_, _ = c.p.Embed(ctx, []string{"c"})
		s.status = 0
		if got := strings.Join(calls, "; "); got != c.want {
			t.Errorf("%s: the meter heard %q, want %q", c.p.Name(), got, c.want)
		}
	}
}

// TestAnOllamaWithAKeySendsIt: Ollama Cloud takes its key as a bearer token, on the model list and
// every embed; a local server, given none, is sent none.
func TestAnOllamaWithAKeySendsIt(t *testing.T) {
	s := &server{dims: 4}
	ts := httptest.NewServer(s.handler())
	defer ts.Close()
	ctx := context.Background()
	o, err := NewOllama(ctx, ts.URL, "cloud-key", "embedder", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Embed(ctx, []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Models(ctx, "ollama", ts.URL, "cloud-key", nil); err != nil {
		t.Fatal(err)
	}
	for i, a := range s.auth {
		if a != "Bearer cloud-key" {
			t.Fatalf("request %d went with %q, want the key", i, a)
		}
	}
	s.auth = nil
	if _, err := NewOllama(ctx, ts.URL, "", "embedder", nil); err != nil {
		t.Fatal(err)
	}
	for i, a := range s.auth {
		if a != "" {
			t.Fatalf("a local server's request %d went with %q, want none", i, a)
		}
	}
}

// TestOllamaSendsItsContextWindow: WithContext's num_ctx rides every request, the probe with the
// rest, so the server loads the model once, at that size.
func TestOllamaSendsItsContextWindow(t *testing.T) {
	s := &server{dims: 4}
	ts := httptest.NewServer(s.handler())
	defer ts.Close()
	ctx := context.Background()
	o, err := NewOllama(ctx, ts.URL, "", "embedder", nil, WithContext(8192))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Embed(ctx, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.numCtx, []int{8192, 8192}) {
		t.Errorf("num_ctx per request %v, want 8192 on the probe and the embed", s.numCtx)
	}
}
