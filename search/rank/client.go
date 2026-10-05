package rank

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// The bounds of one request: how much of an answer a client reads, how large a request it sends,
// and how long it waits for one (DefaultTimeout, unless told otherwise).
const (
	maxAnswer      = 8 << 20
	maxRequest     = 8 << 20
	DefaultTimeout = 30 * time.Second
)

// ErrTooLarge is a request over the size a client sends: texts too long for one request.
var ErrTooLarge = errors.New("rank: the request is over the size a ranker is sent")

// DefaultRerankAPIBatch is the batch a rerank-API client sends at most, unless told otherwise.
const DefaultRerankAPIBatch = 100

func bearer(key string) http.Header {
	if key == "" {
		return nil
	}
	return http.Header{"Authorization": {"Bearer " + key}}
}

// call sends one JSON request, within timeout, and decodes the JSON answer. A failure is one of the
// package's sentinels with the method and URL, never the server's own text.
func call(ctx context.Context, client *http.Client, timeout time.Duration, method, url string, header http.Header, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		if len(b) > maxRequest {
			return fmt.Errorf("%w: %s %s: %d bytes", ErrTooLarge, method, url, len(b))
		}
		body = bytes.NewReader(b)
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		if parent.Err() != nil {
			return fmt.Errorf("rank: %s %s: %w", method, url, parent.Err()) // the caller gave up: not the ranker
		}
		return fmt.Errorf("%w: %s %s", ErrUnreachable, method, url) // its own timeout included
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxAnswer))
		var sentinel error
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			sentinel = ErrUnauthorized
		case http.StatusNotFound:
			sentinel = ErrNoModel
		case http.StatusTooManyRequests:
			sentinel = ErrRateLimited
		default:
			sentinel = ErrRefused
		}
		return fmt.Errorf("%w: %s %s: %s", sentinel, method, url, resp.Status)
	}
	// the whole answer first: a body that stalls or breaks after the headers is the ranker not
	// answering (or the caller giving up), never a bad answer
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	switch {
	case err != nil && parent.Err() != nil:
		return fmt.Errorf("rank: %s %s: %w", method, url, parent.Err())
	case err != nil:
		return fmt.Errorf("%w: %s %s: reading the answer", ErrUnreachable, method, url)
	case len(b) > maxAnswer:
		return fmt.Errorf("%w: %s %s: the answer is over %d bytes", ErrBadAnswer, method, url, maxAnswer)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("%w: %s %s: decoding the answer", ErrBadAnswer, method, url)
	}
	return nil
}

// indexed is one score of an answer, by the index of its text in the request; nil when the answer
// had none.
type indexed struct {
	Index *int
	Score *float64
}

// place puts a batch's indexed scores at offset into scores: every index present and in range,
// none twice, every score present and finite, one for each of the batch's n texts.
func place(scores []float64, set []bool, offset, n int, got []indexed) error {
	if len(got) != n {
		return fmt.Errorf("%w: %d scores for %d texts", ErrBadAnswer, len(got), n)
	}
	for _, g := range got {
		if g.Index == nil || *g.Index < 0 || *g.Index >= n || set[offset+*g.Index] {
			return fmt.Errorf("%w: an index missing, out of range or repeated", ErrBadAnswer)
		}
		if g.Score == nil || math.IsNaN(*g.Score) || math.IsInf(*g.Score, 0) {
			return fmt.Errorf("%w: text %d has no finite score", ErrBadAnswer, *g.Index)
		}
		scores[offset+*g.Index], set[offset+*g.Index] = *g.Score, true
	}
	return nil
}

// batched scores texts in batches of at most size, by send, merging the answers by offset.
func batched(ctx context.Context, texts []string, size int, send func(ctx context.Context, batch []string) ([]indexed, int, error)) ([]float64, int, error) {
	scores, set := make([]float64, len(texts)), make([]bool, len(texts))
	tokens := 0
	for offset := 0; offset < len(texts); offset += size {
		batch := texts[offset:min(offset+size, len(texts))]
		got, n, err := send(ctx, batch)
		if err != nil {
			return nil, tokens, err
		}
		tokens += n
		if err := place(scores, set, offset, len(batch), got); err != nil {
			return nil, tokens, err
		}
	}
	return scores, tokens, nil
}

// TEI ranks with a Hugging Face Text Embeddings Inference server serving a re-ranker model.
type TEI struct {
	meter
	base    string
	auth    http.Header
	client  *http.Client
	timeout time.Duration
	model   Model
}

// TEIOption configures a TEI client.
type TEIOption func(*TEI)

// WithTEITimeout bounds each of the client's requests (DefaultTimeout otherwise).
func WithTEITimeout(d time.Duration) TEIOption {
	return func(t *TEI) {
		if d > 0 {
			t.timeout = d
		}
	}
}

