package embed

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The token the fakes accept, and one they refuse: neither may appear in any error.
const (
	goodToken  = "ya29.good-token-text"
	staleToken = "ya29.stale-token-text"
)

const vertexPath = "/v1/projects/p-1/locations/us/publishers/google/models/gemini-embedding-2:embedContent"

// vertexRequest is what the fake read of one call.
type vertexRequest struct {
	Path, Auth, Project, ContentType string
	Text                             string
	Dims                             *int
	Body                             map[string]any
}

// vertexFake is Vertex AI's :embedContent as Google's reference gives it: one content of text
// parts, embedContentConfig.outputDimensionality, an answer of embedding.values, usageMetadata
// and truncated. reply, when set, answers a call instead (nil from it: the normal answer).
type vertexFake struct {
	mu    sync.Mutex
	seen  []vertexRequest
	reply func(n int, r vertexRequest, w http.ResponseWriter) bool
}

func (f *vertexFake) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		var req struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			Config *struct {
				Dims *int `json:"outputDimensionality"`
			} `json:"embedContentConfig"`
		}
		b, _ := json.Marshal(body)
		_ = json.Unmarshal(b, &req)
		got := vertexRequest{Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Project: r.Header.Get("X-Goog-User-Project"),
			ContentType: r.Header.Get("Content-Type"), Body: body}
		if r.Method != http.MethodPost {
			got.Path = r.Method + " " + got.Path
		}
		if len(req.Content.Parts) == 1 {
			got.Text = req.Content.Parts[0].Text
		}
		if req.Config != nil {
			got.Dims = req.Config.Dims
		}
		f.mu.Lock()
		n := len(f.seen)
		f.seen = append(f.seen, got)
		f.mu.Unlock()
		if f.reply != nil && f.reply(n, got, w) {
			return
		}
		if got.Auth != "Bearer "+goodToken {
			http.Error(w, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`, http.StatusUnauthorized)
			return
		}
		if got.Path != vertexPath || len(req.Content.Parts) != 1 {
			http.Error(w, `{"error":{"code":400,"status":"INVALID_ARGUMENT"}}`, http.StatusBadRequest)
			return
		}
		dims := VertexMaxDims
		if got.Dims != nil {
			dims = *got.Dims
		}
		vec := make([]float32, dims)
		vec[len(got.Text)%dims] = 1
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embedding":     map[string]any{"values": vec},
			"usageMetadata": map[string]any{"promptTokenCount": len(got.Text), "totalTokenCount": len(got.Text) + 1000},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *vertexFake) requests() []vertexRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.seen)
}

// tokensOf answers good for every asking, and records each asking's refresh.
func tokensOf(good string, asked *[]bool) TokenFunc {
	var mu sync.Mutex
	return func(ctx context.Context, refresh bool) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if asked != nil {
			*asked = append(*asked, refresh)
		}
		return good, nil
	}
}

func vertexConfig(endpoint string, dims int) VertexConfig {
	return VertexConfig{Endpoint: endpoint, Project: "p-1", Location: "us", Model: VertexModel, Dimensions: dims}
}

// TestVertexSendsGooglesRequest: each text is one :embedContent call at the model's path, with the
// token and the project; a document in "title: none | text:", a query in "task: search result |
// query:"; outputDimensionality in embedContentConfig; the meter hears the input tokens Google
// counted (promptTokenCount), and the model's identity carries the format's version.
func TestVertexSendsGooglesRequest(t *testing.T) {
	f := &vertexFake{}
	srv := f.server(t)
	ctx := context.Background()
	v, err := NewVertex(ctx, vertexConfig(srv.URL+"/", 1536), tokensOf(goodToken, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Model().Fingerprint(); got != "vertex|gemini-embedding-2|doc-v1|1536" {
		t.Errorf("fingerprint %q", got)
	}
	var calls []Call
	v.SetMeter(func(c Call) { calls = append(calls, c) })
	vecs, err := v.Embed(ctx, []string{"alpha", "beta gamma"})
	if err != nil {
		t.Fatal(err)
	}
	q, err := Query(ctx, v, "where is alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 1536 || len(vecs[1]) != 1536 || len(q) != 1536 {
		t.Fatalf("sizes: %d vectors, %d and %d; query %d", len(vecs), len(vecs[0]), len(vecs[1]), len(q))
	}
	seen := f.requests()[1:] // after the probe
	wantTexts := []string{"title: none | text: alpha", "title: none | text: beta gamma", "task: search result | query: where is alpha"}
	if len(seen) != len(wantTexts) {
		t.Fatalf("%d calls, want one a text: %+v", len(seen), seen)
	}
	for i, r := range seen {
		if r.Path != vertexPath || r.Auth != "Bearer "+goodToken || r.Project != "p-1" || r.ContentType != "application/json" {
			t.Errorf("call %d: path %q, auth %q, project %q, type %q", i, r.Path, r.Auth, r.Project, r.ContentType)
		}
		if r.Text != wantTexts[i] {
			t.Errorf("call %d sent %q, want %q", i, r.Text, wantTexts[i])
		}
		if r.Dims == nil || *r.Dims != 1536 {
			t.Errorf("call %d: outputDimensionality %v, want 1536 in embedContentConfig", i, r.Dims)
		}
		if _, ok := r.Body["outputDimensionality"]; ok {
			t.Errorf("call %d sent the deprecated top-level outputDimensionality", i)
		}
	}
	if len(calls) != 2 || calls[0].Texts != 2 || calls[0].Tokens != len(wantTexts[0])+len(wantTexts[1]) ||
		calls[1].Texts != 1 || calls[1].Tokens != len(wantTexts[2]) {
		t.Errorf("meter heard %+v", calls)
	}
}

// TestVertexDefaultDimensions: no Dimensions sends no embedContentConfig, and the model's 3072 are
// expected.
func TestVertexDefaultDimensions(t *testing.T) {
	f := &vertexFake{}
	srv := f.server(t)
	v, err := NewVertex(context.Background(), vertexConfig(srv.URL, 0), tokensOf(goodToken, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Model().Dims != VertexMaxDims {
		t.Errorf("dims %d", v.Model().Dims)
	}
	if _, ok := f.requests()[0].Body["embedContentConfig"]; ok {
		t.Errorf("sent %v", f.requests()[0].Body)
	}
}

// TestVertexRetriesA401WithAFreshToken: a refused token is asked again with refresh, and the call
// sent once more with the fresh one.
func TestVertexRetriesA401WithAFreshToken(t *testing.T) {
	f := &vertexFake{}
	srv := f.server(t)
	var asked []bool
	tokens := func(ctx context.Context, refresh bool) (string, error) {
		asked = append(asked, refresh)
		if refresh {
			return goodToken, nil
		}
		return staleToken, nil // a cache's token, expired
	}
	if _, err := NewVertex(context.Background(), vertexConfig(srv.URL, 256), tokens, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(asked, []bool{false, true}) {
		t.Errorf("tokens asked with refresh %v, want [false true]", asked)
	}
	seen := f.requests()
	if len(seen) != 2 || seen[0].Auth != "Bearer "+staleToken || seen[1].Auth != "Bearer "+goodToken {
		t.Errorf("calls: %+v", seen)
	}
}

// TestVertexRefusesASecond401: a fresh token refused too is ErrUnauthorized, after two calls.
func TestVertexRefusesASecond401(t *testing.T) {
	f := &vertexFake{}
	srv := f.server(t)
	var asked []bool
	_, err := NewVertex(context.Background(), vertexConfig(srv.URL, 256), tokensOf(staleToken, &asked), nil)
	if !errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrAPIDisabled) {
		t.Fatalf("err %v, want ErrUnauthorized", err)
	}
	if len(f.requests()) != 2 || !slices.Equal(asked, []bool{false, true}) {
		t.Errorf("%d calls, refresh %v", len(f.requests()), asked)
	}
	if strings.Contains(err.Error(), staleToken) {
		t.Errorf("the token is in the error: %v", err)
	}
}

const disabledBody = `{"error":{"code":403,"message":"Vertex AI API has not been used in project 123 before or it is disabled.",
"status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"SERVICE_DISABLED",
"domain":"googleapis.com","metadata":{"service":"aiplatform.googleapis.com","consumer":"projects/123"}}]}}`

// TestVertexMapsErrors: each failure is the package's sentinel, after the probe succeeded; no
// error has a token's text.
func TestVertexMapsErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header map[string]string
		body   string
		want   []error
		not    []error
		says   string
	}{
		{name: "403", status: 403, body: `{"error":{"code":403,"status":"PERMISSION_DENIED"}}`, want: []error{ErrUnauthorized}, not: []error{ErrAPIDisabled}},
		{name: "403 disabled", status: 403, body: disabledBody, want: []error{ErrAPIDisabled, ErrUnauthorized}, says: "aiplatform.googleapis.com in projects/123"},
		{name: "429 with Retry-After", status: 429, header: map[string]string{"Retry-After": "7"}, want: []error{ErrRateLimited}},
		{name: "429 without", status: 429, want: []error{ErrRateLimited}},
		{name: "404", status: 404, want: []error{ErrNoModel}},
		{name: "400", status: 400, want: []error{ErrRejected}},
		{name: "500", status: 500, not: []error{ErrRejected, ErrUnauthorized, ErrRateLimited, ErrNoModel, ErrUnreachable}},
		{name: "malformed", status: 200, body: `{"embedding":`, says: "decoding the answer"},
		{name: "wrong size", status: 200, body: `{"embedding":{"values":[1,2,3]}}`, want: []error{ErrDims}},
		{name: "truncated", status: 200, body: `{"embedding":{"values":[]},"truncated":true}`, want: []error{ErrRejected}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &vertexFake{}
			f.reply = func(n int, r vertexRequest, w http.ResponseWriter) bool {
				if n == 0 {
					return false // the probe
				}
				for k, v := range c.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
				return true
			}
			srv := f.server(t)
			v, err := NewVertex(context.Background(), vertexConfig(srv.URL, 128), tokensOf(goodToken, nil), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = v.Embed(context.Background(), []string{"x"})
			if err == nil {
				t.Fatal("no error")
			}
			for _, w := range c.want {
				if !errors.Is(err, w) {
					t.Errorf("%v is not %v", err, w)
				}
			}
			for _, w := range c.not {
				if errors.Is(err, w) {
					t.Errorf("%v is %v", err, w)
				}
			}
			if c.says != "" && !strings.Contains(err.Error(), c.says) {
				t.Errorf("%v does not say %q", err, c.says)
			}
			if strings.Contains(err.Error(), goodToken) {
				t.Errorf("the token is in the error: %v", err)
			}
			if c.status == 401 || c.status == 403 || c.status == 429 {
				if n := len(f.requests()); n != 2 {
					t.Errorf("%d calls: only a 401 is retried", n)
				}
			}
		})
	}
}

// TestVertexTimeoutAndTokenFailure: a call past its deadline is the provider not answering, the
// deadline still visible; a token the caller can't give is its own error, passed on.
func TestVertexTimeoutAndTokenFailure(t *testing.T) {
	release := make(chan struct{})
	var stall atomic.Bool // set once the probe has answered: a probe that fails must fail, not hang
	f := &vertexFake{}
	f.reply = func(n int, r vertexRequest, w http.ResponseWriter) bool {
		if !stall.Load() {
			return false
		}
		<-release
		return true
	}
	srv := f.server(t)
	defer close(release)
	v, err := NewVertex(context.Background(), vertexConfig(srv.URL, 128), tokensOf(goodToken, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	stall.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = v.Embed(ctx, []string{"x"})
	if !errors.Is(err, ErrUnreachable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("past the deadline: %v", err)
	}

	noCreds := errors.New("no credentials found")
	v.tokens = func(context.Context, bool) (string, error) { return "", noCreds }
	if _, err := v.Embed(context.Background(), []string{"x"}); !errors.Is(err, noCreds) {
		t.Errorf("a token failure: %v", err)
	}
	v.tokens = func(context.Context, bool) (string, error) { return "", nil }
	if _, err := v.Embed(context.Background(), []string{"x"}); err == nil {
		t.Error("an empty token was sent")
	}
}

// TestVertexTruncatedTextIsSetAside: through Bisect, a text the API truncated is that text's
// rejection, and the others are embedded.
func TestVertexTruncatedTextIsSetAside(t *testing.T) {
	f := &vertexFake{}
	f.reply = func(n int, r vertexRequest, w http.ResponseWriter) bool {
		if !strings.HasSuffix(r.Text, "long") {
			return false
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embedding": map[string]any{"values": make([]float32, 128)}, "truncated": true})
		return true
	}
	srv := f.server(t)
	v, err := NewVertex(context.Background(), vertexConfig(srv.URL, 128), tokensOf(goodToken, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	vecs, rejected, err := Bisect(context.Background(), v, []string{"a", "too long", "b"}, 128, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rejected) != 1 || rejected[0].Index != 1 || vecs[1] != nil || vecs[0] == nil || vecs[2] == nil {
		t.Errorf("rejected %+v, vectors %v %v %v", rejected, vecs[0] != nil, vecs[1] != nil, vecs[2] != nil)
	}
}

// TestNewVertexRefusesItsConfig: what the client can't serve is refused before any call.
func TestNewVertexRefusesItsConfig(t *testing.T) {
	f := &vertexFake{}
	srv := f.server(t)
	good := vertexConfig(srv.URL, 0)
	cases := map[string]func(c *VertexConfig){
		"another model":   func(c *VertexConfig) { c.Model = "gemini-embedding-001" },
		"no project":      func(c *VertexConfig) { c.Project = "" },
		"a region":        func(c *VertexConfig) { c.Location = "us-central1" },
		"too few dims":    func(c *VertexConfig) { c.Dimensions = 64 },
		"too many dims":   func(c *VertexConfig) { c.Dimensions = 4096 },
		"no tokens (nil)": nil,
	}
	for name, change := range cases {
		c := good
		tokens := tokensOf(goodToken, nil)
		if change != nil {
			change(&c)
		} else {
			tokens = nil
		}
		_, err := NewVertex(context.Background(), c, tokens, nil)
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
		if name == "another model" && !errors.Is(err, ErrNoModel) {
			t.Errorf("another model: %v, want ErrNoModel", err)
		}
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("%d calls made", n)
	}
}

func TestVertexHost(t *testing.T) {
	for loc, want := range map[string]string{
		"global":      "https://aiplatform.googleapis.com",
		"us":          "https://aiplatform.us.rep.googleapis.com",
		"eu":          "https://aiplatform.eu.rep.googleapis.com",
		"us-central1": "",
	} {
		if got := VertexHost(loc); got != want {
			t.Errorf("VertexHost(%q) = %q, want %q", loc, got, want)
		}
	}
}

// docsOnly is a provider with no query format of its own.
type docsOnly struct{ got [][]string }

func (d *docsOnly) Name() string { return "docs" }
func (d *docsOnly) Model() Model { return Model{Provider: "docs", Name: "d", Dims: 2} }
func (d *docsOnly) Embed(_ context.Context, texts []string) ([][]float32, error) {
	d.got = append(d.got, texts)
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1, 0}
	}
	return out, nil
}

// TestQueryWithoutAQuerier: a provider with no EmbedQuery embeds a query as a document, as before.
func TestQueryWithoutAQuerier(t *testing.T) {
	d := &docsOnly{}
	v, err := Query(context.Background(), d, "q")
	if err != nil || len(v) != 2 || len(d.got) != 1 || !slices.Equal(d.got[0], []string{"q"}) {
		t.Errorf("Query = %v, %v; sent %v", v, err, d.got)
	}
}
