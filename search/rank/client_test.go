package rank

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// teiServer is a TEI server whose model is modelType ("reranker", "embedding"), taking batches of
// at most batch, scoring a text by its length, and answering in its own order (best first). answer,
// when set, replaces its /rerank answer.
func teiServer(t *testing.T, modelType string, batch int, answer func(texts []string) any) (*httptest.Server, *[][]string) {
	t.Helper()
	var mu sync.Mutex
	var seen [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"model_id": "BAAI/bge-reranker-v2-m3",
				"model_type": map[string]any{modelType: map[string]any{}}, "max_client_batch_size": batch})
		case "/rerank":
			var req struct {
				Query    string   `json:"query"`
				Texts    []string `json:"texts"`
				Truncate bool     `json:"truncate"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !req.Truncate {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			seen = append(seen, req.Texts)
			mu.Unlock()
			if answer != nil {
				_ = json.NewEncoder(w).Encode(answer(req.Texts))
				return
			}
			out := []map[string]any{}
			for i, x := range req.Texts {
				out = append(out, map[string]any{"index": i, "score": float64(len(x))})
			}
			slices.Reverse(out) // not the texts' order
			_ = json.NewEncoder(w).Encode(out)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// TestTEIRanksInTheTextsOrder: the scores come back in the texts' order whatever order the server
// answers in, batched at the server's limit with every text scored once; the meter hears each call.
func TestTEIRanksInTheTextsOrder(t *testing.T) {
	srv, seen := teiServer(t, "reranker", 2, nil)
	r, err := NewTEI(context.Background(), srv.URL+"/", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m := r.Model(); m.Provider != "tei" || m.Name != "BAAI/bge-reranker-v2-m3" || m.MaxBatch != 2 {
		t.Fatalf("model %+v", m)
	}
	var calls []Call
	r.SetMeter(func(c Call) { calls = append(calls, c) })
	scores, err := r.Rank(context.Background(), "q", []string{"a", "bbb", "cc", "dddd", "e"})
	if err != nil || !slices.Equal(scores, []float64{1, 3, 2, 4, 1}) {
		t.Fatalf("scores %v, %v", scores, err)
	}
	if len(*seen) != 3 || len(calls) != 1 || calls[0].Texts != 5 || calls[0].Err != nil {
		t.Fatalf("batches %v, calls %+v", *seen, calls)
	}
}

// TestTEIRefusesAModelThatIsNotARanker: an embedding model's server is refused when set up.
func TestTEIRefusesAModelThatIsNotARanker(t *testing.T) {
	srv, _ := teiServer(t, "embedding", 32, nil)
	if _, err := NewTEI(context.Background(), srv.URL, "", nil); !errors.Is(err, ErrNotARanker) {
		t.Fatalf("an embedding server: %v", err)
	}
}

// TestAnAnswerThatIsNotOneScoreATextIsRefused: a missing, duplicated or out-of-range index is
// ErrBadAnswer, never a partial answer.
func TestAnAnswerThatIsNotOneScoreATextIsRefused(t *testing.T) {
	for name, answer := range map[string]func([]string) any{
		"missing":      func(x []string) any { return []map[string]any{{"index": 0, "score": 1}} },
		"duplicate":    func(x []string) any { return []map[string]any{{"index": 0, "score": 1}, {"index": 0, "score": 2}} },
		"out of range": func(x []string) any { return []map[string]any{{"index": 0, "score": 1}, {"index": 2, "score": 2}} },
		"not json":     func(x []string) any { return "nope" },
	} {
		srv, _ := teiServer(t, "reranker", 32, answer)
		r, err := NewTEI(context.Background(), srv.URL, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if scores, err := r.Rank(context.Background(), "q", []string{"a", "b"}); !errors.Is(err, ErrBadAnswer) {
			t.Errorf("%s: %v, %v", name, scores, err)
		}
	}
}

// TestTheRankersErrorsAreSentinels: each failure is its sentinel, and the server's own text is
// never in the error.
func TestTheRankersErrorsAreSentinels(t *testing.T) {
	for status, want := range map[int]error{http.StatusUnauthorized: ErrUnauthorized, http.StatusForbidden: ErrUnauthorized,
		http.StatusNotFound: ErrNoModel, http.StatusTooManyRequests: ErrRateLimited, http.StatusInternalServerError: ErrRefused} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "secret server detail", status)
		}))
		r := NewRerankAPI(srv.URL, "key", "m", nil)
		_, err := r.Rank(context.Background(), "q", []string{"a"})
		if !errors.Is(err, want) || strings.Contains(err.Error(), "secret") {
			t.Errorf("status %d: %v, want %v without the server's text", status, err, want)
		}
		srv.Close()
	}
	r := NewRerankAPI("http://127.0.0.1:1", "", "m", nil)
	if _, err := r.Rank(context.Background(), "q", []string{"a"}); !errors.Is(err, ErrUnreachable) {
		t.Errorf("nothing listening: %v", err)
	}
	if _, err := NewTEI(context.Background(), "http://127.0.0.1:1", "", nil); !errors.Is(err, ErrUnreachable) {
		t.Errorf("TEI, nothing listening: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Rank(ctx, "q", []string{"a"}); !errors.Is(err, context.Canceled) || errors.Is(err, ErrUnreachable) {
		t.Errorf("a cancelled call: %v", err)
	}
}

// TestTheRerankAPIAsksForEveryScore: top_n is each batch's full length, the key and model are sent,
// batches merge by offset, and the tokens the server reports are summed.
func TestTheRerankAPIAsksForEveryScore(t *testing.T) {
	var mu sync.Mutex
	var batches [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model     string   `json:"model"`
			Query     string   `json:"query"`
			Documents []string `json:"documents"`
			TopN      int      `json:"top_n"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/v2/rerank" || r.Header.Get("Authorization") != "Bearer k" || req.Model != "rerank-v3.5" || req.TopN != len(req.Documents) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		batches = append(batches, req.Documents)
		mu.Unlock()
		var results []map[string]any
		for i := len(req.Documents) - 1; i >= 0; i-- {
			results = append(results, map[string]any{"index": i, "relevance_score": float64(len(req.Documents[i])) / 10})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "usage": map[string]any{"total_tokens": 7}})
	}))
	defer srv.Close()
	r := NewRerankAPI(srv.URL+"/v2", "k", "rerank-v3.5", nil, WithBatch(2))
	var calls []Call
	r.SetMeter(func(c Call) { calls = append(calls, c) })
	scores, err := r.Rank(context.Background(), "q", []string{"a", "bb", "ccc"})
	if err != nil || !slices.Equal(scores, []float64{0.1, 0.2, 0.3}) || len(batches) != 2 {
		t.Fatalf("scores %v, %v, batches %v", scores, err, batches)
	}
	if len(calls) != 1 || calls[0].Tokens != 14 || calls[0].Texts != 3 {
		t.Fatalf("calls %+v", calls)
	}
	if m := r.Model(); m.Provider != "rerank-api" || m.MaxBatch != 2 {
		t.Fatalf("model %+v", m)
	}
	if m := NewRerankAPI(srv.URL, "", "m", nil).Model(); m.MaxBatch != DefaultRerankAPIBatch {
		t.Fatalf("default batch %+v", m)
	}
}