// NewTEI returns the client of the TEI server at base. It asks /info for the model, and refuses a
// server whose model is not a re-ranker (ErrNotARanker): an embedding model's server, say.
func NewTEI(ctx context.Context, base, key string, client *http.Client, opts ...TEIOption) (*TEI, error) {
	if client == nil {
		client = http.DefaultClient
	}
	t := &TEI{base: strings.TrimRight(base, "/"), auth: bearer(key), client: client, timeout: DefaultTimeout}
	for _, o := range opts {
		o(t)
	}
	var info struct {
		ModelID   string                     `json:"model_id"`
		ModelType map[string]json.RawMessage `json:"model_type"`
		MaxBatch  int                        `json:"max_client_batch_size"`
	}
	if err := call(ctx, client, t.timeout, http.MethodGet, t.base+"/info", t.auth, nil, &info); err != nil {
		return nil, err
	}
	if _, ok := info.ModelType["reranker"]; !ok {
		return nil, fmt.Errorf("%w: TEI at %s serves %q", ErrNotARanker, t.base, info.ModelID)
	}
	if info.MaxBatch <= 0 {
		info.MaxBatch = 32
	}
	t.model = Model{Provider: "tei", Name: info.ModelID, MaxBatch: info.MaxBatch}
	return t, nil
}

func (t *TEI) Model() Model { return t.model }

func (t *TEI) Rank(ctx context.Context, query string, texts []string) (scores []float64, err error) {
	start := time.Now()
	tokens := 0
	defer func() { t.report(len(texts), tokens, err, start) }()
	scores, tokens, err = batched(ctx, texts, t.model.MaxBatch, func(ctx context.Context, batch []string) ([]indexed, int, error) {
		var out []indexed
		req := map[string]any{"query": query, "texts": batch, "truncate": true}
		if err := call(ctx, t.client, t.timeout, http.MethodPost, t.base+"/rerank", t.auth, req, &out); err != nil {
			return nil, 0, err
		}
		return out, 0, nil
	})
	return scores, err
}

// RerankAPI ranks with a Cohere-style re-ranking endpoint: POST {base}/rerank, served by Cohere
// (…/v2), Jina, Voyage, vLLM, Infinity and llama.cpp's server (…/v1). base carries the version.
type RerankAPI struct {
	meter
	base    string
	auth    http.Header
	client  *http.Client
	timeout time.Duration
	model   Model
}

// RerankAPIOption configures a RerankAPI client.
type RerankAPIOption func(*RerankAPI)

// WithTimeout bounds each of the client's requests (DefaultTimeout otherwise).
func WithTimeout(d time.Duration) RerankAPIOption {
	return func(r *RerankAPI) {
		if d > 0 {
			r.timeout = d
		}
	}
}

// WithBatch sends at most n texts in one request (DefaultRerankAPIBatch otherwise).
func WithBatch(n int) RerankAPIOption {
	return func(r *RerankAPI) {
		if n > 0 {
			r.model.MaxBatch = n
		}
	}
}

// NewRerankAPI returns the client of model at base. It sends nothing: a probe is the caller's
// (Rank with two texts), since such endpoints describe no model.
func NewRerankAPI(base, key, model string, client *http.Client, opts ...RerankAPIOption) *RerankAPI {
	if client == nil {
		client = http.DefaultClient
	}
	r := &RerankAPI{base: strings.TrimRight(base, "/"), auth: bearer(key), client: client, timeout: DefaultTimeout,
		model: Model{Provider: "rerank-api", Name: model, MaxBatch: DefaultRerankAPIBatch}}
	for _, o := range opts {
		o(r)
	}
	return r
}

func (r *RerankAPI) Model() Model { return r.model }

func (r *RerankAPI) Rank(ctx context.Context, query string, texts []string) (scores []float64, err error) {
	start := time.Now()
	tokens := 0
	defer func() { r.report(len(texts), tokens, err, start) }()
	scores, tokens, err = batched(ctx, texts, r.model.MaxBatch, func(ctx context.Context, batch []string) ([]indexed, int, error) {
		var out struct {
			Results []struct {
				Index *int     `json:"index"`
				Score *float64 `json:"relevance_score"`
			} `json:"results"`
			Usage struct {
				Total int `json:"total_tokens"`
			} `json:"usage"`
		}
		// top_n is the whole batch, so the server scores every text
		req := map[string]any{"model": r.model.Name, "query": query, "documents": batch, "top_n": len(batch)}
		if err := call(ctx, r.client, r.timeout, http.MethodPost, r.base+"/rerank", r.auth, req, &out); err != nil {
			return nil, 0, err
		}
		got := make([]indexed, len(out.Results))
		for i, x := range out.Results {
			got[i] = indexed{Index: x.Index, Score: x.Score}
		}
		return got, out.Usage.Total, nil
	})
	return scores, err
}

var (
	_ Ranker  = (*TEI)(nil)
	_ Ranker  = (*RerankAPI)(nil)
	_ Metered = (*TEI)(nil)
	_ Metered = (*RerankAPI)(nil)
)
