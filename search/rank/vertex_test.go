package rank

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	goodToken  = "ya29.good-token-text"
	staleToken = "ya29.stale-token-text"
	rankPath   = "/v1/projects/p-1/locations/global/rankingConfigs/default_ranking_config:rank"
)

type rankRecord struct {
	ID      string  `json:"id"`
	Title   *string `json:"title"`
	Content string  `json:"content"`
}

type rankRequest struct {
	Path, Auth, Project string
	Model, Query        string
	Records             []rankRecord
	Body                map[string]any
}

// rankFake is the Ranking API as Google's reference gives it: records of id and content, answered
// as records sorted by descending score (here a text's length), ids and scores only when details
// are ignored. reply, when set, answers a call instead (false from it: the normal answer).
type rankFake struct {
	mu    sync.Mutex
	seen  []rankRequest
	reply func(n int, r rankRequest, w http.ResponseWriter) bool
}

func (f *rankFake) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		var req struct {
			Model   string       `json:"model"`
			Query   string       `json:"query"`
			Records []rankRecord `json:"records"`
		}
		b, _ := json.Marshal(body)
		_ = json.Unmarshal(b, &req)
		got := rankRequest{Path: r.Method + " " + r.URL.Path, Auth: r.Header.Get("Authorization"),
			Project: r.Header.Get("X-Goog-User-Project"), Model: req.Model, Query: req.Query, Records: req.Records, Body: body}
		f.mu.Lock()
		n := len(f.seen)
		f.seen = append(f.seen, got)
		f.mu.Unlock()
		if f.reply != nil && f.reply(n, got, w) {
			return
		}
		if got.Auth != "Bearer "+goodToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		out := []map[string]any{}
		for _, rec := range req.Records {
			if rec.Content == "" {
				w.WriteHeader(http.StatusBadRequest) // neither title nor content
				return
			}
			out = append(out, map[string]any{"id": rec.ID, "score": float64(len(rec.Content)) / 100})
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i]["score"].(float64) > out[j]["score"].(float64) })
		_ = json.NewEncoder(w).Encode(map[string]any{"records": out})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *rankFake) requests() []rankRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.seen)
}

func tokensOf(tok string, asked *[]bool) TokenFunc {
	var mu sync.Mutex
	return func(ctx context.Context, refresh bool) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if asked != nil {
			*asked = append(*asked, refresh)
		}
		return tok, nil
	}
}