// TestTheHolder: a set ranker is current with its window clamped; nil holds none.
func TestTheHolder(t *testing.T) {
	var h Holder
	if r, w := h.Current(); r != nil || w != 0 {
		t.Fatal("the zero Holder holds a ranker")
	}
	r := NewRerankAPI("http://x", "", "m", nil)
	for in, want := range map[int]int{0: DefaultWindow, 1: MinWindow, 50: 50, 1000: MaxWindow} {
		h.Set(r, in)
		if got, w := h.Current(); got != r || w != want {
			t.Errorf("window %d: %v, %d; want %d", in, got, w, want)
		}
	}
	h.Set(nil, 40)
	if got, _ := h.Current(); got != nil {
		t.Fatal("Set(nil) left a ranker")
	}
}

// TestAnAnswerWithoutAScoreIsRefused: a missing or null score, or a missing index, is ErrBadAnswer,
// never a score of 0, from either client.
func TestAnAnswerWithoutAScoreIsRefused(t *testing.T) {
	for name, answer := range map[string]any{
		"no score":   []map[string]any{{"index": 0}, {"index": 1, "score": 1}},
		"null score": []map[string]any{{"index": 0, "score": nil}, {"index": 1, "score": 1}},
		"no index":   []map[string]any{{"score": 1}, {"index": 1, "score": 1}},
	} {
		srv, _ := teiServer(t, "reranker", 32, func([]string) any { return answer })
		r, err := NewTEI(context.Background(), srv.URL, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if scores, err := r.Rank(context.Background(), "q", []string{"a", "b"}); !errors.Is(err, ErrBadAnswer) {
			t.Errorf("TEI, %s: %v, %v", name, scores, err)
		}
	}
	for name, results := range map[string]any{
		"no score":   []map[string]any{{"index": 0}, {"index": 1, "relevance_score": 1}},
		"null score": []map[string]any{{"index": 0, "relevance_score": nil}, {"index": 1, "relevance_score": 1}},
		"no index":   []map[string]any{{"relevance_score": 1}, {"index": 1, "relevance_score": 1}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
		}))
		if scores, err := NewRerankAPI(srv.URL, "", "m", nil).Rank(context.Background(), "q", []string{"a", "b"}); !errors.Is(err, ErrBadAnswer) {
			t.Errorf("rerank-API, %s: %v, %v", name, scores, err)
		}
		srv.Close()
	}
}

// TestARequestIsBoundedInTimeAndSize: a ranker that does not answer within the client's timeout is
// unreachable, while the caller's own cancellation stays the caller's; a request over the size a
// ranker is sent is refused before anything is sent.
func TestARequestIsBoundedInTimeAndSize(t *testing.T) {
	hang := make(chan struct{})
	var sent atomicCount
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.add()
		<-hang
	}))
	defer srv.Close()
	defer close(hang)
	start := time.Now()
	_, err := NewRerankAPI(srv.URL, "", "m", nil, WithTimeout(50*time.Millisecond)).Rank(context.Background(), "q", []string{"a"})
	if !errors.Is(err, ErrUnreachable) || time.Since(start) > 5*time.Second {
		t.Fatalf("a hanging ranker: %v after %s", err, time.Since(start))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := NewRerankAPI(srv.URL, "", "m", nil).Rank(ctx, "q", []string{"a"}); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUnreachable) {
		t.Fatalf("the caller's deadline: %v", err)
	}
	if _, err := NewTEI(context.Background(), srv.URL, "", nil, WithTEITimeout(50*time.Millisecond)); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("a hanging TEI /info: %v", err)
	}
	before := sent.load()
	huge := strings.Repeat("x", maxRequest)
	if _, err := NewRerankAPI(srv.URL, "", "m", nil).Rank(context.Background(), "q", []string{huge}); !errors.Is(err, ErrTooLarge) || sent.load() != before {
		t.Fatalf("an oversized request: %v, sent %d", err, sent.load()-before)
	}
}

type atomicCount struct {
	mu sync.Mutex
	n  int
}

func (c *atomicCount) add() { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *atomicCount) load() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// TestABodyThatStallsIsUnreachable: a ranker that sends its headers and then stalls in the body is
// unreachable once the client's timeout passes, and a caller who gives up meanwhile gets their own
// error; an answer over the size a client reads is a bad answer.
func TestABodyThatStallsIsUnreachable(t *testing.T) {
	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results": [`))
		w.(http.Flusher).Flush()
		<-hang
	}))
	defer srv.Close()
	defer close(hang)
	if _, err := NewRerankAPI(srv.URL, "", "m", nil, WithTimeout(50*time.Millisecond)).Rank(context.Background(), "q", []string{"a"}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("a stalled body: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := NewRerankAPI(srv.URL, "", "m", nil).Rank(ctx, "q", []string{"a"}); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUnreachable) || errors.Is(err, ErrBadAnswer) {
		t.Fatalf("the caller's deadline during the body: %v", err)
	}
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"pad": "` + strings.Repeat("x", maxAnswer) + `"}`))
	}))
	defer big.Close()
	if _, err := NewRerankAPI(big.URL, "", "m", nil).Rank(context.Background(), "q", []string{"a"}); !errors.Is(err, ErrBadAnswer) {
		t.Fatalf("an oversized answer: %v", err)
	}
}