func newVertex(t *testing.T, url string, tokens TokenFunc, opts ...VertexOption) *Vertex {
	t.Helper()
	v, err := NewVertex(VertexConfig{Endpoint: url + "/", Project: "p-1", Model: "semantic-ranker-default-005"}, tokens, nil, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestVertexSendsGooglesRequest: one call at the global ranking config's :rank, with the token and
// the project; the model pinned, the query, every non-empty text a record of id and content (no
// title), no topN, details ignored; the scores come back in the texts' order whatever order the
// API answers in, an empty text scoring 0 unsent.
func TestVertexSendsGooglesRequest(t *testing.T) {
	f := &rankFake{}
	srv := f.server(t)
	v := newVertex(t, srv.URL, tokensOf(goodToken, nil))
	var calls []Call
	v.SetMeter(func(c Call) { calls = append(calls, c) })
	scores, err := v.Rank(context.Background(), "the query", []string{"a", "ccc", "", "bb"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(scores, []float64{0.01, 0.03, 0, 0.02}) {
		t.Errorf("scores %v", scores)
	}
	seen := f.requests()
	if len(seen) != 1 {
		t.Fatalf("%d calls", len(seen))
	}
	r := seen[0]
	if r.Path != "POST "+rankPath || r.Auth != "Bearer "+goodToken || r.Project != "p-1" ||
		r.Model != "semantic-ranker-default-005" || r.Query != "the query" {
		t.Errorf("request %+v", r)
	}
	want := []rankRecord{{ID: "0", Content: "a"}, {ID: "1", Content: "ccc"}, {ID: "3", Content: "bb"}}
	if !slices.Equal(r.Records, want) {
		t.Errorf("records %+v, want %+v", r.Records, want)
	}
	if _, ok := r.Body["topN"]; ok || r.Body["ignoreRecordDetailsInResponse"] != true {
		t.Errorf("body %v", r.Body)
	}
	if len(calls) != 1 || calls[0].Texts != 4 || calls[0].Err != nil {
		t.Errorf("meter heard %+v", calls)
	}
}

// TestVertexBatchesAndEmpties: texts past the batch go in further calls, ids within each; a batch
// of empty texts only is no call.
func TestVertexBatchesAndEmpties(t *testing.T) {
	f := &rankFake{}
	srv := f.server(t)
	v := newVertex(t, srv.URL, tokensOf(goodToken, nil), WithVertexBatch(2), nil)
	scores, err := v.Rank(context.Background(), "q", []string{"a", "bb", "", "", "ccc"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(scores, []float64{0.01, 0.02, 0, 0, 0.03}) {
		t.Errorf("scores %v", scores)
	}
	seen := f.requests()
	if len(seen) != 2 || len(seen[0].Records) != 2 || len(seen[1].Records) != 1 || seen[1].Records[0].ID != "0" {
		t.Errorf("calls %+v", seen)
	}
}

// TestVertexRetriesA401WithAFreshToken: a refused token is asked again with refresh and sent once
// more; refused again, it is ErrUnauthorized after two calls.
func TestVertexRetriesA401WithAFreshToken(t *testing.T) {
	f := &rankFake{}
	srv := f.server(t)
	var asked []bool
	v := newVertex(t, srv.URL, func(ctx context.Context, refresh bool) (string, error) {
		asked = append(asked, refresh)
		if refresh {
			return goodToken, nil
		}
		return staleToken, nil
	})
	if _, err := v.Rank(context.Background(), "q", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if seen := f.requests(); len(seen) != 2 || seen[0].Auth != "Bearer "+staleToken || seen[1].Auth != "Bearer "+goodToken ||
		!slices.Equal(asked, []bool{false, true}) {
		t.Errorf("calls %+v, refresh %v", seen, asked)
	}

	f2 := &rankFake{}
	srv2 := f2.server(t)
	asked = nil
	v2 := newVertex(t, srv2.URL, tokensOf(staleToken, &asked))
	_, err := v2.Rank(context.Background(), "q", []string{"a", "b"})
	if !errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrAPIDisabled) || len(f2.requests()) != 2 || !slices.Equal(asked, []bool{false, true}) {
		t.Errorf("twice refused: %v, %d calls, refresh %v", err, len(f2.requests()), asked)
	}
	if strings.Contains(err.Error(), staleToken) {
		t.Errorf("the token is in the error: %v", err)
	}
}

const disabledBody = `{"error":{"code":403,"message":"Discovery Engine API has not been used in project 123 before or it is disabled.",
"status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"SERVICE_DISABLED",
"domain":"googleapis.com","metadata":{"service":"discoveryengine.googleapis.com","consumer":"projects/123"}}]}}`

// TestVertexMapsErrors: each failure is the package's sentinel; the API's own text is never in an
// error, nor a token's.
func TestVertexMapsErrors(t *testing.T) {
	records := func(ids ...string) string {
		var out []map[string]any
		for _, id := range ids {
			out = append(out, map[string]any{"id": id, "score": 0.5})
		}
		b, _ := json.Marshal(map[string]any{"records": out})
		return string(b)
	}
	cases := []struct {
		name   string
		status int
		header map[string]string
		body   string
		want   []error
		not    []error
		says   string
	}{
		{name: "403", status: 403, body: `{"error":{"code":403,"message":"Permission denied on resource"}}`, want: []error{ErrUnauthorized}, not: []error{ErrAPIDisabled}},
		{name: "403 disabled", status: 403, body: disabledBody, want: []error{ErrAPIDisabled, ErrUnauthorized}, says: "discoveryengine.googleapis.com in projects/123"},
		{name: "404", status: 404, want: []error{ErrNoModel}},
		{name: "429 with Retry-After", status: 429, header: map[string]string{"Retry-After": "7"}, want: []error{ErrRateLimited}},
		{name: "429 without", status: 429, want: []error{ErrRateLimited}},
		{name: "500", status: 500, want: []error{ErrRefused}},
		{name: "malformed", status: 200, body: `{"records":`, want: []error{ErrBadAnswer}},
		{name: "a record missing", status: 200, body: records("0"), want: []error{ErrBadAnswer}},
		{name: "an unknown id", status: 200, body: records("0", "7"), want: []error{ErrBadAnswer}},
		{name: "an id twice", status: 200, body: records("0", "0"), want: []error{ErrBadAnswer}},
		{name: "no score", status: 200, body: `{"records":[{"id":"0"},{"id":"1","score":0.1}]}`, want: []error{ErrBadAnswer}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &rankFake{}
			f.reply = func(n int, r rankRequest, w http.ResponseWriter) bool {
				for k, v := range c.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
				return true
			}
			srv := f.server(t)
			_, err := newVertex(t, srv.URL, tokensOf(goodToken, nil)).Rank(context.Background(), "q", []string{"a", "b"})
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
			for _, leak := range []string{goodToken, "has not been used", "Permission denied on resource"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("the error has %q: %v", leak, err)
				}
			}
			if n := len(f.requests()); n != 1 {
				t.Errorf("%d calls: only a 401 is retried", n)
			}
		})
	}
}

// TestVertexTimeoutAndTokenFailure: past the client's timeout is ErrUnreachable; a token the
// caller can't give is its own error, passed on.
func TestVertexTimeoutAndTokenFailure(t *testing.T) {
	release := make(chan struct{})
	f := &rankFake{}
	f.reply = func(int, rankRequest, http.ResponseWriter) bool { <-release; return true }
	srv := f.server(t)
	defer close(release)
	v := newVertex(t, srv.URL, tokensOf(goodToken, nil), WithVertexTimeout(50*time.Millisecond))
	if _, err := v.Rank(context.Background(), "q", []string{"a"}); !errors.Is(err, ErrUnreachable) {
		t.Errorf("past the timeout: %v", err)
	}
	noCreds := errors.New("no credentials found")
	v.tokens = func(context.Context, bool) (string, error) { return "", noCreds }
	if _, err := v.Rank(context.Background(), "q", []string{"a"}); !errors.Is(err, noCreds) {
		t.Errorf("a token failure: %v", err)
	}
}

// TestNewVertexRefusesItsConfig: no project, no model (the API's moving default) or no token
// function is refused.
func TestNewVertexRefusesItsConfig(t *testing.T) {
	good := VertexConfig{Project: "p", Model: "semantic-ranker-default-005"}
	if _, err := NewVertex(VertexConfig{Model: good.Model}, tokensOf(goodToken, nil), nil); err == nil {
		t.Error("no project: accepted")
	}
	if _, err := NewVertex(VertexConfig{Project: "p"}, tokensOf(goodToken, nil), nil); !errors.Is(err, ErrNoModel) {
		t.Errorf("no model: %v", err)
	}
	if _, err := NewVertex(good, nil, nil); err == nil {
		t.Error("no tokens: accepted")
	}
	v, err := NewVertex(good, tokensOf(goodToken, nil), nil)
	if err != nil || v.url != VertexHost+"/v1/projects/p/locations/global/rankingConfigs/default_ranking_config:rank" ||
		v.Model() != (Model{Provider: "vertex", Name: good.Model, MaxBatch: DefaultVertexBatch}) {
		t.Errorf("defaults: %v, %q, %+v", err, v.url, v.Model())
	}
}
